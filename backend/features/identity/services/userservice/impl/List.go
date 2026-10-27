package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/userservice"
)

// List lists every user by username, with their tenants.
func (s *service) List(args userservice.ListArgs) userservice.ListResponse {
	if f := check(args.ServiceArgs, "user.list", 0); f != nil {
		return userservice.ListResponse{Common: *f}
	}
	tx := s.db.WithContext(args.Context())
	var users []identity.User
	if err := tx.Order("username").Find(&users).Error; err != nil {
		return userservice.ListResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	if err := identity.MarkSuperAdmins(tx, users); err != nil {
		return userservice.ListResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	var members []identity.Membership
	if err := s.db.WithContext(args.Context()).Order("tenant_id").Find(&members).Error; err != nil {
		return userservice.ListResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	tenants := map[uint64][]uint64{}
	for _, m := range members {
		tenants[m.UserID] = append(tenants[m.UserID], m.TenantID)
	}
	out := make([]userservice.Summary, len(users))
	for i, u := range users {
		out[i] = userservice.Summary{User: u, TenantIDs: tenants[u.ID]}
	}
	return userservice.ListResponse{Common: features.OK(), Users: out}
}
