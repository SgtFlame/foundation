package identity

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

func TestMigrateRoles(t *testing.T) {
	ctx := context.Background()
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "app.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, append(Models(), authz.Models()...)...); err != nil {
		t.Fatal(err)
	}
	// Slice 10's table, with its administrator flag
	if err := gdb.Exec("ALTER TABLE users ADD COLUMN admin numeric").Error; err != nil {
		t.Fatal(err)
	}
	for _, u := range []struct {
		id    uint64
		admin bool
	}{{1, true}, {7, false}} {
		if err := gdb.Exec("INSERT INTO users (id, username, password_hash, admin) VALUES (?, ?, 'x', ?)", u.id, "u"+string(rune('0'+u.id)), u.admin).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []Membership{{TenantID: 1, UserID: 1}, {TenantID: 1, UserID: 7}, {TenantID: 5, UserID: 7}} {
		if err := gdb.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}
	reg := extensions.NewRegistry()
	authz.Contribute(reg)
	ContributeAccess(reg)
	pol := authz.NewPolicy(gdb, reg)
	if err := MigrateRoles(ctx, gdb, pol); err != nil {
		t.Fatal(err)
	}
	if err := MigrateRoles(ctx, gdb, pol); err != nil { // once only: the column is gone
		t.Fatal(err)
	}
	if gdb.Migrator().HasColumn(&User{}, "admin") {
		t.Error("the admin column is still there")
	}
	user := func(id, tenant uint64) features.Principal {
		return features.Principal{Kind: features.PrincipalUser, UserID: id, TenantID: tenant}
	}
	cases := []struct {
		p        features.Principal
		action   string
		resource string
		allowed  bool
	}{
		{user(1, 1), "tenant.create", "app", true},
		{user(1, 1), "tenant.delete", "tenant:1", true},
		{user(7, 1), "tenant.read", "tenant:1", true},
		{user(7, 1), "tenant.addMember", "tenant:1", false},
		{user(7, 5), "tenant.read", "tenant:5", true},
		{user(7, 1), "user.list", "app", false},
	}
	for _, c := range cases {
		if got := pol.Explain(ctx, c.p, c.action, c.resource); got.Allowed != c.allowed {
			t.Errorf("%d %s on %s: %v (%s), want %v", c.p.UserID, c.action, c.resource, got.Allowed, got.Reason, c.allowed)
		}
	}
	grants, _ := pol.GrantsOf(1)
	if len(grants) != 2 {
		t.Errorf("user 1's grants: %+v", grants)
	}
}

func TestBootstrapWithoutUsers(t *testing.T) {
	ctx := context.Background()
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "app.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, append(Models(), authz.Models()...)...); err != nil {
		t.Fatal(err)
	}
	reg := extensions.NewRegistry()
	authz.Contribute(reg)
	ContributeAccess(reg)
	pol := authz.NewPolicy(gdb, reg)
	if err := MigrateRoles(ctx, gdb, pol); err != nil {
		t.Fatal(err)
	}
	if d := pol.Explain(ctx, features.LocalPrincipal(), "tenant.create", "app"); !d.Allowed {
		t.Errorf("single-user mode cannot administer: %s", d.Reason)
	}
}
