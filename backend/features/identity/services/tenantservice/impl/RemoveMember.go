package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/tenantservice"
	"gorm.io/gorm"
)

// RemoveMember takes a user out of a tenant and ends their sessions there.
// Their access token keeps working until it expires, at most
// identity.AccessLifetime.
func (s *service) RemoveMember(args tenantservice.RemoveMemberArgs) tenantservice.RemoveMemberResponse {
	if f := check(args.ServiceArgs, "tenant.removeMember", args.TenantID); f != nil {
		return tenantservice.RemoveMemberResponse{Common: *f}
	}
	err := s.db.WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		return leave(args.ServiceArgs, tx, args.TenantID, args.UserID)
	})
	identity.Changed()
	if err != nil {
		return tenantservice.RemoveMemberResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return tenantservice.RemoveMemberResponse{Common: features.OK()}
}
