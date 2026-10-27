package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/tenantservice"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AddMember puts a user in a tenant with identity.DefaultMemberRole; adding
// a member again does nothing.
func (s *service) AddMember(args tenantservice.AddMemberArgs) tenantservice.AddMemberResponse {
	if f := check(args.ServiceArgs, "tenant.addMember", args.TenantID); f != nil {
		return tenantservice.AddMemberResponse{Common: *f}
	}
	if _, f := s.find(args.ServiceArgs, args.TenantID); f != nil {
		return tenantservice.AddMemberResponse{Common: *f}
	}
	if _, f := s.findUser(args.ServiceArgs, args.UserID); f != nil {
		return tenantservice.AddMemberResponse{Common: *f}
	}
	err := s.db.WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		m := identity.Membership{TenantID: args.TenantID, UserID: args.UserID}
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&m)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			grant := authz.GrantArgs{UserID: args.UserID, RoleID: identity.DefaultMemberRole, Resource: authz.Resource("tenant", args.TenantID), By: args.Principal.UserID}
			if err := identity.Grant(args.Context(), tx, grant); err != nil {
				return err
			}
		}
		return tx.Model(&identity.User{}).Where("id = ?", args.UserID).UpdateColumn("policy_version", gorm.Expr("policy_version + 1")).Error
	})
	identity.Changed()
	if err != nil {
		return tenantservice.AddMemberResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return tenantservice.AddMemberResponse{Common: features.OK()}
}
