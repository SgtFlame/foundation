package impl

import (
	"errors"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"gorm.io/gorm"
)

type service struct {
	db *gorm.DB
}

var log = logging.In("identity").Tags("tenant")

// check runs authz.Check for action on the tenant (0: the application).
func check(args features.ServiceArgs, action string, tenant uint64) *features.ServiceResponse {
	res := "app"
	if tenant != 0 {
		res = authz.Resource("tenant", tenant)
	}
	if err := authz.Check(args.Context(), args.Principal, action, res).Err(action, res); err != nil {
		return args.Fail(authz.Status, err)
	}
	return nil
}

// find reads the tenant, failing with 404 tenant_not_found.
func (s *service) find(args features.ServiceArgs, id uint64) (identity.Tenant, *features.ServiceResponse) {
	var t identity.Tenant
	err := s.db.WithContext(args.Context()).First(&t, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return t, args.Fail(http.StatusNotFound, log.Code("tenant_not_found").With("tenantId", id).New("there is no such tenant"))
	}
	if err != nil {
		return t, args.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	return t, nil
}

// findUser reads the user, failing with 404 user_not_found.
func (s *service) findUser(args features.ServiceArgs, id uint64) (identity.User, *features.ServiceResponse) {
	var u identity.User
	err := s.db.WithContext(args.Context()).First(&u, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return u, args.Fail(http.StatusNotFound, log.Code("user_not_found").With("userId", id).New("there is no such user"))
	}
	if err != nil {
		return u, args.Fail(http.StatusInternalServerError, log.Wrap(err))
	}
	return u, nil
}

// invalid is a 400 invalid_tenant with err's text.
func invalid(args features.ServiceArgs, err error) *features.ServiceResponse {
	return args.Fail(http.StatusBadRequest, log.Code("invalid_tenant").Public(err.Error()).Wrap(err))
}

// leave removes the user from the tenant inside tx, with their grants
// there, ending their sessions there and raising their policy version.
func leave(args features.ServiceArgs, tx *gorm.DB, tenant, user uint64) error {
	if err := tx.Where("tenant_id = ? AND user_id = ?", tenant, user).Delete(&identity.Membership{}).Error; err != nil {
		return err
	}
	if err := identity.RevokeUnder(args.Context(), tx, user, authz.Resource("tenant", tenant), args.Principal.UserID); err != nil {
		return err
	}
	if err := (identity.Sessions{}).RevokeUser(tx, user, tenant); err != nil {
		return err
	}
	return tx.Model(&identity.User{}).Where("id = ?", user).UpdateColumn("policy_version", gorm.Expr("policy_version + 1")).Error
}
