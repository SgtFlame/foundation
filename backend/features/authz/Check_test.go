package authz

import (
	"context"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

func testPolicy(t *testing.T) *Policy {
	t.Helper()
	gdb, err := db.Open(db.WithPath(t.TempDir() + "/app.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, Models()...); err != nil {
		t.Fatal(err)
	}
	reg := extensions.NewRegistry()
	Contribute(reg)
	ResourceTypes.Register(reg, ResourceType{
		Descriptor: extensions.Descriptor{ID: "tenant"}, Parent: "app",
		Actions: []Action{{Name: "create", Class: ClassCreate, On: OnApp}, {Name: "read", Class: ClassRead}, {Name: "delete", Class: ClassDelete}},
	})
	ResourceTypes.Register(reg, ResourceType{
		Descriptor: extensions.Descriptor{ID: "volume"}, Parent: "tenant",
		Actions: []Action{{Name: "read", Class: ClassRead}, {Name: "write", Class: ClassWrite}},
	})
	ResourceTypes.Register(reg, ResourceType{
		Descriptor: extensions.Descriptor{ID: "config"}, Parent: "tenant", Owned: true,
		Actions: []Action{{Name: "read", Class: ClassSelf}, {Name: "write", Class: ClassAdmin}},
	})
	return NewPolicy(gdb, reg)
}

func TestPolicy(t *testing.T) {
	ctx := context.Background()
	pol := testPolicy(t)
	user := func(id, tenant uint64) features.Principal {
		return features.Principal{Kind: features.PrincipalUser, UserID: id, TenantID: tenant}
	}
	grant := func(u, role uint64, res string) RoleGrant {
		g, err := pol.Grant(ctx, nil, GrantArgs{UserID: u, RoleID: role, Resource: res})
		if err != nil {
			t.Fatalf("grant %d %d %s: %v", u, role, res, err)
		}
		return g
	}
	grant(1, RoleSuperAdmin, "app")
	grant(2, RoleViewer, "tenant:1")
	grant(2, RoleEditor, "tenant:1/volume:42")
	grant(3, RoleTenantAdmin, "app")
	grant(4, RoleAdmin, "tenant:1")
	soon := time.Now().Add(-time.Minute)
	if _, err := pol.Grant(ctx, nil, GrantArgs{UserID: 5, RoleID: RoleEditor, Resource: "tenant:1", ExpiresAt: &soon}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		p        features.Principal
		action   string
		resource string
		allowed  bool
	}{
		{"anonymous signs in", features.AnonymousPrincipal(), "session.login", "app", true},
		{"anonymous reads nothing", features.AnonymousPrincipal(), "volume.read", "tenant:1", false},
		{"background work", features.ServicePrincipal(1), "tenant.create", "app", true},
		{"super administrator does anything", user(1, 1), "volume.write", "tenant:9/volume:1", true},
		{"viewer reads", user(2, 1), "volume.read", "tenant:1/volume:7", true},
		{"viewer cannot write", user(2, 1), "volume.write", "tenant:1/volume:7", false},
		{"editor on one volume writes it", user(2, 1), "volume.write", "tenant:1/volume:42/file:3", true},
		{"a grant does not reach another tenant", user(2, 2), "volume.read", "tenant:2/volume:7", false},
		{"tenant administrator creates tenants", user(3, 1), "tenant.create", "app", true},
		{"tenant administrator deletes any tenant", user(3, 1), "tenant.delete", "tenant:7", true},
		{"tenant administrator reads no content", user(3, 1), "volume.read", "tenant:1/volume:7", false},
		{"administrator writes tenant settings", user(4, 1), "config.write", "tenant:1/config:chat", true},
		{"administrator cannot write app settings", user(4, 1), "config.write", "app/config:ai.models", false},
		{"everyone writes their own settings", user(6, 1), "config.write", "user:6/config:chat", true},
		{"but not someone else's", user(6, 1), "config.write", "user:2/config:chat", false},
		{"everyone reads settings", user(6, 1), "config.read", "app/config:ai.models", true},
		{"an expired grant gives nothing", user(5, 1), "volume.write", "tenant:1", false},
		{"an undeclared action is denied", user(1, 1), "nothing.here", "app", false},
		{"the administrator may grant", user(4, 1), "grant.write", "tenant:1/volume:42", true},
	}
	Install(pol)
	defer Install(nil)
	for _, c := range cases {
		if got := Check(ctx, c.p, c.action, c.resource); got.Allowed != c.allowed {
			t.Errorf("%s: allowed = %v (%s), want %v", c.name, got.Allowed, got.Reason, c.allowed)
		}
	}

	// No escalation: the administrator cannot grant Owner, which holds
	// tenant.delete, nor an app role on a tenant
	roles, _ := pol.Roles()
	if d := pol.MayGrant(ctx, user(4, 1), roles[RoleOwner], "tenant:1"); d.Allowed {
		t.Errorf("administrator may grant Owner: %s", d.Reason)
	}
	if d := pol.MayGrant(ctx, user(4, 1), roles[RoleEditor], "tenant:1/volume:1"); !d.Allowed {
		t.Errorf("administrator may not grant Editor: %s", d.Reason)
	}
	if _, err := pol.Grant(ctx, nil, GrantArgs{UserID: 2, RoleID: RoleSuperAdmin, Resource: "tenant:1"}); err != ErrWrongScope {
		t.Errorf("app role on a tenant: %v", err)
	}

	// The last Super Administrator stays
	supers, _ := pol.GrantsOn(nil, "app", false)
	for _, g := range supers {
		if g.RoleID == RoleSuperAdmin {
			if _, err := pol.Revoke(ctx, nil, g.ID, 1); err != ErrLastSuperUser {
				t.Errorf("revoked the last Super Administrator: %v", err)
			}
		}
	}

	// Leaving a tenant takes its grants
	if err := pol.RevokeUnder(ctx, nil, 2, "tenant:1", 1); err != nil {
		t.Fatal(err)
	}
	if d := Check(ctx, user(2, 1), "volume.read", "tenant:1/volume:42"); d.Allowed {
		t.Errorf("grants survived leaving the tenant: %s", d.Reason)
	}
	var audits int64
	pol.db.Model(&AccessAudit{}).Count(&audits)
	if audits == 0 {
		t.Error("no audit records")
	}
}
