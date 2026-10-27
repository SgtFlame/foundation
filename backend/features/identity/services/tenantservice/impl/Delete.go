package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/tenantservice"
	"gorm.io/gorm"
)

// Delete removes a tenant, its memberships and its sessions. Its content is
// left where it is: purging a deleted tenant's rows and LanceDB directory
// comes later.
func (s *service) Delete(args tenantservice.DeleteArgs) tenantservice.DeleteResponse {
	if f := check(args.ServiceArgs, "tenant.delete", args.TenantID); f != nil {
		return tenantservice.DeleteResponse{Common: *f}
	}
	if args.TenantID == features.LocalTenantID {
		return tenantservice.DeleteResponse{Common: *args.Fail(http.StatusConflict,
			log.Code("default_tenant").Public("The default tenant cannot be deleted").New("the default tenant cannot be deleted"))}
	}
	if _, f := s.find(args.ServiceArgs, args.TenantID); f != nil {
		return tenantservice.DeleteResponse{Common: *f}
	}
	err := s.db.WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		var members []uint64
		if err := tx.Model(&identity.Membership{}).Where("tenant_id = ?", args.TenantID).Pluck("user_id", &members).Error; err != nil {
			return err
		}
		for _, u := range members {
			if err := leave(args.ServiceArgs, tx, args.TenantID, u); err != nil {
				return err
			}
		}
		if err := identity.RevokeUnder(args.Context(), tx, 0, authz.Resource("tenant", args.TenantID), args.Principal.UserID); err != nil {
			return err
		}
		return tx.Delete(&identity.Tenant{}, args.TenantID).Error
	})
	if err != nil {
		return tenantservice.DeleteResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	identity.Changed()
	log.With("tenantId", args.TenantID).Info(args.Context(), "tenant deleted")
	return tenantservice.DeleteResponse{Common: features.OK()}
}
