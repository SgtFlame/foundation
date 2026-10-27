// Package authz decides what a principal may do (documentation/plans/RBAC.md).
// Every service operation calls Check at its start with the principal from
// its ServiceArgs.
//
// A permission is "<resourceType>.<action>", declared by the owning feature
// on the authz.resourceType point. A role is a set of permissions, granted
// to a user on a resource path ("app", "tenant:1", "tenant:1/volume:42"),
// and applies to that resource and everything under it. Access is
// allow-only (RBAC question 2).
package authz

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

var log = logging.In("authz")

// Decision is the outcome of a Check, with the reason for it and, when a
// grant allowed it, which.
type Decision struct {
	Allowed bool
	Reason  string
	GrantID uint64
	RoleID  uint64
}

var installed atomic.Pointer[Policy]

// Install makes Check evaluate against pol; serve.go installs the policy
// once the registry is complete.
func Install(pol *Policy) { installed.Store(pol) }

// Installed is the policy Check evaluates against, nil before Install.
func Installed() *Policy { return installed.Load() }

// Check decides whether p may do action ("<resourceType>.<action>", e.g.
// "volume.write") on resource (a path such as "tenant:1/volume:42").
//
// An anonymous principal may only use session.*; a service principal may
// do anything; a user needs a grant whose role holds the action on the
// resource or an ancestor, unless the action is of the self class or the
// resource is theirs (a user-owned type under "user:<id>"). Before a policy
// is installed (tests of other features) every principal but an anonymous
// one is allowed.
func Check(ctx context.Context, p features.Principal, action, resource string) Decision {
	if pol := installed.Load(); pol != nil {
		return pol.Decide(ctx, p, action, resource)
	}
	if p.Kind == features.PrincipalAnonymous && !strings.HasPrefix(action, "session.") {
		return Decision{Reason: "not signed in"}
	}
	return Decision{Allowed: true, Reason: "access control is not installed"}
}

// Err is nil when the decision allows, and otherwise an access_denied error
// to Fail with Status.
func (d Decision) Err(action, resource string) error {
	if d.Allowed {
		return nil
	}
	return log.Code("access_denied").With("action", action, "resource", resource).
		Public(fmt.Sprintf("You may not %s here", action)).Errorf("%s on %s denied: %s", action, resource, d.Reason)
}

// Status is the HTTP status of a denied check.
const Status = http.StatusForbidden

// Resource joins path segments as "type:id", e.g. Resource("tenant", 1).
func Resource(kind string, id uint64) string {
	return fmt.Sprintf("%s:%d", kind, id)
}

// Tenant is the resource path of the principal's tenant.
func Tenant(p features.Principal) string {
	return Resource("tenant", p.TenantID)
}
