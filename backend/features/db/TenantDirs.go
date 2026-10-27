package db

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// Each tenant keeps its own LanceDB directory under one root, so removing a
// tenant drops its directory (documentation/plans/MultiTenant.md › Storage):
// <data-dir>/lancedb/<tenantId>/.

// TenantDir is tenant's directory under root.
func TenantDir(root string, tenant uint64) string {
	return filepath.Join(root, strconv.FormatUint(tenant, 10))
}

// TenantDirs lists the tenants that have a directory under root.
func TenantDirs(root string) ([]uint64, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tenants []uint64
	for _, e := range entries {
		if id, ok := tenantOf(e); ok {
			tenants = append(tenants, id)
		}
	}
	return tenants, nil
}

// MoveToTenantDir moves everything directly in root that is not a tenant's
// directory into tenant's: the one-time move of the single-tenant layout,
// where the tables sat in root itself (seam 7 in Index.md). It does nothing
// once root holds only tenant directories.
func MoveToTenantDir(root string, tenant uint64) error {
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	dir := TenantDir(root, tenant)
	for _, e := range entries {
		if _, ok := tenantOf(e); ok {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		to := filepath.Join(dir, e.Name())
		if _, err := os.Stat(to); err == nil {
			return fmt.Errorf("db: cannot move %s into %s: it already exists there", e.Name(), dir)
		}
		if err := os.Rename(filepath.Join(root, e.Name()), to); err != nil {
			return err
		}
	}
	return nil
}

// tenantOf reads a tenant directory's name, which is only digits.
func tenantOf(e fs.DirEntry) (uint64, bool) {
	if !e.IsDir() {
		return 0, false
	}
	id, err := strconv.ParseUint(e.Name(), 10, 64)
	return id, err == nil && id > 0
}
