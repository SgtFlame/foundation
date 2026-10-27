package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
)

// Me is the principal's effective permissions. Everyone may ask about
// themselves.
func (s *service) Me(args authzservice.MeArgs) authzservice.MeResponse {
	e, err := s.pol.EffectiveFor(args.Principal)
	if err != nil {
		return authzservice.MeResponse{Common: *internal(args.ServiceArgs, err)}
	}
	return authzservice.MeResponse{Common: features.OK(), Effective: e}
}
