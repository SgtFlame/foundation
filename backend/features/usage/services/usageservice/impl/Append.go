package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/usage"
	"github.com/SgtFlame/foundation/backend/features/usage/services/usageservice"
)

// onlyServices refuses a principal that is not a service's: records are
// appended by recorders, never by people.
func onlyServices(args features.ServiceArgs) *features.ServiceResponse {
	if args.Principal.Kind != features.PrincipalService {
		return args.Fail(http.StatusForbidden, log.Code("services_only").New("only services may append usage"))
	}
	return nil
}

func (s *service) Append(args usageservice.AppendArgs) usageservice.AppendResponse {
	if f := onlyServices(args.ServiceArgs); f != nil {
		return usageservice.AppendResponse{Common: *f}
	}
	for _, r := range args.Records {
		if r.ID == 0 {
			return usageservice.AppendResponse{Common: *args.Fail(http.StatusBadRequest, log.Code("record_without_id").New("usage records are appended with their ids"))}
		}
	}
	if err := usage.StoreIn(s.db).Append(args.Context(), args.Records, args.Captures); err != nil {
		return usageservice.AppendResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return usageservice.AppendResponse{Common: features.OK()}
}

func (s *service) PurgeCaptures(args usageservice.PurgeCapturesArgs) usageservice.PurgeCapturesResponse {
	if f := onlyServices(args.ServiceArgs); f != nil {
		return usageservice.PurgeCapturesResponse{Common: *f}
	}
	n, err := usage.StoreIn(s.db).PurgeCaptures(args.Context(), args.Before)
	if err != nil {
		return usageservice.PurgeCapturesResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return usageservice.PurgeCapturesResponse{Common: features.OK(), Deleted: n}
}
