package impl

import (
	"errors"
	"net/http"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/identity/services/identityservice"
	"gorm.io/gorm"
)

// Login checks a username and password and starts a session in the tenant
// asked for, else the one the user last used, else their first. A wrong
// username and a wrong password fail alike, taking as long.
func (s *service) Login(args identityservice.LoginArgs) identityservice.LoginResponse {
	if f := check(args.ServiceArgs, "session.login"); f != nil {
		return identityservice.LoginResponse{Common: *f}
	}
	if s.auth.SingleUser() {
		return identityservice.LoginResponse{Common: *args.Fail(http.StatusConflict,
			log.Code("no_account").Public("Set up accounts first").New("no user exists yet"))}
	}
	tx := s.db.WithContext(args.Context())
	var u identity.User
	err := tx.Where("username = ?", strings.ToLower(strings.TrimSpace(args.Username))).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		identity.VerifyNobody(args.Password)
		return identityservice.LoginResponse{Common: *badCredentials(args.ServiceArgs)}
	}
	if err != nil {
		return identityservice.LoginResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	if !identity.VerifyPassword(u.PasswordHash, args.Password) {
		return identityservice.LoginResponse{Common: *badCredentials(args.ServiceArgs)}
	}
	if u.Disabled {
		return identityservice.LoginResponse{Common: *args.Fail(http.StatusForbidden,
			log.Code("account_disabled").With("userId", u.ID).Public("This account is disabled").New("a disabled user tried to sign in"))}
	}
	tenant, ok, err := identity.TenantFor(tx, u, args.TenantID)
	if err != nil {
		return identityservice.LoginResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	if !ok {
		return identityservice.LoginResponse{Common: *args.Fail(http.StatusForbidden,
			log.Code("no_tenant").With("userId", u.ID).Public("This account belongs to no tenant; ask an administrator to add it to one").New("a user in no tenant tried to sign in"))}
	}
	return s.begin(args.ServiceArgs, u, tenant)
}

// begin starts u's session in tenant and returns it with their account.
func (s *service) begin(args features.ServiceArgs, u identity.User, tenant uint64) identityservice.LoginResponse {
	tx := s.db.WithContext(args.Context())
	if err := rememberTenant(tx, &u, tenant); err != nil {
		return identityservice.LoginResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	session, err := s.sessions.Start(args.Context(), u, tenant)
	if err != nil {
		return identityservice.LoginResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	account, err := s.account(tx, u, tenant)
	if err != nil {
		return identityservice.LoginResponse{Common: *args.Fail(http.StatusInternalServerError, log.Wrap(err))}
	}
	log.With("userId", u.ID, "tenantId", tenant).Info(args.Context(), "signed in")
	return identityservice.LoginResponse{Common: features.OK(), Session: session, Account: account}
}

func badCredentials(args features.ServiceArgs) *features.ServiceResponse {
	return args.Fail(http.StatusUnauthorized, log.Code("bad_credentials").Public("Wrong username or password").New("wrong username or password"))
}
