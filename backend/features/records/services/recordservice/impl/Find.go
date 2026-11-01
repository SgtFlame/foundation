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
		var status uint16 = http.StatusInternalServerError
		if o, ok := oops.AsOops(err); ok {
			switch o.Code() {
			case "entity_not_found":
				status = http.StatusNotFound
			case "bad_filter", "bad_sort", "bad_cursor":
				status = http.StatusBadRequest
			}
		}
		return recordservice.FindResponse{Common: *args.Fail(status, err)}
	}
	return recordservice.FindResponse{Common: features.OK(), Result: res}
}
