package authz

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
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
	grants map[grantee][]RoleGrant
	roles  map[uint64]Role
	warned map[string]bool // unknown actions already logged
}

// grantee is whose grants are cached: a user's, or an agent's.
type grantee struct {
	agent bool
	id    uint64
}

// NewPolicy evaluates with the grants in gdb and the resource types in reg.
func NewPolicy(gdb *gorm.DB, reg *extensions.Registry) *Policy {
	return &Policy{db: gdb, registry: reg, now: time.Now, grants: map[grantee][]RoleGrant{}, warned: map[string]bool{}}
}

// DB is the application database the grants live in.
func (pol *Policy) DB() *gorm.DB { return pol.db }

// Registry is the registry the resource types come from.
func (pol *Policy) Registry() *extensions.Registry { return pol.registry }

// Changed drops the caches; call it after writing grants or roles.
func (pol *Policy) Changed() {
	pol.mu.Lock()
	defer pol.mu.Unlock()
	pol.grants = map[grantee][]RoleGrant{}
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
	r := Role{ID: c.ID, Name: c.Name, Description: c.Description, Scope: OnTenant, TenantID: c.TenantID, Revision: c.Revision, Permissions: []string{},
		App: c.AppID, AppRole: c.AppRole, AgentWarning: c.AgentWarning}
	_ = json.Unmarshal([]byte(c.Permissions), &r.Permissions)
	if c.Rules != "" {
		_ = json.Unmarshal([]byte(c.Rules), &r.Rules)
	}
	return r
}

// GrantsOf is the user's grants that have not expired.
func (pol *Policy) GrantsOf(user uint64) ([]RoleGrant, error) {
	return pol.grantsOf(grantee{id: user})
}

// GrantsOfAgent is the agent's grants that have not expired (forge 07j).
// They are read again after every change to grants, so a revoked grant
// fails the agent's next check.
func (pol *Policy) GrantsOfAgent(agent uint64) ([]RoleGrant, error) {
	return pol.grantsOf(grantee{agent: true, id: agent})
}

func (pol *Policy) grantsOf(who grantee) ([]RoleGrant, error) {
	pol.mu.Lock()
	cached, ok := pol.grants[who]
	pol.mu.Unlock()
	if !ok {
		var rows []RoleGrant
		q := pol.db.Where("user_id = ? AND agent_id = 0", who.id)
		if who.agent {
			q = pol.db.Where("agent_id = ?", who.id)
		}
		if err := q.Order("id").Find(&rows).Error; err != nil {
			return nil, err
		}
		pol.mu.Lock()
		pol.grants[who] = rows
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
	case features.PrincipalAgent:
		if strings.HasPrefix(action, "session.") {
			return Decision{Reason: "an agent cannot sign in"}
		}
		if _, ok, err := FindAgent(ctx, p.TenantID, p.AgentID); err != nil || !ok {
			return Decision{Reason: fmt.Sprintf("agent %d is retired or unknown", p.AgentID)}
		}
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
	if typ.Tenant != 0 && (len(path) == 0 || path[0] != Resource("tenant", typ.Tenant)) {
		return Decision{Reason: fmt.Sprintf("%s is held only in tenant %d", action, typ.Tenant)}
	}
	if act.Apps && p.Kind == features.PrincipalApp && len(path) > 0 && path[0] == Resource("tenant", p.TenantID) {
		return Decision{Allowed: true, Reason: "every app may in its tenant"}
	}
	if act.Agents && p.Kind == features.PrincipalAgent && len(path) > 0 && path[0] == Resource("tenant", p.TenantID) {
		return Decision{Allowed: true, Reason: "every agent may in its tenant"}
	}
	if act.AsMaker && p.Kind == features.PrincipalAgent {
		// The person the run acts for decides, never the agent's own grants
		// (forge 15a)
		if p.OnBehalfOf == 0 {
			return Decision{Reason: "the agent's run acts for no one"}
		}
		maker := features.Principal{Kind: features.PrincipalUser, UserID: p.OnBehalfOf, TenantID: p.TenantID}
		d := pol.decide(ctx, maker, action, resource, audit)
		d.Reason = fmt.Sprintf("as user %d, for whom the agent acts: %s", p.OnBehalfOf, d.Reason)
		return d
	}
	if typ.Owned && p.Kind == features.PrincipalUser && OwnedBy(path, p.UserID) {
		return Decision{Allowed: true, Reason: "you own it"}
	}
	allowing, reason := pol.allowing(ctx, p, typ, action, path, true)
	if reason != "" {
		return Decision{Reason: reason}
	}
	if len(allowing) > 0 {
		g, role := allowing[0].Grant, allowing[0].Role
		reason := fmt.Sprintf("%s granted on %s", role.Name, g.Resource)
		if allowing[0].Record != "" {
			reason += " (its rows rules decide which records)"
		}
		if g.GrantedBy == 0 {
			reason += " by the system"
		} else {
			reason += " by user " + strconv.FormatUint(g.GrantedBy, 10)
		}
		return Decision{Allowed: true, Reason: reason + " on " + g.CreatedAt.Format("2006-01-02"), GrantID: g.ID, RoleID: g.RoleID}
	}
	d := Decision{Reason: fmt.Sprintf("no grant gives %s on %s", action, resource)}
	if audit && changes(act.Class) {
		pol.audit(ctx, AccessAudit{Kind: AuditDenied, UserID: p.UserID, AgentID: p.AgentID, TenantID: p.TenantID, Action: action, Resource: resource, Reason: d.Reason})
	}
	return d
}

// allowing is the grants of p that give action on the resource at path,
// each with its role, the first found first when first is set (a check
// needs one; RowRules needs them all). reason says why grants or roles
// could not be read.
func (pol *Policy) allowing(ctx context.Context, p features.Principal, typ ResourceType, action string, path []string, first bool) ([]Held, string) {
	roles, err := pol.Roles()
	if err != nil {
		log.Error(ctx, err, "authz: cannot read roles")
		return nil, "roles could not be read"
	}
	grants, err := pol.grantsFor(p, roles)
	if err != nil {
		log.Error(ctx, err, "authz: cannot read grants")
		return nil, "grants could not be read"
	}
	var out []Held
	for _, g := range grants {
		role, ok := roles[g.RoleID]
		if !ok {
			continue
		}
		grant := Split(g.Resource)
		if typ.Tenant != 0 && (len(grant) == 0 || grant[0] != Resource("tenant", typ.Tenant)) {
			continue // a pinned type is reached only by grants in its tenant
		}
		held := Held{Grant: g, Role: role}
		if !Covers(grant, path) {
			// A grant on one record reaches records of its scope's types
			// through row rules (forge 07j)
			scope, entity, id, ok := recordReach(typ, role, grant, path)
			if !ok {
				continue
			}
			grant, held.Record, held.RecordEntity = scope, id, entity
		}
		if !role.HoldsOn(action, path) || !crosses(typ, role, grant, path) {
			continue
		}
		out = append(out, held)
		if first {
			break
		}
	}
	return out, ""
}

// grantsFor is p's grants: an app's roles, an agent's or a user's grants.
func (pol *Policy) grantsFor(p features.Principal, roles map[uint64]Role) ([]RoleGrant, error) {
	switch p.Kind {
	case features.PrincipalApp:
		return AppGrants(roles, p.TenantID, p.App), nil
	case features.PrincipalAgent:
		return pol.GrantsOfAgent(p.AgentID)
	}
	return pol.GrantsOf(p.UserID)
}

// recordReach tells whether a grant on one record (grant ends in
// entity:<e>/record:<id>) applies to a check of a record action at path:
// path must be an entity type's records under the grant's scope (the app
// or the tenant's records), and either the record's own type or one the
// role has a row rule for, which says which of its records it reaches.
func recordReach(typ ResourceType, role Role, grant, path []string) (scope []string, entity, id string, ok bool) {
	if typ.ID != "record" {
		return nil, "", "", false
	}
	entity, id, ok = RecordOf(grant)
	if !ok {
		return nil, "", "", false
	}
	scope = grant[:len(grant)-2]
	if len(path) != len(scope)+1 || !Covers(scope, path) {
		return nil, "", "", false
	}
	target, isEntity := strings.CutPrefix(path[len(path)-1], "entity:")
	if !isEntity || target != entity && role.Rules[target] == "" {
		return nil, "", "", false
	}
	return scope, entity, id, true
}

// AppGrants are an app principal's grants (forge 07a question 1): each of
// the app's roles in the tenant, granted on the tenant by the system. They
// change as the app's roles do, at install, upgrade and uninstall.
func AppGrants(roles map[uint64]Role, tenant uint64, app string) []RoleGrant {
	var out []RoleGrant
	for _, r := range roles {
		if app != "" && r.App == app && r.TenantID == tenant {
			out = append(out, RoleGrant{RoleID: r.ID, Resource: Resource("tenant", tenant)})
		}
	}
	slices.SortFunc(out, func(a, b RoleGrant) int { return cmp.Compare(a.RoleID, b.RoleID) })
	return out
}

// crosses tells whether a grant on grant may reach path past the type's
// fence: a segment of the fence's kind below the grant ("app:tasks" under a
// grant on the tenant) is crossed only by the Super Administrator, the
// tenant's Owner and Administrator, and that app's own roles.
func crosses(typ ResourceType, role Role, grant, path []string) bool {
	if typ.Fence == "" {
		return true
	}
	for _, seg := range path[len(grant):] {
		kind, id, _ := strings.Cut(seg, ":")
		if kind == typ.Fence && !role.CrossesFences() && role.App != id {
			return false
		}
	}
	return true
}

// CrossesFences tells whether the role reaches past every fence: the Super
// Administrator, the tenant's Owner and Administrator.
func (r Role) CrossesFences() bool {
	return r.ID == RoleSuperAdmin || r.ID == RoleOwner || r.ID == RoleAdmin
}

// RowRules are the row rules p's roles put on a resource for action
// (forge 05c question 4), under key (an entity type's id): restricted is
// false when some grant giving the action has no rule for key (a role
// with none for it, or any other role, such as the tenant's Owner), and
// otherwise rules are the rules of every role giving it, any of which
// allows a row. Service principals, and checks before a policy is
// installed, are not restricted. The rules' language is the caller's. A
// context made WithAs gives its role's rule where it covers the action.
func RowRules(ctx context.Context, p features.Principal, action, resource, key string) (rules []string, restricted bool) {
	if as, _ := asFor(ctx, p, action, resource); as != nil {
		if rule := as.Rules[key]; rule != "" {
			return []string{rule}, true
		}
		return nil, false
	}
	pol := installed.Load()
	if pol == nil || (p.Kind != features.PrincipalUser && p.Kind != features.PrincipalApp && p.Kind != features.PrincipalAgent) {
		return nil, false
	}
	typ, _, ok := actionOf(pol.registry, action)
	if !ok {
		return nil, false
	}
	allowing, _ := pol.allowing(ctx, p, typ, action, Split(resource), false)
	for _, h := range allowing {
		rule, ok := h.Role.Rules[key]
		if h.Record != "" {
			// A grant on one record always restricts: that record of its
			// own type, and what the role's rule reaches
			switch {
			case key == h.RecordEntity && rule != "":
				rule = "(" + recordRule(h.Record) + ") || (" + rule + ")"
			case key == h.RecordEntity:
				rule = recordRule(h.Record)
			case rule == "":
				rule = "false"
			}
			ok = true
		}
		if !ok || rule == "" {
			return nil, false
		}
		if !slices.Contains(rules, rule) {
			rules = append(rules, rule)
		}
	}
	return rules, len(rules) > 0
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
