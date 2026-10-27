package impl

import (
	"errors"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/identityservice"
	"gorm.io/gorm"
)

var errSetupDone = errors.New("accounts are already set up")

// Setup makes the default tenant and its Super Administrator with the reserved
// IDs, so everything written in single-user mode becomes theirs, and signs
// them in. It works once: while no user exists.
func (s *service) Setup(args identityservice.SetupArgs) identityservice.SetupResponse {
	if f := check(args.ServiceArgs, "session.setup"); f != nil {
		return identityservice.SetupResponse{Common: *f}
	}
	tenantName, err := identity.Name("tenant name", args.TenantName)
	if err != nil {
		return identityservice.SetupResponse{Common: *invalid(args.ServiceArgs, err)}
	}
	username, err := identity.Username(args.Username)
	if err != nil {
		return identityservice.SetupResponse{Common: *invalid(args.ServiceArgs, err)}
	}
	email, err := identity.Email(args.Email)
	if err != nil {
		return identityservice.SetupResponse{Common: *invalid(args.ServiceArgs, err)}
	}
	if err := identity.CheckPassword(args.Password); err != nil {
		return identityservice.SetupResponse{Common: *invalid(args.ServiceArgs, err)}
	}
	display := args.DisplayName
	if display == "" {
		display = username
	}
	if display, err = identity.Name("display name", display); err != nil {
		return identityservice.SetupResponse{Common: *invalid(args.ServiceArgs, err)}
	}
	hash, err := identity.HashPassword(args.Password)
	if err != nil {
		return identityservice.SetupResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}

	u := identity.User{
		Model: db.Model{ID: features.LocalUserID}, Username: username, DisplayName: display, Email: email,
		PasswordHash: hash, SuperAdmin: true, LastTenantID: features.LocalTenantID,
	}
	var resp identityservice.SetupResponse
	err = s.db.WithContext(args.Context()).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&identity.User{}).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return errSetupDone
		}
		tenant := identity.Tenant{Model: db.Model{ID: features.LocalTenantID}, Name: tenantName}
		if err := tx.Save(&tenant).Error; err != nil { // kept if an earlier setup left it
			return err
		}
		if err := tx.Create(&u).Error; err != nil {
			return err
		}
		if err := tx.Create(&identity.Membership{TenantID: tenant.ID, UserID: u.ID}).Error; err != nil {
			return err
		}
		// Single-user mode's grants become the first user's; made again in
		// case an earlier start found users
		if pol := authz.Installed(); pol != nil {
			if err := identity.Bootstrap(args.Context(), tx, pol); err != nil {
				return err
			}
		}
		session, err := s.sessions.In(tx).Start(args.Context(), u, tenant.ID)
		if err != nil {
			return err
		}
		resp.Session = session
		resp.Account = identityservice.Account{User: u, TenantID: tenant.ID, Tenants: []identity.Tenant{tenant}}
		return nil
	})
	if errors.Is(err, errSetupDone) {
		return identityservice.SetupResponse{Common: *args.Fail(http.StatusConflict, log.Code("setup_done").Public("Accounts are already set up; sign in").Wrap(err))}
	}
	if err != nil {
		return identityservice.SetupResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	identity.Changed()
	s.auth.UsersExist()
	log.With("userId", u.ID).Info(args.Context(), "accounts set up: single-user mode has ended")
	resp.Common = features.OK()
	return resp
}
