package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
)

// Resolve is Resolved for another process's config.Read.
func (s *service) Resolve(args configservice.ResolveArgs) configservice.ResolveResponse {
	raw, err := s.Resolved(args.ServiceArgs, args.Section)
	if err != nil {
		return configservice.ResolveResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return configservice.ResolveResponse{Common: features.OK(), Value: raw}
}
