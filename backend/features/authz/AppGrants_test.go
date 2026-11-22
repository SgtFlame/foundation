package authz

import (
	"context"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// An app grants its own roles on its own records, to people too (forge
// 7o: Mail keeps an account's members' grants), and nothing else; an
// Owner is told apart from an Administrator (principal.owner).
func TestAppGrantsItsOwnRolesAndOwner(t *testing.T) {
	ctx := context.Background()
	pol := testPolicy(t)
	ResourceTypes.Register(pol.registry, ResourceType{
		Descriptor: extensions.Descriptor{ID: "record"}, Parent: "tenant", Fence: "app",
		Actions: []Action{{Name: "read", Class: ClassRead}},
	})
	Install(pol)
	t.Cleanup(func() { Install(nil) })
	mail, err := pol.SyncAppRoles(ctx, 1, "mail", []AppRole{{ID: "member", Name: "Member", Permissions: []string{"app:mail/entity:account/record.read"}}})
	if err != nil {
		t.Fatal(err)
	}
	other, err := pol.SyncAppRoles(ctx, 1, "crm", []AppRole{{ID: "member", Name: "Member", Permissions: []string{"app:crm/entity:deal/record.read"}}})
	if err != nil {
		t.Fatal(err)
	}
	roles, _ := pol.Roles()
	app := features.AppPrincipal(1, "mail")
	account := "tenant:1/app:mail/entity:account/record:7"
	if d := pol.MayGrant(ctx, app, roles[mail["member"]], account); !d.Allowed {
		t.Errorf("the app may not grant its own role on its own record: %s", d.Reason)
	}
	if d := pol.MayGrant(ctx, app, roles[other["member"]], "tenant:1/app:crm/entity:deal/record:7"); d.Allowed {
		t.Error("an app granted another app's role")
	}
	if d := pol.MayGrant(ctx, app, roles[mail["member"]], "tenant:1"); d.Allowed {
		t.Error("an app granted its role on the tenant")
	}
	if d := pol.MayGrant(ctx, app, roles[RoleEditor], account); d.Allowed {
		t.Error("an app granted a tenant role")
	}
	if d := pol.MayGrant(ctx, features.AppPrincipal(2, "mail"), roles[mail["member"]], "tenant:2/app:mail/entity:account/record:7"); d.Allowed {
		t.Error("an app granted another tenant's role")
	}
	if _, err := pol.Grant(ctx, nil, GrantArgs{UserID: 3, RoleID: mail["member"], Resource: account}); err != nil {
		t.Fatal(err)
	}
	pol.Changed()
	if !HasRole(ctx, features.Principal{Kind: features.PrincipalUser, UserID: 3, TenantID: 1}, "member", "7") {
		t.Error("the user granted Member on 7 does not hold it")
	}

	for _, g := range []GrantArgs{{UserID: 4, RoleID: RoleOwner, Resource: "tenant:1"}, {UserID: 5, RoleID: RoleAdmin, Resource: "tenant:1"}, {UserID: 6, RoleID: RoleOwner, Resource: "tenant:2"}} {
		if _, err := pol.Grant(ctx, nil, g); err != nil {
			t.Fatal(err)
		}
	}
	pol.Changed()
	user := func(id uint64) features.Principal { return features.Principal{Kind: features.PrincipalUser, UserID: id, TenantID: 1} }
	if !IsOwner(ctx, user(4)) || IsOwner(ctx, user(5)) || IsOwner(ctx, user(6)) || IsOwner(ctx, app) {
		t.Errorf("owners: 4 %v, 5 %v, 6 %v, app %v", IsOwner(ctx, user(4)), IsOwner(ctx, user(5)), IsOwner(ctx, user(6)), IsOwner(ctx, app))
	}
}
