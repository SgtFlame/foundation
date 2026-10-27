package impl

import (
	"errors"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/userservice"
	"gorm.io/gorm"
)

// Create makes a user with a password the administrator gives them, in at
// least one tenant.
func (s *service) Create(args userservice.CreateArgs) userservice.CreateResponse {
	if f := check(args.ServiceArgs, "user.create", 0); f != nil {
		return userservice.CreateResponse{Common: *f}
	}
	username, err := identity.Username(args.Username)
	if err != nil {
		return userservice.CreateResponse{Common: *invalid(args.ServiceArgs, err)}
	}
	display := args.DisplayName
	if display == "" {
		display = username
	}
	if display, err = identity.Name("display name", display); err != nil {
		return userservice.CreateResponse{Common: *invalid(args.ServiceArgs, err)}
	}
	email, err := identity.Email(args.Email)
	if err != nil {
		return userservice.CreateResponse{Common: *invalid(args.ServiceArgs, err)}
	}
	if err := identity.CheckPassword(args.Password); err != nil {
		return userservice.CreateResponse{Common: *invalid(args.ServiceArgs, err)}
	}
	hash, err := identity.HashPassword(args.Password)
	if err != nil {
		return userservice.CreateResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}

	u := identity.User{Username: username, DisplayName: display, Email: email, PasswordHash: hash}
	var out userservice.Summary
	err = s.db.WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		if len(args.TenantIDs) == 0 {
			return errNoTenants
		}
		var taken int64
		if err := tx.Model(&identity.User{}).Where("username = ?", username).Count(&taken).Error; err != nil {
			return err
		}
		if taken > 0 {
			return errTaken
		}
		var found int64
		if err := tx.Model(&identity.Tenant{}).Where("id IN ?", args.TenantIDs).Count(&found).Error; err != nil {
			return err
		}
		if int(found) != len(args.TenantIDs) {
			return errNoTenant
		}
		u.LastTenantID = args.TenantIDs[0]
		if err := tx.Create(&u).Error; err != nil {
			return err
		}
		for _, t := range args.TenantIDs {
			if err := tx.Create(&identity.Membership{TenantID: t, UserID: u.ID}).Error; err != nil {
				return err
			}
			grant := authz.GrantArgs{UserID: u.ID, RoleID: identity.DefaultMemberRole, Resource: authz.Resource("tenant", t), By: args.Principal.UserID}
			if err := identity.Grant(args.Context(), tx, grant); err != nil {
				return err
			}
		}
		if args.SuperAdmin {
			grant := authz.GrantArgs{UserID: u.ID, RoleID: authz.RoleSuperAdmin, Resource: authz.OnApp, By: args.Principal.UserID}
			if err := identity.Grant(args.Context(), tx, grant); err != nil {
				return err
			}
			u.SuperAdmin = true
		}
		out, err = summary(tx, u)
		return err
	})
	switch {
	case errors.Is(err, errNoTenants), errors.Is(err, errNoTenant):
		return userservice.CreateResponse{Common: *invalid(args.ServiceArgs, err)}
	case errors.Is(err, errTaken):
		return userservice.CreateResponse{Common: *args.Fail(http.StatusConflict, log.Code("username_taken").Public("That username is taken").Wrap(err))}
	case err != nil:
		return userservice.CreateResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	identity.Changed()
	log.With("userId", u.ID).Info(args.Context(), "user created")
	return userservice.CreateResponse{Common: features.OK(), User: out}
}

var errTaken = errors.New("the username is taken")
