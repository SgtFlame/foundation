package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
	"github.com/samber/oops"
)

// Find reads one page of a query through the planner.
func (s *service) Find(args recordservice.FindArgs) recordservice.FindResponse {
	resource := records.Resource(args.Principal, args.Query.Entity)
	if err := authz.Check(args.Context(), args.Principal, "record.read", resource).Err("record.read", resource); err != nil {
		return recordservice.FindResponse{Common: *args.Fail(authz.Status, err)}
	}
	res, err := data.Find(args.ServiceArgs, s.deps, args.Query)
	if err != nil {
		return recordservice.FindResponse{Common: *args.Fail(statusOf(err), err)}
	}
	return recordservice.FindResponse{Common: features.OK(), Result: res}
}

// statusOf is a query error's HTTP status.
func statusOf(err error) uint16 {
	if o, ok := oops.AsOops(err); ok {
		switch o.Code() {
		case "entity_not_found":
			return http.StatusNotFound
		case "bad_filter", "bad_sort", "bad_cursor", "bad_expand", "bad_aggregate":
			return http.StatusBadRequest
		}
	}
	return http.StatusInternalServerError
}

// Aggregate computes an aggregation once.
func (s *service) Aggregate(args recordservice.AggregateArgs) recordservice.AggregateResponse {
	resource := records.Resource(args.Principal, args.Aggregation.Entity)
	if err := authz.Check(args.Context(), args.Principal, "record.read", resource).Err("record.read", resource); err != nil {
		return recordservice.AggregateResponse{Common: *args.Fail(authz.Status, err)}
	}
	sum, err := data.Summarize(args.ServiceArgs, s.deps, args.Aggregation)
	if err != nil {
		return recordservice.AggregateResponse{Common: *args.Fail(statusOf(err), err)}
	}
	return recordservice.AggregateResponse{Common: features.OK(), Summary: sum}
}
