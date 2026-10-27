package impl

import (
	"slices"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
)

// Roles is the built-in roles, then the tenant's custom roles by name.
func (s *service) Roles(args authzservice.RolesArgs) authzservice.RolesResponse {
	res := authz.Tenant(args.Principal)
	if f := check(args.ServiceArgs, "role.read", res); f != nil {
		return authzservice.RolesResponse{Common: *f}
	}
	all, err := s.pol.Roles()
	if err != nil {
		return authzservice.RolesResponse{Common: *internal(args.ServiceArgs, err)}
	}
	out := authz.Builtins(s.pol.Registry())
	var custom []authz.Role
	for _, r := range all {
		if !r.BuiltIn && r.TenantID == args.Principal.TenantID {
			custom = append(custom, r)
		}
	}
	slices.SortFunc(custom, func(a, b authz.Role) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return authzservice.RolesResponse{Common: features.OK(), Roles: append(out, custom...)}
}
