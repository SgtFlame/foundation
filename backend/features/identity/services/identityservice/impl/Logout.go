package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/identity/services/identityservice"
)

// Logout ends the session of the refresh token; the handler clears the
// cookies whatever happens.
func (s *service) Logout(args identityservice.LogoutArgs) identityservice.LogoutResponse {
	if f := check(args.ServiceArgs, "session.logout"); f != nil {
		return identityservice.LogoutResponse{Common: *f}
	}
	if args.Token != "" {
		if err := s.sessions.End(args.Context(), args.Token); err != nil {
			return identityservice.LogoutResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
		}
	}
	return identityservice.LogoutResponse{Common: features.OK()}
}
