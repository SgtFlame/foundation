package db

import (
	"path/filepath"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
)

type draft struct {
	Model
	Tenanted
	Owned
	Title string
}

func TestTenantScope(t *testing.T) {
	gdb, err := Open(WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(gdb, &draft{}); err != nil {
		t.Fatal(err)
	}

	local := features.LocalPrincipal()
	other := features.Principal{Kind: features.PrincipalUser, UserID: 2, TenantID: 2}
	for _, d := range []draft{
		{Tenanted: TenantOf(local), Owned: OwnedBy(local), Title: "mine"},
		{Tenanted: TenantOf(other), Owned: OwnedBy(other), Title: "theirs"},
	} {
		if err := gdb.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}

	var got []draft
	if err := gdb.Scopes(InTenant(local.TenantID)).Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "mine" || got[0].OwnerID != local.UserID {
		t.Errorf("tenant %d sees %+v", local.TenantID, got)
	}
}
