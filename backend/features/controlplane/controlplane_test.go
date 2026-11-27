package controlplane

import (
	"context"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"gorm.io/gorm"
)

func fixture(t *testing.T) (*gorm.DB, *authz.Policy) {
	t.Helper()
	gdb, err := db.Open(db.WithPath(t.TempDir() + "/app.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, append(identity.Models(), authz.Models()...)...); err != nil {
		t.Fatal(err)
	}
	reg := extensions.NewRegistry()
	authz.Contribute(reg)
	identity.ContributeAccess(reg)
	ContributeAccess(reg)
	pol := authz.NewPolicy(gdb, reg)
	authz.Install(pol)
	t.Cleanup(func() { authz.Install(nil) })
	for _, u := range []uint64{features.LocalUserID, 20, 21, 22} {
		if err := gdb.Create(&identity.User{Model: db.Model{ID: u}, Username: "u" + string(rune('a'+u%26))}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return gdb, pol
}

func user(id, tenant uint64) features.Principal {
	return features.Principal{Kind: features.PrincipalUser, UserID: id, TenantID: tenant}
}

func TestRootTenant(t *testing.T) {
	ctx := context.Background()
	gdb, pol := fixture(t)
	root := authz.Resource("tenant", features.RootTenantID)

	made, err := EnsureRootTenant(ctx, gdb, 20)
	if err != nil || !made {
		t.Fatalf("made %v: %v", made, err)
	}
	if made, err := EnsureRootTenant(ctx, gdb, 21); err != nil || made {
		t.Fatalf("a second call made %v: %v", made, err)
	}
	var tn identity.Tenant
	if err := gdb.First(&tn, features.RootTenantID).Error; err != nil || tn.Name != RootTenantName {
		t.Fatalf("root tenant %+v %v", tn, err)
	}
	// its Owner holds every infra action; the second call's user nothing
	for _, a := range []string{"infra.read", "infra.operate", "infra.admin", "infra.enroll", "infra.approve"} {
		if d := pol.Explain(ctx, user(20, features.RootTenantID), a, root); !d.Allowed {
			t.Errorf("the root tenant's Owner may not %s: %s", a, d.Reason)
		}
	}
	if d := pol.Explain(ctx, user(21, features.RootTenantID), "infra.read", root); d.Allowed {
		t.Fatal("a non-member reads infrastructure")
	}

	// The Super Administrator holds nothing of infra (part question 1)
	if _, err := pol.Grant(ctx, gdb, authz.GrantArgs{UserID: features.LocalUserID, RoleID: authz.RoleSuperAdmin, Resource: authz.OnApp}); err != nil {
		t.Fatal(err)
	}
	// A viewer in the root tenant reads and does not operate
	if _, err := pol.Grant(ctx, gdb, authz.GrantArgs{UserID: 21, RoleID: RoleInfraViewer, Resource: root}); err != nil {
		t.Fatal(err)
	}
	// An administrator role granted in tenant 1 holds nothing
	if _, err := pol.Grant(ctx, gdb, authz.GrantArgs{UserID: 22, RoleID: RoleInfraAdmin, Resource: authz.Resource("tenant", features.LocalTenantID)}); err != nil {
		t.Fatal(err)
	}
	pol.Changed()
	if d := pol.Explain(ctx, user(features.LocalUserID, features.RootTenantID), "infra.read", root); d.Allowed {
		t.Fatalf("the Super Administrator reads infrastructure: %s", d.Reason)
	}
	if d := pol.Explain(ctx, user(21, features.RootTenantID), "infra.read", root+"/host:h1"); !d.Allowed {
		t.Fatalf("the viewer may not read: %s", d.Reason)
	}
	if d := pol.Explain(ctx, user(21, features.RootTenantID), "infra.operate", root); d.Allowed {
		t.Fatal("the viewer operates")
	}
	tenant1 := authz.Resource("tenant", features.LocalTenantID)
	if d := pol.Explain(ctx, user(22, features.LocalTenantID), "infra.admin", tenant1); d.Allowed {
		t.Fatalf("infra.admin held in tenant 1: %s", d.Reason)
	}
	if d := pol.Explain(ctx, user(22, features.LocalTenantID), "infra.admin", root); d.Allowed {
		t.Fatalf("a grant in tenant 1 reaches the root tenant: %s", d.Reason)
	}
	// A later owner
	if err := AddRootOwner(ctx, gdb, 22); err != nil {
		t.Fatal(err)
	}
	if d := pol.Explain(ctx, user(22, features.RootTenantID), "infra.approve", root); !d.Allowed {
		t.Fatalf("the added Owner may not approve: %s", d.Reason)
	}
}

func TestRootTenantWithoutOwner(t *testing.T) {
	gdb, _ := fixture(t)
	if made, err := EnsureRootTenant(context.Background(), gdb, 0); err != nil || !made {
		t.Fatalf("made %v: %v", made, err)
	}
	var n int64
	gdb.Model(&identity.Membership{}).Where("tenant_id = ?", features.RootTenantID).Count(&n)
	if n != 0 {
		t.Fatalf("%d members", n)
	}
	if _, err := EnsureRootTenant(context.Background(), gdb, 99); err != nil {
		t.Fatalf("an existing root tenant ignores the owner: %v", err)
	}
}
