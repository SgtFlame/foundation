package authz

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Agent principals (forge 07j, RBAC.md › Built in slice 51): an agent is
// a principal an app's record defines. Its id is the record's id, its name
// the record's title and its creator the record's; it is retired when the
// record is deleted. The records feature answers for them (SetAgents),
// so making, renaming and retiring an agent has no side effect of its own.
// An agent holds roles as a user does, in grants with AgentID set, and
// cannot sign in.

// AgentInfo is an agent principal as its record defines it.
type AgentInfo struct {
	ID        uint64 `json:"id,string"`
	TenantID  uint64 `json:"tenantId,string"`
	Name      string `json:"name"`
	Entity    string `json:"entity"` // the entity type of the record defining it
	App       string `json:"app,omitempty"`
	CreatedBy uint64 `json:"createdBy,string"`
	// Resource is the record's authz path, where agent.use is checked.
	Resource string `json:"resource"`
	// Record is the defining record's values, read as the system: what
	// Current user's agentRecord gives the agent itself (an agent may always
	// know its own definition, forge 7p-4). Never sent in listings.
	Record map[string]any `json:"-"`
}

// Agents finds the agents of a tenant: the records feature's directory.
type Agents interface {
	// Agent is a live agent of the tenant; false once its record is deleted
	// or when there is none.
	Agent(ctx context.Context, tenant, id uint64) (AgentInfo, bool, error)
	// List is the tenant's live agents.
	List(ctx context.Context, tenant uint64) ([]AgentInfo, error)
}

var agents atomic.Pointer[Agents]

// SetAgents sets where agents are found (records' directory, wired by
// serve.go); nil leaves none, so every agent check is denied.
func SetAgents(a Agents) {
	if a == nil {
		agents.Store(nil)
		return
	}
	agents.Store(&a)
}

// FindAgent is a live agent of the tenant.
func FindAgent(ctx context.Context, tenant, id uint64) (AgentInfo, bool, error) {
	a := agents.Load()
	if a == nil || id == 0 {
		return AgentInfo{}, false, nil
	}
	return (*a).Agent(ctx, tenant, id)
}

// ListAgents is the tenant's live agents; none when no directory is set.
func ListAgents(ctx context.Context, tenant uint64) ([]AgentInfo, error) {
	a := agents.Load()
	if a == nil {
		return nil, nil
	}
	return (*a).List(ctx, tenant)
}

// ContributeAgents declares the agent resource type: using an agent, which
// is checked on the record defining it.
func ContributeAgents(reg *extensions.Registry) {
	ResourceTypes.Register(reg, ResourceType{
		Descriptor: extensions.Descriptor{ID: "agent", Source: "authz", Order: 87}, Title: "Agents", Parent: "tenant", Fence: "app",
		Actions: []Action{
			{Name: "use", Title: "Run work as an agent", Class: ClassExecute},
		},
	})
}

// MayUseAgent decides whether p may run work as the agent: its creator
// may, the app whose entity type defines it (its triggers and jobs), and
// anyone holding agent.use on its record. It returns the agent.
func MayUseAgent(ctx context.Context, p features.Principal, id uint64) (AgentInfo, error) {
	a, ok, err := FindAgent(ctx, p.TenantID, id)
	if err != nil {
		return AgentInfo{}, err
	}
	if !ok {
		return AgentInfo{}, log.Code("agent_not_found").With("agentId", id).
			Public("There is no such agent").Errorf("there is no live agent %d in tenant %d", id, p.TenantID)
	}
	switch {
	case p.Kind == features.PrincipalService:
		return a, nil
	case p.Kind == features.PrincipalUser && p.UserID == a.CreatedBy:
		return a, nil
	case p.Kind == features.PrincipalApp && p.App != "" && p.App == a.App:
		// An app's triggers and jobs run the agents its own records define
		// (forge 7m); the agent stays held to its own grants
		return a, nil
	case p.Kind == features.PrincipalAgent && p.AgentID == a.ID:
		return a, nil
	}
	return a, Check(ctx, p, "agent.use", a.Resource).Err("agent.use", a.Resource)
}

// AsAgent is the principal p runs work as when it names agent id: the
// agent, acting for p's user (or for whom p acts), once p may use it.
func AsAgent(ctx context.Context, p features.Principal, id uint64) (features.Principal, error) {
	a, err := MayUseAgent(ctx, p, id)
	if err != nil {
		return features.Principal{}, err
	}
	by := p.UserID
	switch p.Kind {
	case features.PrincipalAgent:
		by = p.OnBehalfOf
	case features.PrincipalService, features.PrincipalApp:
		by = a.CreatedBy
	}
	as := features.AgentPrincipal(p.TenantID, a.ID, by)
	if p.Kind == features.PrincipalAgent {
		// An agent's run keeps its reach in whatever it starts (forge 7p-3)
		as.Reach = p.Reach
	}
	return as, nil
}

// ParseAgent reads an agent's id from "agent:<id>" or "<id>".
func ParseAgent(s string) (uint64, bool) {
	id, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(s), "agent:"), 10, 64)
	return id, err == nil && id != 0
}

// HasRole tells whether p holds the app role (its id in the manifest, e.g.
// mailSender) on the record with id recordID, or on something above it:
// the tenant or the role's app (forge 07j: row rules test it with
// principal.hasRole). record may be an id as text or a number, a
// relation's value ({id}) or a list of them, any of which counts.
func HasRole(ctx context.Context, p features.Principal, role string, record any) bool {
	pol := installed.Load()
	if pol == nil || role == "" {
		return false
	}
	ids := recordIDs(record)
	if len(ids) == 0 {
		return false
	}
	roles, err := pol.Roles()
	if err != nil {
		return false
	}
	var grants []RoleGrant
	switch p.Kind {
	case features.PrincipalApp:
		grants = AppGrants(roles, p.TenantID, p.App)
	case features.PrincipalAgent:
		grants, err = pol.GrantsOfAgent(p.AgentID)
	case features.PrincipalUser:
		grants, err = pol.GrantsOf(p.UserID)
	default:
		return false
	}
	if err != nil {
		return false
	}
	tenant := Resource("tenant", p.TenantID)
	for _, g := range grants {
		r, ok := roles[g.RoleID]
		if !ok || r.AppRole != role || r.TenantID != p.TenantID {
			continue
		}
		path := Split(g.Resource)
		switch {
		case len(path) == 0 || path[0] != tenant:
		case len(path) == 1 || len(path) == 2 && path[1] == "app:"+r.App:
			return true
		default:
			if _, id, ok := RecordOf(path); ok && ids[id] {
				return true
			}
		}
	}
	return false
}

// recordIDs are the record ids a value names.
func recordIDs(v any) map[string]bool {
	out := map[string]bool{}
	var add func(v any)
	add = func(v any) {
		switch x := v.(type) {
		case string:
			if x != "" {
				out[x] = true
			}
		case float64:
			out[strconv.FormatFloat(x, 'f', -1, 64)] = true
		case int:
			out[strconv.Itoa(x)] = true
		case uint64:
			out[strconv.FormatUint(x, 10)] = true
		case json.Number:
			out[x.String()] = true
		case map[string]any:
			add(x["id"])
		case []any:
			for _, e := range x {
				add(e)
			}
		case []string:
			for _, e := range x {
				add(e)
			}
		}
	}
	add(v)
	return out
}

// RecordOf reads a record grant's path, ".../entity:<type>/record:<id>":
// the entity type and the record's id.
func RecordOf(path []string) (entity, id string, ok bool) {
	n := len(path)
	if n < 3 {
		return "", "", false
	}
	id, isRecord := strings.CutPrefix(path[n-1], "record:")
	entity, isEntity := strings.CutPrefix(path[n-2], "entity:")
	return entity, id, isRecord && isEntity && id != "" && entity != ""
}

// RecordResource is a record's authz path under its entity type's.
func RecordResource(entityResource, id string) string {
	return entityResource + "/record:" + id
}

// recordRule is the row rule a grant on one record puts on its own entity
// type: that record alone.
func recordRule(id string) string {
	return fmt.Sprintf("record._id == %q", id)
}

// IsOwner tells whether p is a user holding the tenant's Owner role on its
// tenant, or the Super Administrator (forge 07o: row rules test it with
// principal.owner, so an app can let Owners read what Administrators may
// not without a grant).
func IsOwner(ctx context.Context, p features.Principal) bool {
	pol := installed.Load()
	if pol == nil || p.Kind != features.PrincipalUser {
		return false
	}
	grants, err := pol.GrantsOf(p.UserID)
	if err != nil {
		return false
	}
	tenant := Resource("tenant", p.TenantID)
	for _, g := range grants {
		path := Split(g.Resource)
		switch {
		case g.RoleID == RoleSuperAdmin:
			return true
		case g.RoleID == RoleOwner && len(path) == 1 && path[0] == tenant:
			return true
		}
	}
	return false
}
