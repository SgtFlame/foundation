package features

import (
	"context"
	"strconv"
	"strings"
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
	// PrincipalAgent is an agent (forge 07j): a principal an app's record
	// defines (records' principal field), holding roles as a user does but
	// unable to sign in. Its runs carry the user who started them as
	// OnBehalfOf; its access is its own grants, never theirs.
	PrincipalAgent PrincipalKind = "agent"
)

// Principal is who a service operation runs for. It is filled from the
// request (the session token), never from a request body, and carried in
// every ServiceArgs.
type Principal struct {
	Kind     PrincipalKind
	UserID   uint64 // 0 for a service, app or anonymous principal
	TenantID uint64
	App      string // the app of an app principal
	// AgentID is an agent principal's id: the id of the record defining it.
	AgentID uint64
	// OnBehalfOf is the user an agent's run acts for (who started it, or
	// the agent's creator), for audit; it gives the agent no access.
	OnBehalfOf uint64
	// Reach is what an agent's run is about (forge 7p-3): the subjects it
	// was started on, as record keys "<entity>:<id>" separated by spaces;
	// empty when the run is not confined. It grants nothing: a host's
	// tools read it (Current user's reach) and refuse writes outside it.
	// Set by Call graph's Reach when it runs a graph As agent; work an
	// agent starts keeps its reach (authz.AsAgent), so it never widens.
	Reach string
	// MemoryOf is the person whose Chronicle memory an agent's run may
	// search (forge 15a): the person who started it, else the owner the
	// host named for its subject, else none (0). It grants nothing else; set
	// only by Call graph's memoryOf when it runs a graph As agent, from the
	// caller or the host's dispatch, never from a model or a tool's
	// arguments; work an agent starts keeps it (authz.AsAgent).
	MemoryOf uint64
	// Service names the service a service principal works for (forge
	// 21b), for the audit and the logs; empty for background work that
	// names none.
	Service string
}

// ReachKeys is the principal's reach as record keys ("<entity>:<id>").
func (p Principal) ReachKeys() []string {
	return strings.Fields(p.Reach)
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

// RootTenantID is the reserved root tenant (forge 20b-1), *Infrastructure*,
// the only tenant that may hold infrastructure: a real row made when the
// control plane is set up (controlplane.EnsureRootTenant), never by
// setup, never deleted.
const RootTenantID uint64 = 3

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

// ServicePrincipalFor is background work of service for tenant (forge
// 21b): a service principal that says which service acted.
func ServicePrincipalFor(tenant uint64, service string) Principal {
	return Principal{Kind: PrincipalService, TenantID: tenant, Service: service}
}

// AppPrincipal is an installed app's principal in tenant (forge 07a).
func AppPrincipal(tenant uint64, app string) Principal {
	return Principal{Kind: PrincipalApp, TenantID: tenant, App: app}
}

// AgentPrincipal is an agent's principal in tenant, acting for user.
func AgentPrincipal(tenant, agent, onBehalfOf uint64) Principal {
	return Principal{Kind: PrincipalAgent, TenantID: tenant, AgentID: agent, OnBehalfOf: onBehalfOf}
}

// String names the principal for logs and the Jobs view: user:<id>,
// app:<id>, agent:<id>, service or anonymous.
func (p Principal) String() string {
	switch p.Kind {
	case PrincipalUser:
		return "user:" + strconv.FormatUint(p.UserID, 10)
	case PrincipalApp:
		return "app:" + p.App
	case PrincipalAgent:
		return "agent:" + strconv.FormatUint(p.AgentID, 10)
	case PrincipalService:
		if p.Service != "" {
			return "service:" + p.Service
		}
	}
	return string(p.Kind)
}

// PrincipalRef is a principal as data, for work that runs later as it (a
// job): ids as JSON strings, the tenant left to where it is stored.
type PrincipalRef struct {
	Kind   PrincipalKind `json:"kind"`
	UserID uint64        `json:"userId,string,omitempty"`
	App    string        `json:"app,omitempty"`
	// AgentID and OnBehalfOf are an agent principal's (forge 07j).
	AgentID    uint64 `json:"agentId,string,omitempty"`
	OnBehalfOf uint64 `json:"onBehalfOf,string,omitempty"`
	// Reach is an agent run's reach (forge 7p-3), kept by work it enqueues.
	Reach string `json:"reach,omitempty"`
	// MemoryOf is an agent run's memory owner (forge 15a), kept likewise.
	MemoryOf uint64 `json:"memoryOf,string,omitempty"`
	// Service is a service principal's service (forge 21b).
	Service string `json:"service,omitempty"`
}

// Ref is the principal as data.
func (p Principal) Ref() PrincipalRef {
	return PrincipalRef{Kind: p.Kind, UserID: p.UserID, App: p.App, AgentID: p.AgentID, OnBehalfOf: p.OnBehalfOf, Reach: p.Reach, MemoryOf: p.MemoryOf, Service: p.Service}
}

// In is the principal a reference names in tenant.
func (r PrincipalRef) In(tenant uint64) Principal {
	return Principal{Kind: r.Kind, UserID: r.UserID, TenantID: tenant, App: r.App, AgentID: r.AgentID, OnBehalfOf: r.OnBehalfOf, Reach: r.Reach, MemoryOf: r.MemoryOf, Service: r.Service}
}
