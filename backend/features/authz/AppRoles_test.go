package authz

import (
	"context"
	"slices"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// An app's roles (forge 05c): scoped permissions, the app fence, row rules
// per role, and their sync.
func TestAppRoles(t *testing.T) {
	ctx := context.Background()
	pol := testPolicy(t)
	ResourceTypes.Register(pol.registry, ResourceType{
		Descriptor: extensions.Descriptor{ID: "record"}, Parent: "tenant", Fence: "app",
		Actions: []Action{{Name: "read", Class: ClassRead}, {Name: "update", Class: ClassWrite}},
	})
	Install(pol)
	t.Cleanup(func() { Install(nil) })
	user := func(id uint64) features.Principal {
		return features.Principal{Kind: features.PrincipalUser, UserID: id, TenantID: 1}
	}
	ids, err := pol.SyncAppRoles(ctx, 1, "tasks", []AppRole{
		{ID: "manager", Name: "Manager", Permissions: []string{"app:tasks/entity:task/record.read", "app:tasks/entity:task/record.update"}},
		{ID: "member", Name: "Member", Permissions: []string{"app:tasks/entity:task/record.read"}, Rules: map[string]string{"task": "record.assignee == principal.user"}},
	})
	if err != nil || len(ids) != 2 {
		t.Fatal(ids, err)
	}
	grant := func(u, role uint64, res string) {
		if _, err := pol.Grant(ctx, nil, GrantArgs{UserID: u, RoleID: role, Resource: res}); err != nil {
			t.Fatalf("grant %d %d %s: %v", u, role, res, err)
		}
	}
	grant(1, RoleOwner, "tenant:1")
	grant(2, RoleEditor, "tenant:1")
	grant(2, ids["member"], "tenant:1")
	grant(3, RoleEditor, "tenant:1")
	grant(4, ids["manager"], "tenant:1/app:tasks")
	if _, err := pol.Grant(ctx, nil, GrantArgs{UserID: 5, RoleID: ids["member"], Resource: "tenant:1/volume:9"}); err != ErrWrongScope {
		t.Errorf("an app's role went outside the app: %v", err)
	}

	task, other := "tenant:1/app:tasks/entity:task", "tenant:1/app:tasks/entity:note"
	for _, c := range []struct {
		user           uint64
		action, res    string
		want           bool
		rules          []string
		restrictedWant bool
	}{
		{1, "record.update", task, true, nil, false},                                        // the Owner crosses the fence
		{2, "record.read", task, true, []string{"record.assignee == principal.user"}, true}, // a Member, held to its rule
		{2, "record.update", task, false, nil, false},                                       // Member does not update; Editor stops at the fence
		{2, "record.read", other, false, nil, false},                                        // nor reads another type of the app
		{2, "record.read", "tenant:1/records/entity:note", true, nil, false},                // the tenant's types are the Editor's
		{3, "record.read", task, false, nil, false},                                         // an Editor alone
		{4, "record.update", task, true, nil, false},                                        // a Manager granted on the app
		{2, "volume.write", "tenant:1/volume:9", true, nil, false},                          // other types have no fence
	} {
		if d := Check(ctx, user(c.user), c.action, c.res); d.Allowed != c.want {
			t.Errorf("user %d %s on %s: %v (%s)", c.user, c.action, c.res, d.Allowed, d.Reason)
		}
		rules, restricted := RowRules(ctx, user(c.user), c.action, c.res, "task")
		if restricted != c.restrictedWant || !slices.Equal(rules, c.rules) {
			t.Errorf("user %d %s rules: %v %v", c.user, c.action, rules, restricted)
		}
	}
	// A second role without a rule lifts it
	grant(2, ids["manager"], "tenant:1")
	if _, restricted := RowRules(ctx, user(2), "record.read", task, "task"); restricted {
		t.Error("Manager's rule-less grant should lift Member's rule")
	}
	// The Owner may grant Member; an Editor may not
	roles, _ := pol.Roles()
	if !pol.MayGrant(ctx, user(1), roles[ids["member"]], "tenant:1").Allowed || pol.MayGrant(ctx, user(3), roles[ids["member"]], "tenant:1").Allowed {
		t.Error("MayGrant on an app's role")
	}

	// Sync again: Manager goes with its grants, Member changes in place
	again, err := pol.SyncAppRoles(ctx, 1, "tasks", []AppRole{{ID: "member", Name: "Member", Permissions: []string{"app:tasks/entity:task/record.update"}}})
	if err != nil || again["member"] != ids["member"] || len(again) != 1 {
		t.Fatal(again, err)
	}
	if Check(ctx, user(4), "record.update", task).Allowed {
		t.Error("a removed role's grant still allows")
	}
	if !Check(ctx, user(2), "record.update", task).Allowed {
		t.Error("the changed role does not allow")
	}
	if err := pol.RemoveAppRoles(ctx, 1, "tasks"); err != nil {
		t.Fatal(err)
	}
	if Check(ctx, user(2), "record.update", task).Allowed {
		t.Error("an uninstalled app's role still allows")
	}
}
