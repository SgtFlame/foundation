package identityservice

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/identity"
)

// Service signs users in and out and keeps their sessions
// (documentation/MultiTenant.md › Built in slice 10). Its operations
// that start a session return it for the handler to put in cookies.
type Service interface {
	// Status is whether sign-in is required and who is signed in.
	Status(args StatusArgs) StatusResponse
	// Setup makes the first tenant and administrator, ending single-user
	// mode, and signs them in. 409 setup_done once a user exists.
	Setup(args SetupArgs) SetupResponse
	Login(args LoginArgs) LoginResponse
	// Refresh rotates a refresh token; 401 session_ended means signing in
	// again.
	Refresh(args RefreshArgs) RefreshResponse
	Logout(args LogoutArgs) LogoutResponse
	// SwitchTenant starts a session in another of the user's tenants.
	SwitchTenant(args SwitchTenantArgs) SwitchTenantResponse
	// UpdateAccount changes the user's own name, email or password. A new
	// password ends their other sessions and starts a new one.
	UpdateAccount(args UpdateAccountArgs) UpdateAccountResponse
}

// Account is the signed-in user and the tenants they may switch to.
type Account struct {
	User     identity.User
	TenantID uint64
	Tenants  []identity.Tenant
}

type StatusArgs struct {
	features.ServiceArgs
}

type StatusResponse struct {
	Common features.ServiceResponse
	// SingleUser: no user exists yet, so everyone is the implicit
	// administrator and setup is available.
	SingleUser bool
	SignedIn   bool
	Account    *Account // when a user is signed in
}

type SetupArgs struct {
	features.ServiceArgs
	TenantName  string
	Username    string
	DisplayName string
	Email       string
	Password    string
}

type SetupResponse struct {
	Common  features.ServiceResponse
	Session identity.Session
	Account Account
}

type LoginArgs struct {
	features.ServiceArgs
	Username string
	Password string
	TenantID uint64 // optional: where to start
}

// LoginResponse is 401 bad_credentials for a wrong username or password,
// 403 account_disabled, and 403 no_tenant for a user in no tenant.
type LoginResponse struct {
	Common  features.ServiceResponse
	Session identity.Session
	Account Account
}

type RefreshArgs struct {
	features.ServiceArgs
	Token string // the refresh cookie
}

type RefreshResponse struct {
	Common  features.ServiceResponse
	Session identity.Session
}

type LogoutArgs struct {
	features.ServiceArgs
	Token string // the refresh cookie, if any
}

type LogoutResponse struct {
	Common features.ServiceResponse
}

type SwitchTenantArgs struct {
	features.ServiceArgs
	TenantID uint64
	Token    string // the refresh cookie, whose session ends
}

// SwitchTenantResponse is 404 tenant_not_found unless the user is a member.
type SwitchTenantResponse struct {
	Common  features.ServiceResponse
	Session identity.Session
	Account Account
}

// UpdateAccountArgs changes what is set. A new password needs the current
// one.
type UpdateAccountArgs struct {
	features.ServiceArgs
	DisplayName     *string
	Email           *string
	CurrentPassword string
	NewPassword     string
}

// UpdateAccountResponse has a Session only when the password changed.
type UpdateAccountResponse struct {
	Common  features.ServiceResponse
	Account Account
	Session *identity.Session
}
