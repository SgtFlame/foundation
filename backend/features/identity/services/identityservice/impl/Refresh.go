package impl

import (
	"errors"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/identityservice"
)

// Refresh rotates the session's refresh token. Anyone may call it: the
// token is the credential.
func (s *service) Refresh(args identityservice.RefreshArgs) identityservice.RefreshResponse {
	if f := check(args.ServiceArgs, "session.refresh"); f != nil {
		return identityservice.RefreshResponse{Common: *f}
	}
	if args.Token == "" {
		return identityservice.RefreshResponse{Common: *sessionEnded(args.ServiceArgs, errors.New("no refresh token"))}
	}
	session, err := s.sessions.Rotate(args.Context(), args.Token)
	switch {
	case errors.Is(err, identity.ErrReused):
		log.Warn(args.Context(), "a replaced refresh token was used again; its session is revoked")
		return identityservice.RefreshResponse{Common: *sessionEnded(args.ServiceArgs, err)}
	case errors.Is(err, identity.ErrBadRefresh):
		return identityservice.RefreshResponse{Common: *sessionEnded(args.ServiceArgs, err)}
	case err != nil:
		return identityservice.RefreshResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return identityservice.RefreshResponse{Common: features.OK(), Session: session}
}

func sessionEnded(args features.ServiceArgs, err error) *features.ServiceResponse {
	return args.Fail(http.StatusUnauthorized, log.Code("session_ended").Public("Your session has ended; sign in again").Wrap(err))
}
