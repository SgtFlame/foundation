package entrystore

import (
	"context"
	"errors"
	"maps"
	"sync"

	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/db"
)

// Tenants implements chronicle.Store over one entries table per tenant, each
// in the tenant's own LanceDB directory (db.TenantDir), opened when first
// used. Writes and searches go to the row's or owner's tenant; Hashes and
// Delete, which name no tenant, cover every tenant with a directory.
type Tenants struct {
	root       string
	dimensions int

	mu     sync.Mutex
	stores map[uint64]*Store
}

// OpenTenants keeps the tenants' entries tables under root (the LanceDB
// root, <data-dir>/lancedb).
func OpenTenants(root string, dimensions int) *Tenants {
	return &Tenants{root: root, dimensions: dimensions, stores: map[uint64]*Store{}}
}

// For opens tenant's table, creating it on first use.
func (t *Tenants) For(ctx context.Context, tenant uint64) (*Store, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.stores[tenant]; ok {
		return s, nil
	}
	s, err := Open(ctx, db.TenantDir(t.root, tenant), t.dimensions)
	if err != nil {
		return nil, err
	}
	t.stores[tenant] = s
	return s, nil
}

// all opens the table of every tenant with a directory.
func (t *Tenants) all(ctx context.Context) ([]*Store, error) {
	tenants, err := db.TenantDirs(t.root)
	if err != nil {
		return nil, err
	}
	out := make([]*Store, 0, len(tenants))
	for _, id := range tenants {
		s, err := t.For(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (t *Tenants) Dimensions() int { return t.dimensions }

func (t *Tenants) Put(ctx context.Context, row chronicle.Row) error {
	s, err := t.For(ctx, row.TenantID)
	if err != nil {
		return err
	}
	return s.Put(ctx, row)
}

func (t *Tenants) Delete(ctx context.Context, entryID uint64) error {
	stores, err := t.all(ctx)
	if err != nil {
		return err
	}
	for _, s := range stores {
		if err := s.Delete(ctx, entryID); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tenants) Hashes(ctx context.Context) (map[uint64]string, error) {
	stores, err := t.all(ctx)
	if err != nil {
		return nil, err
	}
	out := map[uint64]string{}
	for _, s := range stores {
		h, err := s.Hashes(ctx)
		if err != nil {
			return nil, err
		}
		maps.Copy(out, h)
	}
	return out, nil
}

func (t *Tenants) VectorSearch(ctx context.Context, owner chronicle.Owner, vector []float32, k int) ([]chronicle.Match, error) {
	s, err := t.For(ctx, owner.TenantID)
	if err != nil {
		return nil, err
	}
	return s.VectorSearch(ctx, owner, vector, k)
}

func (t *Tenants) KeywordSearch(ctx context.Context, owner chronicle.Owner, query string, k int) ([]chronicle.Match, error) {
	s, err := t.For(ctx, owner.TenantID)
	if err != nil {
		return nil, err
	}
	return s.KeywordSearch(ctx, owner, query, k)
}

// Close closes every opened table.
func (t *Tenants) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	var errs []error
	for id, s := range t.stores {
		errs = append(errs, s.Close())
		delete(t.stores, id)
	}
	return errors.Join(errs...)
}
