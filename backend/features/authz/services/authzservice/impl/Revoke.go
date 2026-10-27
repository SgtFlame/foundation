package impl

import (
	"errors"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
	"gorm.io/gorm"
)

// Revoke removes a grant in the principal's tenant (or on the
// application). Revoking needs what granting it would.
func (s *service) Revoke(args authzservice.RevokeArgs) authzservice.RevokeResponse {
	var g authz.RoleGrant
	err := s.pol.DB().WithContext(args.Context()).First(&g, args.GrantID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && !inTenant(args.Principal, g.Resource) {
		return authzservice.RevokeResponse{Common: *args.Fail(http.StatusNotFound, log.Code("grant_not_found").With("grantId", args.GrantID).New("there is no such grant"))}
	}
	if err != nil {
		return authzservice.RevokeResponse{Common: *internal(args.ServiceArgs, err)}
	}
	roles, err := s.pol.Roles()
	if err != nil {
		return authzservice.RevokeResponse{Common: *internal(args.ServiceArgs, err)}
	}
	if d := s.pol.MayGrant(args.Context(), args.Principal, roles[g.RoleID], g.Resource); !d.Allowed {
		return authzservice.RevokeResponse{Common: *args.Fail(authz.Status, d.Err("grant.write", g.Resource))}
	}
	if _, err := s.pol.Revoke(args.Context(), nil, g.ID, args.Principal.UserID); err != nil {
		if errors.Is(err, authz.ErrLastSuperUser) {
			return authzservice.RevokeResponse{Common: *args.Fail(http.StatusConflict,
				log.Code("last_admin").Public("One Super Administrator must remain").Wrap(err))}
		}
		return authzservice.RevokeResponse{Common: *internal(args.ServiceArgs, err)}
	}
	return authzservice.RevokeResponse{Common: features.OK()}
}
