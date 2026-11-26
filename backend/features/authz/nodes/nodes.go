// Package authznodes is access control's Cognition nodes (forge 07j):
// Grant role and Revoke role give an agent (or, forge 07o, a user) a role
// on a resource (often one record), as the run's principal, who needs the
// right to grant it; Has role tells whether the run's principal holds an
// app role on a record; May tells whether it holds an action (forge 15b).
package authznodes

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/records"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
)

const source = "authz"

// Node type ids.
const (
	GrantID   = "authz.grantRole"
	RevokeID  = "authz.revokeRole"
	HasRoleID = "authz.hasRole"
	// CanID tells whether the run's principal holds an action (forge 15b)
	CanID = "authz.can"
	// GrantsID lists the grants on an entity type's records (forge 18a)
	GrantsID = "authz.grants"
)

var kinds = []string{livenodes.KindAction, livenodes.KindTrigger, uinodes.KindHandler, cognition.KindFunction, cognition.KindAgent}

var inPins = []cognition.Pin{
	cognition.ExecPin(cognition.Then, ""),
	{ID: "agent", Name: "Agent", Type: "string", Optional: true, Description: "The agent's id (its record's id), or agent:<id>"},
	{ID: "user", Name: "User", Type: "string", Optional: true, Description: "A user's id, in place of an agent (forge 07o)"},
	cognition.DataPin("role", "Role", "string", "The role: an app role's id in its manifest (mailSender), or a role's id"),
	{ID: "entity", Name: "Entity", Type: "string", Optional: true, Description: "With Record, the entity type of the one record it is granted on"},
	{ID: "record", Name: "Record", Type: "string", Optional: true, Description: "The id of the one record it is granted on, e.g. a mail account"},
	{ID: "resource", Name: "Resource", Type: "string", Optional: true, Description: "A resource path in place of a record; the tenant when neither is given"},
}

// Contribute registers the nodes.
func Contribute(reg *extensions.Registry) {
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: CanID, Source: source, Order: 975}, Title: "May", Category: "Access",
		Description: "Whether the run's principal holds an action on a resource (the tenant when none is given), as authz.Check decides it (forge 15b): for a page to show a control only to those who may use it, such as the agent editor's Customize, which needs cognition.write and cognition.publish. The server still checks every command; this only decides what to show.",
		Example:     "May cognition.publish, into Set state mayCustomize on mount",
		Tags:        []string{"rbac", "permission", "access", "can"},
		Pins: cognition.Pins{
			In: []cognition.Pin{
				cognition.DataPin("action", "Action", "string", "The action, <type>.<action>, e.g. cognition.publish"),
				{ID: "resource", Name: "Resource", Type: "string", Optional: true, Description: "A resource path, e.g. tenant:1/app:mail; the tenant when empty"},
			},
			Out: []cognition.Pin{cognition.DataPin("allowed", "Allowed", "bool", "Whether the principal holds it")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			action, _ := cognition.Get[string](in, "action")
			resource, _ := cognition.Get[string](in, "resource")
			if strings.TrimSpace(resource) == "" {
				resource = authz.Tenant(x.Principal)
			}
			if strings.TrimSpace(action) == "" {
				return cognition.Values{"allowed": false}, "", nil
			}
			return cognition.Values{"allowed": authz.Check(x.Context(), x.Principal, action, resource).Err(action, resource) == nil}, "", nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: GrantID, Source: source, Order: 960}, Title: "Grant role", Category: "Access",
		Description: "Gives an agent, or a user, a role on one record (or another resource), as the run's principal, who needs grant.write there and every permission of the role: no one holds more than their granter could give. An app's own principal (its triggers and jobs) may grant the app's own roles on the app's own records. Granting it again changes nothing. Leaves by Failed with the reason when refused.",
		Example:     "Grant role mailSender to the triage agent on the support account's record",
		Examples:    cognition.ConfigExample(GrantID, `{}`),
		Tags:        []string{"rbac", "agent", "share", "permission"},
		Kinds:       kinds, Effects: true, External: true,
		Sample: cognition.Values{"grant": "0"},
		Pins: cognition.Pins{In: inPins, Out: []cognition.Pin{
			cognition.ExecPin(cognition.Then, ""), cognition.ExecPin("failed", "Failed"),
			cognition.DataPin("grant", "Grant", "string", "The grant's id"),
			cognition.DataPin("error", "Error", "string", "Why it was refused"),
		}},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			pol, a, failed := prepare(x, in)
			if failed != nil {
				return failed, "failed", nil
			}
			if d := pol.MayGrant(x.Context(), x.Principal, a.role, a.resource); !d.Allowed {
				return cognition.Values{"error": d.Err("grant.write", a.resource).Error()}, "failed", nil
			}
			g, err := pol.Grant(x.Context(), nil, authz.GrantArgs{UserID: a.user, AgentID: a.agent, RoleID: a.role.ID, Resource: a.resource, By: grantor(x.Principal)})
			if err != nil {
				return cognition.Values{"error": err.Error()}, "failed", nil
			}
			return cognition.Values{"grant": strconv.FormatUint(g.ID, 10)}, cognition.Then, nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: RevokeID, Source: source, Order: 961}, Title: "Revoke role", Category: "Access",
		Description: "Takes a role an agent or a user holds on one record (or another resource) away, as the run's principal, with the same right as Grant role; their next check fails. Leaves by Failed when refused or when they do not hold it there.",
		Example:     "Revoke role mailSender from the triage agent on the support account's record",
		Examples:    cognition.ConfigExample(RevokeID, `{}`),
		Tags:        []string{"rbac", "agent", "share", "permission"},
		Kinds:       kinds, Effects: true, External: true,
		Sample: cognition.Values{},
		Pins: cognition.Pins{In: inPins, Out: []cognition.Pin{
			cognition.ExecPin(cognition.Then, ""), cognition.ExecPin("failed", "Failed"),
			cognition.DataPin("error", "Error", "string", "Why it was refused"),
		}},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			pol, a, failed := prepare(x, in)
			if failed != nil {
				return failed, "failed", nil
			}
			if d := pol.MayGrant(x.Context(), x.Principal, a.role, a.resource); !d.Allowed {
				return cognition.Values{"error": d.Err("grant.write", a.resource).Error()}, "failed", nil
			}
			grants, err := pol.GrantsOn(nil, a.resource, false)
			if err != nil {
				return nil, "", err
			}
			for _, g := range grants {
				if g.AgentID == a.agent && g.UserID == a.user && g.RoleID == a.role.ID {
					if _, err := pol.Revoke(x.Context(), nil, g.ID, grantor(x.Principal)); err != nil {
						return cognition.Values{"error": err.Error()}, "failed", nil
					}
					return cognition.Values{}, cognition.Then, nil
				}
			}
			return cognition.Values{"error": "they do not hold that role there"}, "failed", nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: GrantsID, Source: source, Order: 963}, Title: "Grants", Category: "Access",
		Description: "The role grants on one record, or on every record of an entity type (forge 18a), so a graph that keeps grants in step with records (an app's access rules) can revoke those no rule gives any more. An app's own principal reads the grants of its own roles on its own records; anyone else needs grant.read there. Each grant is {grant, user, agent, role (an app role's id in its manifest, else the role's id), app, record}.",
		Example:     "Grants on requirements' reqBoard records, to revoke the levels no access rule gives",
		Examples:    cognition.ConfigExample(GrantsID, `{}`),
		Tags:        []string{"rbac", "share", "permission", "grants"},
		Kinds:       kinds, Volatile: true,
		Sample:      cognition.Values{"grants": []any{}},
		Pins: cognition.Pins{In: []cognition.Pin{
			cognition.DataPin("entity", "Entity", "string", "The entity type whose records' grants are listed"),
			{ID: "record", Name: "Record", Type: "string", Optional: true, Description: "One record's id; every record of the type when empty"},
		}, Out: []cognition.Pin{
			cognition.DataPin("grants", "Grants", "json", "[{grant, user, agent, role, app, record}], oldest first"),
			cognition.DataPin("error", "Error", "string", "Why they cannot be read; empty when they could"),
		}},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			out, err := grantsOn(x, in)
			if err != "" {
				return cognition.Values{"grants": []any{}, "error": err}, "", nil
			}
			return cognition.Values{"grants": out, "error": ""}, "", nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: HasRoleID, Source: source, Order: 962}, Title: "Has role", Category: "Access",
		Description: "Whether the run's principal (the command's sender in a command graph) holds an app role on one record, or above it on the app or the tenant, as a row rule's principal.hasRole tells (forge 07o): a command refuses what only a role allows, such as sending from a mail account without Mail sender.",
		Example:     "Has role mailSender on the draft's account, else Reject",
		Examples:    cognition.ConfigExample(HasRoleID, `{"role":"sender"}`),
		Tags:        []string{"rbac", "role", "permission", "check"},
		Kinds:       append([]string{livenodes.KindCommand, livenodes.KindQuery}, kinds...),
		Config:      []byte(`{"type":"object","properties":{"role":{"type":"string","title":"Role","description":"An app role's id in its manifest, used when the input is unconnected"}}}`),
		Sample:      cognition.Values{"has": true},
		Pins: cognition.Pins{In: []cognition.Pin{
			{ID: "role", Name: "Role", Type: "string", Optional: true, Description: "The app role's id in its manifest; the config's when unconnected"},
			cognition.DataPin("record", "Record", "string", "The record's id, e.g. a mail account's"),
		}, Out: []cognition.Pin{cognition.DataPin("has", "Has", "bool", "Whether the principal holds the role there")}},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Role string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			role, _ := cognition.Get[string](in, "role")
			if role == "" {
				role = c.Role
			}
			record, _ := cognition.Get[string](in, "record")
			return cognition.Values{"has": authz.HasRole(x.Context(), x.Principal, role, record)}, "", nil
		},
	})
}

// grantsOn lists the grants Grants' inputs name, or why it may not.
func grantsOn(x *cognition.Exec, in cognition.Values) ([]any, string) {
	pol := authz.Installed()
	if pol == nil {
		return nil, "access control is not installed"
	}
	entity, _ := cognition.Get[string](in, "entity")
	record, _ := cognition.Get[string](in, "record")
	if strings.TrimSpace(entity) == "" {
		return nil, "give the entity type"
	}
	resource := records.Resource(x.Principal, entity)
	under := record == ""
	if !under {
		resource = authz.RecordResource(resource, record)
	}
	app := ""
	if x.Principal.Kind == features.PrincipalApp {
		path := authz.Split(resource)
		if x.Principal.App == "" || len(path) < 2 || path[1] != "app:"+x.Principal.App {
			return nil, "an app reads grants only on its own records"
		}
		app = x.Principal.App
	} else if err := authz.Check(x.Context(), x.Principal, "grant.read", resource).Err("grant.read", resource); err != nil {
		return nil, err.Error()
	}
	grants, err := pol.GrantsOn(nil, resource, under)
	if err != nil {
		return nil, "the grants cannot be read"
	}
	roles, err := pol.Roles()
	if err != nil {
		return nil, "roles cannot be read"
	}
	out := []any{}
	for _, g := range grants {
		r := roles[g.RoleID]
		if app != "" && r.App != app {
			continue
		}
		role := strconv.FormatUint(g.RoleID, 10)
		if r.AppRole != "" {
			role = r.AppRole
		}
		_, id, _ := authz.RecordOf(authz.Split(g.Resource))
		item := map[string]any{"grant": strconv.FormatUint(g.ID, 10), "user": "", "agent": "", "role": role, "app": r.App, "record": id}
		if g.UserID != 0 {
			item["user"] = strconv.FormatUint(g.UserID, 10)
		}
		if g.AgentID != 0 {
			item["agent"] = strconv.FormatUint(g.AgentID, 10)
		}
		out = append(out, item)
	}
	return out, ""
}

// grantArgs are a node's inputs, resolved.
type grantArgs struct {
	agent    uint64
	user     uint64
	role     authz.Role
	resource string
}

// grantor is who a grant is recorded as made by.
func grantor(p features.Principal) uint64 {
	if p.Kind == features.PrincipalAgent {
		return p.OnBehalfOf
	}
	return p.UserID
}

func fail(msg string, args ...any) cognition.Values {
	return cognition.Values{"error": fmt.Sprintf(msg, args...)}
}

// prepare resolves the agent, the role and the resource.
func prepare(x *cognition.Exec, in cognition.Values) (*authz.Policy, grantArgs, cognition.Values) {
	var a grantArgs
	pol := authz.Installed()
	if pol == nil {
		return nil, a, fail("access control is not installed")
	}
	s, _ := cognition.Get[string](in, "agent")
	u, _ := cognition.Get[string](in, "user")
	switch {
	case s != "" && u != "":
		return nil, a, fail("give an agent or a user, not both")
	case u != "":
		id, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(u), "user:"), 10, 64)
		if err != nil || id == 0 {
			return nil, a, fail("%q is not a user's id", u)
		}
		a.user = id
	default:
		id, ok := authz.ParseAgent(s)
		if !ok {
			return nil, a, fail("%q is not an agent's id", s)
		}
		if _, found, err := authz.FindAgent(x.Context(), x.Principal.TenantID, id); err != nil || !found {
			return nil, a, fail("there is no agent %d", id)
		}
		a.agent = id
	}

	a.resource = authz.Tenant(x.Principal)
	entity, _ := cognition.Get[string](in, "entity")
	record, _ := cognition.Get[string](in, "record")
	resource, _ := cognition.Get[string](in, "resource")
	switch {
	case record != "" && entity == "":
		return nil, a, fail("a record needs its entity type")
	case record != "":
		a.resource = authz.RecordResource(records.Resource(x.Principal, entity), record)
	case resource != "":
		a.resource = resource
	}
	if authz.TenantOf(a.resource) != x.Principal.TenantID {
		return nil, a, fail("%s is not in this tenant", a.resource)
	}

	name, _ := cognition.Get[string](in, "role")
	roles, err := pol.Roles()
	if err != nil {
		return nil, a, fail("roles cannot be read")
	}
	app, _ := strings.CutPrefix(x.Owner, "app:")
	found := false
	if n, err := strconv.ParseUint(name, 10, 64); err == nil {
		a.role, found = roles[n]
	}
	if !found {
		// An app role by its manifest id: the running graph's app's first
		for _, r := range roles {
			if r.AppRole == name && r.TenantID == x.Principal.TenantID && (!found || r.App == app) {
				a.role, found = r, true
			}
		}
	}
	if !found || (!a.role.BuiltIn && a.role.TenantID != x.Principal.TenantID) {
		return nil, a, fail("there is no role %q", name)
	}
	if !authz.GrantableOn(a.role, a.resource) {
		return nil, a, fail("%s cannot be granted on %s", a.role.Name, a.resource)
	}
	return pol, a, nil
}
