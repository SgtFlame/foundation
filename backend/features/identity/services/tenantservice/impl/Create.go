package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/tenantservice"
	"gorm.io/gorm"
)

// Create makes a tenant whose creator is its first member and its Owner
// (RBAC question 7).
func (s *service) Create(args tenantservice.CreateArgs) tenantservice.CreateResponse {
	if f := check(args.ServiceArgs, "tenant.create", 0); f != nil {
		return tenantservice.CreateResponse{Common: *f}
	}
	name, err := identity.Name("tenant name", args.Name)
	if err != nil {
		return tenantservice.CreateResponse{Common: *invalid(args.ServiceArgs, err)}
	}
	t := identity.Tenant{Name: name}
	err = s.db.WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&t).Error; err != nil {
			return err
		}
		creator := args.Principal.UserID
		if args.Principal.Kind != features.PrincipalUser || creator == 0 {
			return nil
		}
		var users int64
		if err := tx.Model(&identity.User{}).Where("id = ?", creator).Count(&users).Error; err != nil || users == 0 {
			return err // single-user mode's implicit user has no row to be a member with
		}
		if err := tx.Create(&identity.Membership{TenantID: t.ID, UserID: creator}).Error; err != nil {
			return err
		}
		return identity.Grant(args.Context(), tx, authz.GrantArgs{UserID: creator, RoleID: identity.CreatorRole, Resource: authz.Resource("tenant", t.ID), By: creator})
	})
	identity.Changed()
	if err != nil {
		return tenantservice.CreateResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	log.With("tenantId", t.ID).Info(args.Context(), "tenant created")
	return tenantservice.CreateResponse{Common: features.OK(), Tenant: t}
}
