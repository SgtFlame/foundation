package authz

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"gorm.io/gorm"
)

// Policy evaluates checks against the grants in the application database.
// It caches each user's grants and every role, and drops the caches
// whenever a grant or role changes (one server; RBAC.md › Evaluation).
type Policy struct {
	db       *gorm.DB
	registry *extensions.Registry
	now      func() time.Time

	mu     sync.Mutex
	grants map[uint64][]RoleGrant // by user
	roles  map[uint64]Role
	warned map[string]bool // unknown actions already logged
}

// NewPolicy evaluates with the grants in gdb and the resource types in reg.
func NewPolicy(gdb *gorm.DB, reg *extensions.Registry) *Policy {
	return &Policy{db: gdb, registry: reg, now: time.Now, grants: map[uint64][]RoleGrant{}, warned: map[string]bool{}}
}

// DB is the application database the grants live in.
func (pol *Policy) DB() *gorm.DB { return pol.db }

// Registry is the registry the resource types come from.
func (pol *Policy) Registry() *extensions.Registry { return pol.registry }

// Changed drops the caches; call it after writing grants or roles.
func (pol *Policy) Changed() {
	pol.mu.Lock()
	defer pol.mu.Unlock()
	pol.grants = map[uint64][]RoleGrant{}
	pol.roles = nil
}

// Roles is every role by ID: the built-in ones and every tenant's custom
// ones.
func (pol *Policy) Roles() (map[uint64]Role, error) {
	pol.mu.Lock()
	defer pol.mu.Unlock()
	if pol.roles != nil {
		return pol.roles, nil
	}
	roles := map[uint64]Role{}
	for _, r := range Builtins(pol.registry) {
		roles[r.ID] = r
	}
	var custom []CustomRole
	if err := pol.db.Find(&custom).Error; err != nil {
		return nil, err
	}
	for _, c := range custom {
		roles[c.ID] = RoleFrom(c)
	}
	pol.roles = roles
	return roles, nil
}

// RoleFrom is a custom role as a Role.
func RoleFrom(c CustomRole) Role {
	r := Role{ID: c.ID, Name: c.Name, Description: c.Description, Scope: OnTenant, TenantID: c.TenantID, Revision: c.Revision, Permissions: []string{}}
	_ = json.Unmarshal([]byte(c.Permissions), &r.Permissions)
	return r
}

// GrantsOf is the user's grants that have not expired.
func (pol *Policy) GrantsOf(user uint64) ([]RoleGrant, error) {
	pol.mu.Lock()
	cached, ok := pol.grants[user]
	pol.mu.Unlock()
	if !ok {
		var rows []RoleGrant
		if err := pol.db.Where("user_id = ?", user).Order("id").Find(&rows).Error; err != nil {
			return nil, err
		}
		pol.mu.Lock()
		pol.grants[user] = rows
		pol.mu.Unlock()
		cached = rows
	}
	now := pol.now()
	out := make([]RoleGrant, 0, len(cached))
	for _, g := range cached {
		if g.ExpiresAt == nil || g.ExpiresAt.After(now) {
			out = append(out, g)
		}
	}
	return out, nil
}

// Decide is Check against this policy.
func (pol *Policy) Decide(ctx context.Context, p features.Principal, action, resource string) Decision {
	return pol.decide(ctx, p, action, resource, true)
}

// Explain is Decide without auditing a denial, for the Access checker and
// for checks that only ask.
func (pol *Policy) Explain(ctx context.Context, p features.Principal, action, resource string) Decision {
	return pol.decide(ctx, p, action, resource, false)
}

func (pol *Policy) decide(ctx context.Context, p features.Principal, action, resource string, audit bool) Decision {
	switch p.Kind {
	case features.PrincipalAnonymous:
		if strings.HasPrefix(action, "session.") {
			return Decision{Allowed: true, Reason: "anyone may sign in"}
		}
		return Decision{Reason: "not signed in"}
	case features.PrincipalService:
		return Decision{Allowed: true, Reason: "background work"}
	}
	if strings.HasPrefix(action, "session.") {
		return Decision{Allowed: true, Reason: "anyone may sign in"}
	}
	typ, act, ok := actionOf(pol.registry, action)
	if !ok {
		pol.mu.Lock()
		first := !pol.warned[action]
		pol.warned[action] = true
		pol.mu.Unlock()
		if first {
			log.With("action", action).Warn(ctx, "authz: %s is not a declared action, so it is denied", action)
		}
		return Decision{Reason: action + " is not a declared action"}
	}
	if act.Class == ClassSelf {
		return Decision{Allowed: true, Reason: "every signed-in user may"}
	}
	path := Split(resource)
	if typ.Owned && OwnedBy(path, p.UserID) {
		return Decision{Allowed: true, Reason: "you own it"}
	}
	grants, err := pol.GrantsOf(p.UserID)
	if err != nil {
		log.Error(ctx, err, "authz: cannot read grants")
		return Decision{Reason: "grants could not be read"}
	}
	roles, err := pol.Roles()
	if err != nil {
		log.Error(ctx, err, "authz: cannot read roles")
		return Decision{Reason: "roles could not be read"}
	}
	for _, g := range grants {
		role, ok := roles[g.RoleID]
		if !ok || !Covers(Split(g.Resource), path) || !role.Holds(action) {
			continue
		}
		reason := fmt.Sprintf("%s granted on %s", role.Name, g.Resource)
		if g.GrantedBy == 0 {
			reason += " by the system"
		} else {
			reason += " by user " + strconv.FormatUint(g.GrantedBy, 10)
		}
		return Decision{Allowed: true, Reason: reason + " on " + g.CreatedAt.Format("2006-01-02"), GrantID: g.ID, RoleID: g.RoleID}
	}
	d := Decision{Reason: fmt.Sprintf("no grant gives %s on %s", action, resource)}
	if audit && changes(act.Class) {
		pol.audit(ctx, AccessAudit{Kind: AuditDenied, UserID: p.UserID, TenantID: p.TenantID, Action: action, Resource: resource, Reason: d.Reason})
	}
	return d
}

// changes tells whether actions of a class change something, so a denied
// check is audited.
func changes(c Class) bool {
	return c != ClassRead && c != ClassSelf
}

// audit records an access change.
func (pol *Policy) audit(ctx context.Context, a AccessAudit) { pol.auditIn(ctx, pol.db, a) }

// auditIn records an access change inside tx.
func (pol *Policy) auditIn(ctx context.Context, tx *gorm.DB, a AccessAudit) {
	if err := tx.WithContext(context.WithoutCancel(ctx)).Create(&a).Error; err != nil {
		log.Error(ctx, err, "authz: cannot write the access audit")
	}
}

// Audit records a grant change made outside authzservice, such as setup.
func (pol *Policy) Audit(ctx context.Context, a AccessAudit) { pol.audit(ctx, a) }

// Split is a resource path's segments, without a leading "app": "app" is
// the root every path is under.
func Split(resource string) []string {
	resource = strings.Trim(resource, "/")
	if resource == "" || resource == OnApp {
		return nil
	}
	resource = strings.TrimPrefix(resource, OnApp+"/")
	return strings.Split(resource, "/")
}

// Covers tells whether a grant on grant applies to resource: grant is
// resource or one of its ancestors.
func Covers(grant, resource []string) bool {
	if len(grant) > len(resource) {
		return false
	}
	for i, s := range grant {
		if resource[i] != s {
			return false
		}
	}
	return true
}

// OwnedBy tells whether a path is under the user: one of its segments is
// "user:<id>".
func OwnedBy(path []string, user uint64) bool {
	own := Resource("user", user)
	for _, s := range path {
		if s == own {
			return true
		}
	}
	return false
}
