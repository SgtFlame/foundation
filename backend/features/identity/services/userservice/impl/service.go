package impl

import (
	"errors"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/userservice"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"gorm.io/gorm"
)

type service struct {
	db *gorm.DB
}

var log = logging.In("identity").Tags("user")

// check runs authz.Check for action on the user (0: the application).
func check(args features.ServiceArgs, action string, user uint64) *features.ServiceResponse {
	res := "app"
	if user != 0 {
		res = authz.Resource("user", user)
	}
	if err := authz.Check(args.Context(), args.Principal, action, res).Err(action, res); err != nil {
		return args.Fail(authz.Status, err)
	}
	return nil
}

// invalid is a 400 invalid_user with err's text.
func invalid(args features.ServiceArgs, err error) *features.ServiceResponse {
	return args.Fail(http.StatusBadRequest, log.Code("invalid_user").Public(err.Error()).Wrap(err))
}

// summary is u with their tenants and whether they are a Super
// Administrator.
func summary(tx *gorm.DB, u identity.User) (userservice.Summary, error) {
	users := []identity.User{u}
	if err := identity.MarkSuperAdmins(tx, users); err != nil {
		return userservice.Summary{}, err
	}
	out := userservice.Summary{User: users[0]}
	err := tx.Model(&identity.Membership{}).Where("user_id = ?", u.ID).Order("tenant_id").Pluck("tenant_id", &out.TenantIDs).Error
	return out, err
}

// errNoTenant and errNoTenants are refusals inside a transaction.
var (
	errNoTenants = errors.New("a user needs at least one tenant")
	errNoTenant  = errors.New("there is no such tenant")
)
