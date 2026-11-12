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

// Search finds records once; records the principal may not read are left
// out.
func (s *service) Search(args recordservice.SearchArgs) recordservice.SearchResponse {
	resource := authz.Tenant(args.Principal)
	if args.Query.Entity != "" {
		resource = records.Resource(args.Principal, args.Query.Entity)
	}
	if err := authz.Check(args.Context(), args.Principal, "record.read", resource).Err("record.read", resource); err != nil {
		return recordservice.SearchResponse{Common: *args.Fail(authz.Status, err)}
	}
	res, err := data.SearchRecords(args.ServiceArgs, s.deps, args.Query)
	if err != nil {
		status := uint16(http.StatusInternalServerError)
		if o, ok := oops.AsOops(err); ok && o.Code() == "search_unavailable" {
			status = http.StatusServiceUnavailable
		}
		return recordservice.SearchResponse{Common: *args.Fail(status, err)}
	}
	return recordservice.SearchResponse{Common: features.OK(), Records: res.Records}
}
