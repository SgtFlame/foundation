// Package authznodes is access control's Cognition nodes (forge 07j):
// Grant role and Revoke role give an agent a role on a resource (often one
// record), as the run's principal, who needs the right to grant it.
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
	GrantID  = "authz.grantRole"
	RevokeID = "authz.revokeRole"
)

var kinds = []string{livenodes.KindAction, livenodes.KindTrigger, uinodes.KindHandler, cognition.KindFunction, cognition.KindAgent}

var inPins = []cognition.Pin{
	cognition.ExecPin(cognition.Then, ""),
	cognition.DataPin("agent", "Agent", "string", "The agent's id (its record's id), or agent:<id>"),
	cognition.DataPin("role", "Role", "string", "The role: an app role's id in its manifest (mailSender), or a role's id"),
	{ID: "entity", Name: "Entity", Type: "string", Optional: true, Description: "With Record, the entity type of the one record it is granted on"},
	{ID: "record", Name: "Record", Type: "string", Optional: true, Description: "The id of the one record it is granted on, e.g. a mail account"},
	{ID: "resource", Name: "Resource", Type: "string", Optional: true, Description: "A resource path in place of a record; the tenant when neither is given"},
}

// Contribute registers the nodes.
func Contribute(reg *extensions.Registry) {
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: GrantID, Source: source, Order: 960}, Title: "Grant role", Category: "Access",
		Description: "Gives an agent a role on one record (or another resource), as the run's principal, who needs grant.write there and every permission of the role: an agent never holds more than its granter could give. Granting it again changes nothing. Leaves by Failed with the reason when refused.",
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
			g, err := pol.Grant(x.Context(), nil, authz.GrantArgs{AgentID: a.agent, RoleID: a.role.ID, Resource: a.resource, By: grantor(x.Principal)})
			if err != nil {
				return cognition.Values{"error": err.Error()}, "failed", nil
			}
			return cognition.Values{"grant": strconv.FormatUint(g.ID, 10)}, cognition.Then, nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: RevokeID, Source: source, Order: 961}, Title: "Revoke role", Category: "Access",
		Description: "Takes a role an agent holds on one record (or another resource) away, as the run's principal, with the same right as Grant role; the agent's next check fails. Leaves by Failed when refused or when the agent does not hold it there.",
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
				if g.AgentID == a.agent && g.RoleID == a.role.ID {
					if _, err := pol.Revoke(x.Context(), nil, g.ID, grantor(x.Principal)); err != nil {
						return cognition.Values{"error": err.Error()}, "failed", nil
					}
					return cognition.Values{}, cognition.Then, nil
				}
			}
			return cognition.Values{"error": "the agent does not hold that role there"}, "failed", nil
		},
	})
}

// grantArgs are a node's inputs, resolved.
type grantArgs struct {
	agent    uint64
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
	id, ok := authz.ParseAgent(s)
	if !ok {
		return nil, a, fail("%q is not an agent's id", s)
	}
	if _, found, err := authz.FindAgent(x.Context(), x.Principal.TenantID, id); err != nil || !found {
		return nil, a, fail("there is no agent %d", id)
	}
	a.agent = id

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
