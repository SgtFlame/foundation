package impl

import (
	"errors"
	"net/http"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
)

// Grant gives a role on a resource in the principal's tenant (or on the
// application) to one of that tenant's members.
func (s *service) Grant(args authzservice.GrantArgs) authzservice.GrantResponse {
	if !inTenant(args.Principal, args.Resource) {
		return authzservice.GrantResponse{Common: *outside(args.ServiceArgs, args.Resource)}
	}
	roles, err := s.pol.Roles()
	if err != nil {
		return authzservice.GrantResponse{Common: *internal(args.ServiceArgs, err)}
	}
	role, ok := roles[args.RoleID]
	if !ok || (!role.BuiltIn && role.TenantID != args.Principal.TenantID) {
		return authzservice.GrantResponse{Common: *args.Fail(http.StatusNotFound, log.Code("role_not_found").With("roleId", args.RoleID).New("there is no such role"))}
	}
	if d := s.pol.MayGrant(args.Context(), args.Principal, role, args.Resource); !d.Allowed {
		return authzservice.GrantResponse{Common: *args.Fail(authz.Status, d.Err("grant.write", args.Resource))}
	}
	if t := authz.TenantOf(args.Resource); t != 0 {
		member, err := s.member(t, args.UserID)
		if err != nil {
			return authzservice.GrantResponse{Common: *internal(args.ServiceArgs, err)}
		}
		if !member {
			return authzservice.GrantResponse{Common: *args.Fail(http.StatusBadRequest,
				log.Code("not_member").Public("Only members of the tenant can be given roles in it").New("the user is not a member"))}
		}
	}
	if args.ExpiresAt != nil && !args.ExpiresAt.After(time.Now()) {
		return authzservice.GrantResponse{Common: *args.Fail(http.StatusBadRequest,
			log.Code("invalid_grant").Public("The expiry must be in the future").New("expiry in the past"))}
	}
	g, err := s.pol.Grant(args.Context(), nil, authz.GrantArgs{UserID: args.UserID, RoleID: args.RoleID, Resource: args.Resource, By: args.Principal.UserID, ExpiresAt: args.ExpiresAt})
	if errors.Is(err, authz.ErrWrongScope) {
		return authzservice.GrantResponse{Common: *args.Fail(http.StatusBadRequest,
			log.Code("invalid_grant").Public("That role cannot be granted here").Wrap(err))}
	}
	if err != nil {
		return authzservice.GrantResponse{Common: *internal(args.ServiceArgs, err)}
	}
	return authzservice.GrantResponse{Common: features.OK(), Grant: g}
}
