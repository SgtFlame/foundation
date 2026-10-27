package impl

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
)

// Grants lists the grants on a resource, or of a role: an app role's on
// the application, a tenant role's within the principal's tenant.
func (s *service) Grants(args authzservice.GrantsArgs) authzservice.GrantsResponse {
	resource, under := args.Resource, args.Under
	if args.RoleID != 0 {
		roles, err := s.pol.Roles()
		if err != nil {
			return authzservice.GrantsResponse{Common: *internal(args.ServiceArgs, err)}
		}
		resource, under = authz.Tenant(args.Principal), true
		if roles[args.RoleID].Scope == authz.OnApp {
			resource, under = authz.OnApp, false
		}
	}
	if !inTenant(args.Principal, resource) {
		return authzservice.GrantsResponse{Common: *outside(args.ServiceArgs, resource)}
	}
	if f := check(args.ServiceArgs, "grant.read", resource); f != nil {
		return authzservice.GrantsResponse{Common: *f}
	}
	grants, err := s.pol.GrantsOn(nil, resource, under)
	if err != nil {
		return authzservice.GrantsResponse{Common: *internal(args.ServiceArgs, err)}
	}
	if args.RoleID != 0 {
		kept := grants[:0]
		for _, g := range grants {
			if g.RoleID == args.RoleID {
				kept = append(kept, g)
			}
		}
		grants = kept
	}
	return authzservice.GrantsResponse{Common: features.OK(), Grants: grants}
}
