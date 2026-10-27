package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/identityservice"
)

// SwitchTenant ends the current session and starts one in another of the
// user's tenants.
func (s *service) SwitchTenant(args identityservice.SwitchTenantArgs) identityservice.SwitchTenantResponse {
	if f := check(args.ServiceArgs, "account.switchTenant"); f != nil {
		return identityservice.SwitchTenantResponse{Common: *f}
	}
	if f := s.signedIn(args.ServiceArgs); f != nil {
		return identityservice.SwitchTenantResponse{Common: *f}
	}
	tx := s.db.WithContext(args.Context())
	var u identity.User
	if err := tx.First(&u, args.Principal.UserID).Error; err != nil {
		return identityservice.SwitchTenantResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	tenant, ok, err := identity.TenantFor(tx, u, args.TenantID)
	if err != nil {
		return identityservice.SwitchTenantResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	if !ok || tenant != args.TenantID {
		return identityservice.SwitchTenantResponse{Common: *args.Fail(http.StatusNotFound,
			log.Code("tenant_not_found").With("tenantId", args.TenantID).Public("You are not a member of that tenant").New("not a member of the tenant"))}
	}
	if args.Token != "" {
		if err := s.sessions.End(args.Context(), args.Token); err != nil {
			return identityservice.SwitchTenantResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
		}
	}
	resp := s.begin(args.ServiceArgs, u, tenant)
	return identityservice.SwitchTenantResponse{Common: resp.Common, Session: resp.Session, Account: resp.Account}
}
