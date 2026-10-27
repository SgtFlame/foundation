package impl

import (
	"fmt"
	"net/http"
	"slices"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/usage"
	"github.com/SgtFlame/foundation/backend/features/usage/services/usageservice"
	"gorm.io/gorm"
)

// bucketFormats are the strftime formats of the time buckets.
var bucketFormats = map[string]string{"hour": "%Y-%m-%d %H", "day": "%Y-%m-%d", "month": "%Y-%m"}

// groupColumns are the columns records can be grouped by.
var groupColumns = map[string]string{"feature": "feature", "model": "model", "level": "level", "conversation": "conversation_id"}

const sums = "COUNT(*) AS requests, SUM(input_tokens) AS input_tokens, SUM(cached_tokens) AS cached_tokens, SUM(output_tokens) AS output_tokens, " +
	"SUM(thinking_tokens) AS thinking_tokens, SUM(embed_tokens) AS embed_tokens, SUM(cost_micros) AS cost_micros"

// Summary sums the tenant's usage records for the Dashboard.
func (s *service) Summary(args usageservice.SummaryArgs) usageservice.SummaryResponse {
	const action = "usage.read"
	resource := authz.Tenant(args.Principal)
	if err := authz.Check(args.Context(), args.Principal, action, resource).Err(action, resource); err != nil {
		return usageservice.SummaryResponse{Common: *args.Fail(authz.Status, err)}
	}
	format, okBucket := bucketFormats[args.Bucket]
	column, okGroup := groupColumns[args.GroupBy]
	switch {
	case args.Bucket != "" && !okBucket:
		return usageservice.SummaryResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("bad_bucket").Errorf("bucket %q is not hour, day or month", args.Bucket))}
	case args.GroupBy != "" && !okGroup:
		return usageservice.SummaryResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("bad_group").Errorf("cannot group by %q", args.GroupBy))}
	case args.TZOffsetMinutes < -14*60 || args.TZOffsetMinutes > 14*60:
		return usageservice.SummaryResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("bad_offset").New("the time zone offset is out of range"))}
	}

	ranged := func() *gorm.DB {
		q := s.db.WithContext(args.Context()).Model(&usage.Record{}).Scopes(db.InTenant(args.Principal.TenantID))
		if !args.From.IsZero() {
			q = q.Where("created_at >= ?", args.From)
		}
		if !args.To.IsZero() {
			q = q.Where("created_at < ?", args.To)
		}
		if args.Feature != "" {
			q = q.Where("feature = ?", args.Feature)
		}
		return q
	}
	resp := usageservice.SummaryResponse{Common: features.OK()}
	if err := ranged().Select(sums).Scan(&resp.Totals).Error; err != nil {
		return usageservice.SummaryResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	if !okBucket && !okGroup {
		return resp
	}

	// The groups kept by Limit, costliest first
	var keep []string
	if okGroup && args.Limit > 0 {
		var top []struct{ Group string }
		err := ranged().Select("CAST(" + column + " AS TEXT) AS \"group\", SUM(cost_micros) AS cost").Group(column).
			Order("cost DESC").Limit(args.Limit).Scan(&top).Error
		if err != nil {
			return usageservice.SummaryResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
		}
		for _, t := range top {
			keep = append(keep, t.Group)
		}
	}

	bucket, group := "''", "''"
	if okBucket {
		bucket = fmt.Sprintf("strftime('%s', created_at, '%+d minutes')", format, args.TZOffsetMinutes)
	}
	if okGroup {
		group = "CAST(" + column + " AS TEXT)"
	}
	q := ranged().Select(bucket + " AS bucket, " + group + " AS \"group\", " + sums).Group("bucket, \"group\"").Order("bucket, cost_micros DESC")
	if keep != nil {
		q = q.Where(column+" IN ?", keep)
	}
	if err := q.Scan(&resp.Rows).Error; err != nil {
		return usageservice.SummaryResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	if keep != nil && !okBucket { // keep the costliest-first order of the limit
		slices.SortStableFunc(resp.Rows, func(a, b usageservice.SummaryRow) int {
			return slices.Index(keep, a.Group) - slices.Index(keep, b.Group)
		})
	}
	return resp
}
