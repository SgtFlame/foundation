package impl

import (
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/identityservice"
	"gorm.io/gorm"
)

// UpdateAccount changes the signed-in user's own profile or password. A
// new password needs the current one, ends every session of the user, and
// starts a new one here.
func (s *service) UpdateAccount(args identityservice.UpdateAccountArgs) identityservice.UpdateAccountResponse {
	if f := check(args.ServiceArgs, "account.update"); f != nil {
		return identityservice.UpdateAccountResponse{Common: *f}
	}
	if f := s.signedIn(args.ServiceArgs); f != nil {
		return identityservice.UpdateAccountResponse{Common: *f}
	}
	fail := func(err error) identityservice.UpdateAccountResponse {
		return identityservice.UpdateAccountResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	var u identity.User
	if err := s.db.WithContext(args.Context()).First(&u, args.Principal.UserID).Error; err != nil {
		return fail(err)
	}
	updates := map[string]any{}
	if args.DisplayName != nil {
		name, err := identity.Name("display name", *args.DisplayName)
		if err != nil {
			return identityservice.UpdateAccountResponse{Common: *invalid(args.ServiceArgs, err)}
		}
		updates["display_name"], u.DisplayName = name, name
	}
	if args.Email != nil {
		email, err := identity.Email(*args.Email)
		if err != nil {
			return identityservice.UpdateAccountResponse{Common: *invalid(args.ServiceArgs, err)}
		}
		updates["email"], u.Email = email, email
	}
	if args.NewPassword != "" {
		if !identity.VerifyPassword(u.PasswordHash, args.CurrentPassword) {
			return identityservice.UpdateAccountResponse{Common: *args.Fail(http.StatusForbidden,
				log.Code("bad_credentials").Public("The current password is wrong").New("wrong current password"))}
		}
		if err := identity.CheckPassword(args.NewPassword); err != nil {
			return identityservice.UpdateAccountResponse{Common: *invalid(args.ServiceArgs, err)}
		}
		hash, err := identity.HashPassword(args.NewPassword)
		if err != nil {
			return fail(err)
		}
		updates["password_hash"], u.PasswordHash = hash, hash
	}

	var session *identity.Session
	err := s.db.WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		if len(updates) > 0 {
			if err := tx.Model(&identity.User{}).Where("id = ?", u.ID).Updates(updates).Error; err != nil {
				return err
			}
		}
		if args.NewPassword == "" {
			return nil
		}
		if err := s.sessions.RevokeUser(tx, u.ID, 0); err != nil {
			return err
		}
		started, err := s.sessions.In(tx).Start(args.Context(), u, args.Principal.TenantID)
		session = &started
		return err
	})
	if err != nil {
		return fail(err)
	}
	account, err := s.account(s.db.WithContext(args.Context()), u, args.Principal.TenantID)
	if err != nil {
		return fail(err)
	}
	return identityservice.UpdateAccountResponse{Common: features.OK(), Account: account, Session: session}
}
