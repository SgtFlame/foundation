package db

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestMoveToTenantDir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "lancedb")
	for _, table := range []string{"chunks.lance", "entries.lance"} {
		if err := os.MkdirAll(filepath.Join(root, table, "data"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(TenantDir(root, 7), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := MoveToTenantDir(root, 1); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"chunks.lance", "entries.lance"} {
		if _, err := os.Stat(filepath.Join(TenantDir(root, 1), table, "data")); err != nil {
			t.Errorf("%s was not moved: %v", table, err)
		}
	}
	tenants, err := TenantDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(tenants)
	if !slices.Equal(tenants, []uint64{1, 7}) {
		t.Errorf("tenants = %v, want [1 7]", tenants)
	}

	// A second start finds nothing to move
	if err := MoveToTenantDir(root, 1); err != nil {
		t.Fatal(err)
	}
	if err := MoveToTenantDir(filepath.Join(t.TempDir(), "missing"), 1); err != nil {
		t.Errorf("a missing root: %v", err)
	}
}
