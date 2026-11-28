package docs_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/docs"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
)

// With a policy installed, a Volume's grants are the floor of its pages:
// a Viewer on the Volume reads them but does not change them, someone
// with no grant does neither, and the docs app's own triggers do both.
func TestVolumeFloor(t *testing.T) {
	reg := extensions.NewRegistry()
	authz.Contribute(reg)
	kbservice.ContributeAccess(reg)
	appDB, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "app.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(appDB, authz.Models()...); err != nil {
		t.Fatal(err)
	}
	pol := authz.NewPolicy(appDB, reg)
	authz.Install(pol)
	t.Cleanup(func() { authz.Install(nil) })
	ctx := context.Background()
	if _, err := pol.Grant(ctx, nil, authz.GrantArgs{UserID: 2, RoleID: authz.RoleViewer, Resource: "tenant:1/volume:7"}); err != nil {
		t.Fatal(err)
	}
	if _, err := pol.Grant(ctx, nil, authz.GrantArgs{UserID: 3, RoleID: authz.RoleEditor, Resource: "tenant:1/volume:7"}); err != nil {
		t.Fatal(err)
	}
	user := func(id uint64) features.Principal {
		return features.Principal{Kind: features.PrincipalUser, UserID: id, TenantID: 1}
	}
	for _, c := range []struct {
		name        string
		p           features.Principal
		volume      string
		read, write bool
	}{
		{"viewer", user(2), "7", true, false},
		{"editor", user(3), "7", true, true},
		{"no grant", user(4), "7", false, false},
		{"viewer elsewhere", user(2), "8", false, false},
		{"the docs app", features.AppPrincipal(1, docs.AppID), "7", true, true},
		{"another app", features.AppPrincipal(1, "tasks"), "7", false, false},
		{"not a volume id", user(3), "7/../8", false, false},
	} {
		if got := docs.MayUse(ctx, c.p, c.volume, "read"); got != c.read {
			t.Errorf("%s reads: %v", c.name, got)
		}
		if got := docs.MayUse(ctx, c.p, c.volume, "write"); got != c.write {
			t.Errorf("%s writes: %v", c.name, got)
		}
	}
}

// Only pages/<id>.doc.json is a page's document.
func TestPageIDOf(t *testing.T) {
	for path, want := range map[string]string{
		"pages/123.doc.json": "123", "pages/12a.doc.json": "", "notes/123.doc.json": "",
		"pages/123.md": "", "pages/../123.doc.json": "", "pages/sub/123.doc.json": "",
	} {
		got, ok := docs.PageIDOf(path)
		if got != want || ok != (want != "") {
			t.Errorf("%s: %q %v", path, got, ok)
		}
	}
}
