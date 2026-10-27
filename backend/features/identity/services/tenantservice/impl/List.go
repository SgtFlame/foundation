package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/tenantservice"
)

// List lists every tenant by name, with its member count. Without
// tenant.list it lists the principal's tenants whose members they manage
// (tenant.addMember: an Owner or Administrator), and fails when there are
// none.
func (s *service) List(args tenantservice.ListArgs) tenantservice.ListResponse {
	denied := check(args.ServiceArgs, "tenant.list", 0)
	var out []tenantservice.Summary
	err := s.db.WithContext(args.Context()).Model(&identity.Tenant{}).
		Select("tenants.*, (SELECT COUNT(*) FROM memberships WHERE memberships.tenant_id = tenants.id) AS members").
		Order("tenants.name").Scan(&out).Error
	if err != nil {
		return tenantservice.ListResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	if denied != nil {
		managed := out[:0]
		for _, t := range out {
			if t.ID == args.Principal.TenantID && authz.Check(args.Context(), args.Principal, "tenant.addMember", authz.Resource("tenant", t.ID)).Allowed {
				managed = append(managed, t)
			}
		}
		if len(managed) == 0 {
			return tenantservice.ListResponse{Common: *denied}
		}
		out = managed
	}
	return tenantservice.ListResponse{Common: features.OK(), Tenants: out}
}
