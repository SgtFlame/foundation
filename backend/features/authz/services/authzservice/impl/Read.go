package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
)

// onlyServices refuses a principal that is not a service's: grants are
// read for other processes' policies, never for people.
func onlyServices(args features.ServiceArgs) *features.ServiceResponse {
	if args.Principal.Kind != features.PrincipalService {
		return args.Fail(http.StatusForbidden, log.Code("services_only").New("only services may read grants this way"))
	}
	return nil
}

func (s *service) ReadGrants(args authzservice.ReadGrantsArgs) authzservice.ReadGrantsResponse {
	if f := onlyServices(args.ServiceArgs); f != nil {
		return authzservice.ReadGrantsResponse{Common: *f}
	}
	grants, err := s.pol.ReadSource().Grants(args.Context(), args.Agent, args.ID)
	if err != nil {
		return authzservice.ReadGrantsResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return authzservice.ReadGrantsResponse{Common: features.OK(), Grants: grants}
}

func (s *service) ReadRoles(args authzservice.ReadRolesArgs) authzservice.ReadRolesResponse {
	if f := onlyServices(args.ServiceArgs); f != nil {
		return authzservice.ReadRolesResponse{Common: *f}
	}
	roles, err := s.pol.ReadSource().CustomRoles(args.Context())
	if err != nil {
		return authzservice.ReadRolesResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return authzservice.ReadRolesResponse{Common: features.OK(), Roles: roles}
}
