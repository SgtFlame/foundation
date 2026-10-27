package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/usage"
	"github.com/SgtFlame/foundation/backend/features/usage/services/usageservice"
)

// Records pages through the tenant's usage records, newest first.
func (s *service) Records(args usageservice.RecordsArgs) usageservice.RecordsResponse {
	const action = "usage.read"
	resource := authz.Tenant(args.Principal)
	if err := authz.Check(args.Context(), args.Principal, action, resource).Err(action, resource); err != nil {
		return usageservice.RecordsResponse{Common: *args.Fail(authz.Status, err)}
	}
	size := args.PageSize
	switch {
	case size <= 0:
		size = 50
	case size > 500:
		size = 500
	}
	if args.Page < 0 {
		return usageservice.RecordsResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("bad_page").Errorf("page %d is negative", args.Page))}
	}

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
	var resp usageservice.RecordsResponse
	if err := q.Count(&resp.Total).Error; err != nil {
		return usageservice.RecordsResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	if err := q.Order("id DESC").Limit(size).Offset(args.Page * size).Find(&resp.Records).Error; err != nil {
		return usageservice.RecordsResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	resp.Common = features.OK()
	return resp
}
