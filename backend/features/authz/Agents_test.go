package authz

import (
	"context"
	"slices"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// fakeAgents is a directory of agents for tests.
type fakeAgents map[uint64]AgentInfo

func (f fakeAgents) Agent(_ context.Context, tenant, id uint64) (AgentInfo, bool, error) {
	a, ok := f[id]
	return a, ok && a.TenantID == tenant, nil
}

func (f fakeAgents) List(_ context.Context, tenant uint64) ([]AgentInfo, error) {
	var out []AgentInfo
	for _, a := range f {
		if a.TenantID == tenant {
			out = append(out, a)
		}
	}
	return out, nil
}

// Agents hold roles as users do, on records too (forge 07j): a grant on one
// account gives its role on that account and, by the role's row rules, on
// its messages; a revoke fails the next check; a retired agent may nothing.
func TestAgentGrants(t *testing.T) {
	ctx := context.Background()
	pol := testPolicy(t)
	ResourceTypes.Register(pol.registry, ResourceType{
		Descriptor: extensions.Descriptor{ID: "record"}, Parent: "tenant", Fence: "app",
		Actions: []Action{{Name: "read", Class: ClassRead}, {Name: "create", Class: ClassCreate}},
	})
	ResourceTypes.Register(pol.registry, ResourceType{
		Descriptor: extensions.Descriptor{ID: "connection"}, Parent: "tenant",
		Actions: []Action{{Name: "use", Class: ClassExecute}},
	})
	ContributeAgents(pol.registry)
	Install(pol)
	dir := fakeAgents{50: {ID: 50, TenantID: 1, Name: "Triage", Entity: "agent", App: "agents", CreatedBy: 2, Resource: "tenant:1/app:agents/entity:agent/record:50"}}
	SetAgents(dir)
	t.Cleanup(func() { Install(nil); SetAgents(nil) })

	ids, err := pol.SyncAppRoles(ctx, 1, "mail", []AppRole{
		{ID: "mailSender", Name: "Mail sender", Permissions: []string{"app:mail/entity:account/record.read", "app:mail/entity:message/record.read", "app:mail/entity:message/record.create"},
			Rules: map[string]string{"message": "principal.hasRole('mailSender', record.account)"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := features.AgentPrincipal(1, 50, 2)
	account := "tenant:1/app:mail/entity:account"
	message := "tenant:1/app:mail/entity:message"

	if d := Check(ctx, agent, "record.read", account); d.Allowed {
		t.Fatalf("an agent with no grant read: %s", d.Reason)
	}
	g, err := pol.Grant(ctx, nil, GrantArgs{AgentID: 50, RoleID: ids["mailSender"], Resource: account + "/record:7", By: 2})
	if err != nil || g.UserID != 0 || g.AgentID != 50 {
		t.Fatal(g, err)
	}
	if _, err := pol.Grant(ctx, nil, GrantArgs{UserID: 3, RoleID: RoleEditor, Resource: "tenant:1"}); err != nil {
		t.Fatal(err)
	}
	if grants, _ := pol.GrantsOf(0); len(grants) != 0 {
		t.Errorf("an agent's grant counted as user 0's: %v", grants)
	}

	for _, c := range []struct {
		action, res, key string
		allowed          bool
		rules            []string
	}{
		{"record.read", account, "account", true, []string{`record._id == "7"`}},
		{"record.read", message, "message", true, []string{"principal.hasRole('mailSender', record.account)"}},
		{"record.create", message, "message", true, []string{"principal.hasRole('mailSender', record.account)"}},
		{"record.create", account, "account", false, nil},                        // the role cannot create accounts
		{"record.read", "tenant:1/app:mail/entity:folder", "folder", false, nil}, // no rule reaches folders
		{"record.read", "tenant:1/app:tasks/entity:task", "task", false, nil},    // another app
	} {
		d := Check(ctx, agent, c.action, c.res)
		rules, restricted := RowRules(ctx, agent, c.action, c.res, c.key)
		if d.Allowed != c.allowed || c.allowed && (!restricted || !slices.Equal(rules, c.rules)) {
			t.Errorf("%s on %s: %v (%s), rules %v %v", c.action, c.res, d.Allowed, d.Reason, rules, restricted)
		}
	}
	if !HasRole(ctx, agent, "mailSender", "7") || !HasRole(ctx, agent, "mailSender", map[string]any{"id": "7"}) {
		t.Error("hasRole on the granted account")
	}
	if HasRole(ctx, agent, "mailSender", "8") || HasRole(ctx, features.Principal{Kind: features.PrincipalUser, UserID: 3, TenantID: 1}, "mailSender", "7") {
		t.Error("hasRole beyond the grant")
	}

	// A grant on a connection, checked as any other resource
	pol.Grant(ctx, nil, GrantArgs{AgentID: 50, RoleID: RoleEditor, Resource: "tenant:1/connectors:mail/connection:c1"})
	if !Check(ctx, agent, "connection.use", "tenant:1/connectors:mail/connection:c1").Allowed || Check(ctx, agent, "connection.use", "tenant:1/connectors:mail/connection:c2").Allowed {
		t.Error("an agent's grant on one connection")
	}

	// Revoking fails the next check
	if _, err := pol.Revoke(ctx, nil, g.ID, 2); err != nil {
		t.Fatal(err)
	}
	if Check(ctx, agent, "record.read", account).Allowed {
		t.Error("a revoked grant still allowed")
	}

	// Using an agent: its creator, and who holds agent.use on its record
	if _, err := MayUseAgent(ctx, features.Principal{Kind: features.PrincipalUser, UserID: 2, TenantID: 1}, 50); err != nil {
		t.Errorf("the creator may use the agent: %v", err)
	}
	if _, err := MayUseAgent(ctx, features.Principal{Kind: features.PrincipalUser, UserID: 3, TenantID: 1}, 50); err == nil {
		t.Error("an Editor used an app's agent behind the fence")
	}
	p, err := AsAgent(ctx, features.Principal{Kind: features.PrincipalUser, UserID: 2, TenantID: 1}, 50)
	if err != nil || p.Kind != features.PrincipalAgent || p.AgentID != 50 || p.OnBehalfOf != 2 {
		t.Errorf("AsAgent: %+v %v", p, err)
	}

	// A retired agent may nothing
	delete(dir, 50)
	if d := Check(ctx, agent, "connection.use", "tenant:1/connectors:mail/connection:c1"); d.Allowed {
		t.Error("a retired agent was allowed")
	}
}
