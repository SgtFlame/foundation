package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/tenantservice"
)

// Members lists the tenant's users by name.
func (s *service) Members(args tenantservice.MembersArgs) tenantservice.MembersResponse {
	if f := check(args.ServiceArgs, "tenant.read", args.TenantID); f != nil {
		return tenantservice.MembersResponse{Common: *f}
	}
	if _, f := s.find(args.ServiceArgs, args.TenantID); f != nil {
		return tenantservice.MembersResponse{Common: *f}
	}
	var users []identity.User
	err := s.db.WithContext(args.Context()).Joins("JOIN memberships ON memberships.user_id = users.id").
		Where("memberships.tenant_id = ?", args.TenantID).Order("users.username").Find(&users).Error
	if err != nil {
		return tenantservice.MembersResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	tx := s.db.WithContext(args.Context())
	ids := make([]uint64, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}
	roles, err := identity.RolesOn(authz.Resource("tenant", args.TenantID), ids)
	if err == nil {
		err = identity.MarkSuperAdmins(tx, users)
	}
	if err != nil {
		return tenantservice.MembersResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return tenantservice.MembersResponse{Common: features.OK(), Users: users, Roles: roles}
}
