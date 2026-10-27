package impl

import (
	"errors"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
	"gorm.io/gorm"
)

// DeleteRole removes one of the tenant's custom roles and every grant of it.
func (s *service) DeleteRole(args authzservice.DeleteRoleArgs) authzservice.DeleteRoleResponse {
	res := authz.Tenant(args.Principal)
	if f := check(args.ServiceArgs, "role.write", res); f != nil {
		return authzservice.DeleteRoleResponse{Common: *f}
	}
	if authz.IsBuiltin(args.RoleID) {
		return authzservice.DeleteRoleResponse{Common: *args.Fail(http.StatusConflict,
			log.Code("builtin_role").Public("Built-in roles cannot be deleted").New("built-in roles cannot be deleted"))}
	}
	err := s.pol.DB().WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		var row authz.CustomRole
		if err := tx.Where("tenant_id = ?", args.Principal.TenantID).First(&row, args.RoleID).Error; err != nil {
			return err
		}
		var grants []authz.RoleGrant
		if err := tx.Where("role_id = ?", row.ID).Find(&grants).Error; err != nil {
			return err
		}
		for _, g := range grants {
			if _, err := s.pol.Revoke(args.Context(), tx, g.ID, args.Principal.UserID); err != nil {
				return err
			}
		}
		return tx.Delete(&row).Error
	})
	s.pol.Changed()
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return authzservice.DeleteRoleResponse{Common: *args.Fail(http.StatusNotFound, log.Code("role_not_found").With("roleId", args.RoleID).New("there is no such role"))}
	case err != nil:
		return authzservice.DeleteRoleResponse{Common: *internal(args.ServiceArgs, err)}
	}
	return authzservice.DeleteRoleResponse{Common: features.OK()}
}
