package features

import (
	"context"
	"strconv"
)

// PrincipalKind tells a person apart from work running with no user present.
type PrincipalKind string

const (
	PrincipalUser    PrincipalKind = "user"
	PrincipalService PrincipalKind = "service" // background work, such as indexing
	// PrincipalAnonymous is a request with no valid session once login is
	// required; it may only sign in (documentation/MultiTenant.md).
	PrincipalAnonymous PrincipalKind = "anonymous"
	// PrincipalApp is an installed app's own principal in a tenant (forge
	// 07a question 1): its triggers and background jobs run as it. It holds
	// the app's roles in that tenant and nothing else, and has no user.
	PrincipalApp PrincipalKind = "app"
)

// Principal is who a service operation runs for. It is filled from the
// request (the session token), never from a request body, and carried in
// every ServiceArgs.
type Principal struct {
	Kind     PrincipalKind
	UserID   uint64 // 0 for a service, app or anonymous principal
	TenantID uint64
	App      string // the app of an app principal
}

// Single-user mode runs as one implicit tenant and user. Their IDs are
// reserved, so the default tenant keeps them once tenants and users exist.
const (
	LocalTenantID uint64 = 1
	LocalUserID   uint64 = 1
)

// GlobalTenantID is the reserved global tenant, which holds content shared
// with every tenant, such as approved Chronicle entries
// (documentation/MultiTenant.md › Sharing Chronicles). Nobody signs
// in to it: it has no row and no members.
const GlobalTenantID uint64 = 2

// LocalPrincipal is the implicit user of single-user mode, who holds Super
// Administrator (identity.Bootstrap grants it).
func LocalPrincipal() Principal {
	return Principal{Kind: PrincipalUser, UserID: LocalUserID, TenantID: LocalTenantID}
}

// AnonymousPrincipal is a request without a valid session.
func AnonymousPrincipal() Principal {
	return Principal{Kind: PrincipalAnonymous}
}

// ServicePrincipal is background work for tenant, with no user present.
func ServicePrincipal(tenant uint64) Principal {
	return Principal{Kind: PrincipalService, TenantID: tenant}
}

// BackgroundArgs are the ServiceArgs of work no request started, such as the
// index worker, running as the local tenant's service principal.
func BackgroundArgs(ctx context.Context) ServiceArgs {
	return ServiceArgs{Ctx: ctx, Principal: ServicePrincipal(LocalTenantID)}
}

// AppPrincipal is an installed app's principal in tenant (forge 07a).
func AppPrincipal(tenant uint64, app string) Principal {
	return Principal{Kind: PrincipalApp, TenantID: tenant, App: app}
}

// String names the principal for logs and the Jobs view: user:<id>,
// app:<id>, service or anonymous.
func (p Principal) String() string {
	switch p.Kind {
	case PrincipalUser:
		return "user:" + strconv.FormatUint(p.UserID, 10)
	case PrincipalApp:
		return "app:" + p.App
	}
	return string(p.Kind)
}

// PrincipalRef is a principal as data, for work that runs later as it (a
// job): ids as JSON strings, the tenant left to where it is stored.
type PrincipalRef struct {
	Kind   PrincipalKind `json:"kind"`
	UserID uint64        `json:"userId,string,omitempty"`
	App    string        `json:"app,omitempty"`
}

// Ref is the principal as data.
func (p Principal) Ref() PrincipalRef { return PrincipalRef{Kind: p.Kind, UserID: p.UserID, App: p.App} }

// In is the principal a reference names in tenant.
func (r PrincipalRef) In(tenant uint64) Principal {
	return Principal{Kind: r.Kind, UserID: r.UserID, TenantID: tenant, App: r.App}
}
