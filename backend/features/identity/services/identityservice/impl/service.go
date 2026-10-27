package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/identityservice"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"gorm.io/gorm"
)

type service struct {
	db       *gorm.DB
	auth     *identity.Authenticator
	sessions identity.Sessions
}

var log = logging.In("identity")

// check runs authz.Check for action on the application.
func check(args features.ServiceArgs, action string) *features.ServiceResponse {
	if err := authz.Check(args.Context(), args.Principal, action, "app").Err(action, "app"); err != nil {
		return args.Fail(authz.Status, err)
	}
	return nil
}

// signedIn fails unless a user is signed in, as single-user mode's
// implicit user has no account.
func (s *service) signedIn(args features.ServiceArgs) *features.ServiceResponse {
	if s.auth.SingleUser() || args.Principal.Kind != features.PrincipalUser {
		return args.Fail(http.StatusConflict, log.Code("no_account").Public("Set up accounts first").New("no user is signed in"))
	}
	return nil
}

// account is u's account in tenant.
func (s *service) account(tx *gorm.DB, u identity.User, tenant uint64) (identityservice.Account, error) {
	var tenants []identity.Tenant
	err := tx.Joins("JOIN memberships ON memberships.tenant_id = tenants.id").
		Where("memberships.user_id = ?", u.ID).Order("tenants.name").Find(&tenants).Error
	if err == nil {
		users := []identity.User{u}
		err = identity.MarkSuperAdmins(tx, users)
		u = users[0]
	}
	return identityservice.Account{User: u, TenantID: tenant, Tenants: tenants}, err
}

// rememberTenant records where u works, for their next sign-in.
func rememberTenant(tx *gorm.DB, u *identity.User, tenant uint64) error {
	if u.LastTenantID == tenant {
		return nil
	}
	u.LastTenantID = tenant
	return tx.Model(&identity.User{}).Where("id = ?", u.ID).Update("last_tenant_id", tenant).Error
}

// invalid is a 400 invalid_account with err's text, which names nothing
// private.
func invalid(args features.ServiceArgs, err error) *features.ServiceResponse {
	return args.Fail(http.StatusBadRequest, log.Code("invalid_account").Public(err.Error()).Wrap(err))
}
