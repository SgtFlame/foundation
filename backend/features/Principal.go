package features

import "context"

// PrincipalKind tells a person apart from work running with no user present.
type PrincipalKind string

const (
	PrincipalUser    PrincipalKind = "user"
	PrincipalService PrincipalKind = "service" // background work, such as indexing
	// PrincipalAnonymous is a request with no valid session once login is
	// required; it may only sign in (documentation/MultiTenant.md).
	PrincipalAnonymous PrincipalKind = "anonymous"
)

// Principal is who a service operation runs for. It is filled from the
// request (the session token), never from a request body, and carried in
// every ServiceArgs.
type Principal struct {
	Kind     PrincipalKind
	UserID   uint64 // 0 for a service or anonymous principal
	TenantID uint64
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
