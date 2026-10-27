package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/tenantservice"
)

// Rename changes a tenant's name.
func (s *service) Rename(args tenantservice.RenameArgs) tenantservice.RenameResponse {
	if f := check(args.ServiceArgs, "tenant.update", args.TenantID); f != nil {
		return tenantservice.RenameResponse{Common: *f}
	}
	name, err := identity.Name("tenant name", args.Name)
	if err != nil {
		return tenantservice.RenameResponse{Common: *invalid(args.ServiceArgs, err)}
	}
	t, f := s.find(args.ServiceArgs, args.TenantID)
	if f != nil {
		return tenantservice.RenameResponse{Common: *f}
	}
	t.Name = name
	if err := s.db.WithContext(args.Context()).Model(&t).Update("name", name).Error; err != nil {
		return tenantservice.RenameResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return tenantservice.RenameResponse{Common: features.OK(), Tenant: t}
}
