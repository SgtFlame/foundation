package impl

import (
	"net/http"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/userservice"
)

// Names gives the names of the tenant's members among IDs.
func (s *service) Names(args userservice.NamesArgs) userservice.NamesResponse {
	p := args.Principal
	res := authz.Tenant(p)
	if err := authz.Check(args.Context(), p, "tenant.read", res).Err("tenant.read", res); err != nil {
		return userservice.NamesResponse{Common: *args.Fail(authz.Status, err)}
	}
	out := map[string]userservice.Name{}
	if len(args.IDs) == 0 {
		return userservice.NamesResponse{Common: features.OK(), Users: out}
	}
	var users []identity.User
	err := s.db.WithContext(args.Context()).Select("users.id", "users.username", "users.display_name").
		Joins("JOIN memberships ON memberships.user_id = users.id AND memberships.tenant_id = ?", p.TenantID).
		Where("users.id IN ?", args.IDs).Find(&users).Error
	if err != nil {
		return userservice.NamesResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	for _, u := range users {
		out[strconv.FormatUint(u.ID, 10)] = userservice.Name{Username: u.Username, DisplayName: u.DisplayName}
	}
	return userservice.NamesResponse{Common: features.OK(), Users: out}
}
