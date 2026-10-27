package impl

import (
	"errors"
	"net/http"
	"slices"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/userservice"
	"gorm.io/gorm"
)

var errLastAdmin = errors.New("one enabled Super Administrator must remain")

// Update changes a user's profile, Super Administrator grant, disabled flag
// or password. A change to what they may do or a new password ends their
// sessions.
func (s *service) Update(args userservice.UpdateArgs) userservice.UpdateResponse {
	if f := check(args.ServiceArgs, "user.update", args.UserID); f != nil {
		return userservice.UpdateResponse{Common: *f}
	}
	self := args.Principal.UserID == args.UserID
	if self && ((args.SuperAdmin != nil && !*args.SuperAdmin) || (args.Disabled != nil && *args.Disabled)) {
		return userservice.UpdateResponse{Common: *args.Fail(http.StatusConflict,
			log.Code("own_account").Public("You cannot take away your own Super Administrator role or disable yourself").New("a Super Administrator tried to demote or disable themselves"))}
	}
	updates := map[string]any{}
	if args.DisplayName != nil {
		name, err := identity.Name("display name", *args.DisplayName)
		if err != nil {
			return userservice.UpdateResponse{Common: *invalid(args.ServiceArgs, err)}
		}
		updates["display_name"] = name
	}
	if args.Email != nil {
		email, err := identity.Email(*args.Email)
		if err != nil {
			return userservice.UpdateResponse{Common: *invalid(args.ServiceArgs, err)}
		}
		updates["email"] = email
	}
	if args.Password != "" {
		if err := identity.CheckPassword(args.Password); err != nil {
			return userservice.UpdateResponse{Common: *invalid(args.ServiceArgs, err)}
		}
		hash, err := identity.HashPassword(args.Password)
		if err != nil {
			return userservice.UpdateResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
		}
		updates["password_hash"] = hash
	}

	var out userservice.Summary
	err := s.db.WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		var u identity.User
		if err := tx.First(&u, args.UserID).Error; err != nil {
			return err
		}
		mayChange := false
		supers, err := identity.SuperAdmins(tx)
		if err != nil {
			return err
		}
		if args.SuperAdmin != nil && *args.SuperAdmin != slices.Contains(supers, u.ID) {
			if err := s.setSuperAdmin(args, tx, *args.SuperAdmin); err != nil {
				return err
			}
			mayChange = true
		}
		if args.Disabled != nil && *args.Disabled != u.Disabled {
			updates["disabled"], mayChange = *args.Disabled, true
		}
		if mayChange {
			updates["policy_version"] = u.PolicyVersion + 1
		}
		if len(updates) == 0 {
			out, _ = summary(tx, u)
			return nil
		}
		if err := tx.Model(&u).Updates(updates).Error; err != nil {
			return err
		}
		if supers, err = identity.SuperAdmins(tx); err != nil {
			return err
		}
		if len(supers) > 0 { // none without a policy (tests of identity alone)
			var enabled int64
			if err := tx.Model(&identity.User{}).Where("id IN ? AND NOT disabled", supers).Count(&enabled).Error; err != nil {
				return err
			}
			if enabled == 0 {
				return errLastAdmin
			}
		}
		if mayChange || args.Password != "" {
			if err := (identity.Sessions{}).RevokeUser(tx, u.ID, 0); err != nil {
				return err
			}
		}
		if err := tx.First(&u, args.UserID).Error; err != nil {
			return err
		}
		out, err = summary(tx, u)
		return err
	})
	identity.Changed()
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return userservice.UpdateResponse{Common: *args.Fail(http.StatusNotFound, log.Code("user_not_found").With("userId", args.UserID).New("there is no such user"))}
	case errors.Is(err, errLastAdmin):
		return userservice.UpdateResponse{Common: *args.Fail(http.StatusConflict, log.Code("last_admin").Public("One enabled Super Administrator must remain").Wrap(err))}
	case err != nil:
		return userservice.UpdateResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	return userservice.UpdateResponse{Common: features.OK(), User: out}
}

// setSuperAdmin grants or revokes the user's Super Administrator role
// inside tx.
func (s *service) setSuperAdmin(args userservice.UpdateArgs, tx *gorm.DB, on bool) error {
	pol := authz.Installed()
	if pol == nil {
		return nil
	}
	if on {
		_, err := pol.Grant(args.Context(), tx, authz.GrantArgs{UserID: args.UserID, RoleID: authz.RoleSuperAdmin, Resource: authz.OnApp, By: args.Principal.UserID})
		return err
	}
	grants, err := pol.GrantsOn(tx, authz.OnApp, false)
	if err != nil {
		return err
	}
	for _, g := range grants {
		if g.UserID == args.UserID && g.RoleID == authz.RoleSuperAdmin {
			if _, err := pol.Revoke(args.Context(), tx, g.ID, args.Principal.UserID); err != nil {
				if errors.Is(err, authz.ErrLastSuperUser) {
					return errLastAdmin
				}
				return err
			}
		}
	}
	return nil
}
