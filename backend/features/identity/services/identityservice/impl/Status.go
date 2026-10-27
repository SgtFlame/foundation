package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/identityservice"
)

// Status says whether sign-in is required and who is signed in. Anyone may
// ask, signed in or not.
func (s *service) Status(args identityservice.StatusArgs) identityservice.StatusResponse {
	if f := check(args.ServiceArgs, "session.status"); f != nil {
		return identityservice.StatusResponse{Common: *f}
	}
	if s.auth.SingleUser() {
		return identityservice.StatusResponse{Common: features.OK(), SingleUser: true, SignedIn: true}
	}
	if args.Principal.Kind != features.PrincipalUser {
		return identityservice.StatusResponse{Common: features.OK()}
	}
	tx := s.db.WithContext(args.Context())
	var u identity.User
	if err := tx.First(&u, args.Principal.UserID).Error; err != nil {
		return identityservice.StatusResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	account, err := s.account(tx, u, args.Principal.TenantID)
	if err != nil {
		return identityservice.StatusResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return identityservice.StatusResponse{Common: features.OK(), SignedIn: true, Account: &account}
}
