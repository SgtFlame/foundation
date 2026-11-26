package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/retrieval/fuse"
)

// Indexer is the follower that keeps a tenant's search table: every
// record event, and each publication that changes which fields are
// searched.
const (
	Indexer     = "records.search"
	DefsIndexer = "records.searchDefs"
)

// Embed embeds a text for search, as a document or as a query; an error
// (no key yet) leaves the text to keyword search.
type Embed func(args features.ServiceArgs, text, title string, query bool) ([]float32, error)

// Index is the records' search index, data.Searcher: a Store per tenant
// under the LanceDB root.
type Index struct {
	root       string
	dimensions int
	embed      Embed
	deps       func() data.Deps

	mu     sync.Mutex
	stores map[uint64]*Store
	subs   map[uint64]map[chan live.Position]struct{}
}

// New keeps the tenants' search tables under root (<data-dir>/lancedb);
// embed may be nil, for keyword search alone.
func New(root string, dimensions int, embed Embed, deps func() data.Deps) *Index {
	return &Index{root: root, dimensions: dimensions, embed: embed, deps: deps, stores: map[uint64]*Store{}, subs: map[uint64]map[chan live.Position]struct{}{}}
}

// Close closes every tenant's table.
func (x *Index) Close() {
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, s := range x.stores {
		_ = s.Close()
	}
	x.stores = map[uint64]*Store{}
}

// For is a tenant's table, opened on first use.
func (x *Index) For(ctx context.Context, tenant uint64) (*Store, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if s, ok := x.stores[tenant]; ok {
		return s, nil
	}
	s, err := Open(ctx, db.TenantDir(x.root, tenant), x.dimensions)
	if err != nil {
		return nil, err
	}
	x.stores[tenant] = s
	return s, nil
}

// Follow starts the indexer in the principal's tenant.
func (x *Index) Follow(args features.ServiceArgs) error {
	d := x.deps()
	tenant := args.Principal.TenantID
	recs := fmt.Sprintf("evt.%d.%s.*.*.*", tenant, live.AreaRecords)
	if err := d.Projections.Follow(args.Context(), tenant, live.AreaRecords, Indexer, []string{recs}, x.indexEvent); err != nil {
		return err
	}
	defs := live.Target{Area: live.AreaDefinitions, Type: entities.TargetType, ID: "*"}.Subject(tenant, entities.EventPublished)
	return d.Projections.Follow(args.Context(), tenant, live.AreaDefinitions, DefsIndexer, []string{defs}, x.republished)
}

// Changes sends the position of each change to tenant's table.
func (x *Index) Changes(ctx context.Context, tenant uint64) <-chan live.Position {
	ch := make(chan live.Position, 16)
	x.mu.Lock()
	if x.subs[tenant] == nil {
		x.subs[tenant] = map[chan live.Position]struct{}{}
	}
	x.subs[tenant][ch] = struct{}{}
	x.mu.Unlock()
	go func() {
		<-ctx.Done()
		x.mu.Lock()
		delete(x.subs[tenant], ch)
		x.mu.Unlock()
	}()
	return ch
}

func (x *Index) changed(tenant uint64, pos live.Position) {
	x.mu.Lock()
	defer x.mu.Unlock()
	for ch := range x.subs[tenant] {
		select {
		case ch <- pos:
		default: // a run is coming anyway
		}
	}
}

func (x *Index) indexEvent(e live.Event) error {
	if e.Target.Type == data.CounterType {
		return nil
	}
	ctx := context.Background()
	d := x.deps()
	if err := data.WaitFolded(ctx, d, e.TenantID, e.Seq); err != nil {
		return err
	}
	args := features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipal(e.TenantID)}
	t, problems := data.Entity(args, d.Projections, e.Target.Type)
	var et *records.EntityType
	if problems == nil {
		et = &t
	}
	changed, err := x.indexRecord(args, d, et, e.Target.Type, e.Target.ID)
	if err != nil {
		return err
	}
	if changed {
		x.changed(e.TenantID, live.Position{e.Stream: e.Seq})
	}
	return nil
}

// indexRecord brings one record's row up to date: removed when the record
// is gone or has nothing to search, written again when its text changed.
// e is nil when the type has no published version.
func (x *Index) indexRecord(args features.ServiceArgs, d data.Deps, e *records.EntityType, entity, id string) (bool, error) {
	ctx := args.Context()
	s, err := x.For(ctx, args.Principal.TenantID)
	if err != nil {
		return false, err
	}
	key := data.Key(entity, id)
	var r data.Record
	if e != nil {
		v, ok, err := d.Projections.Get(ctx, args.Principal.TenantID, data.Projector, key)
		if err != nil {
			return false, err
		}
		if ok && v.Value != nil {
			_ = json.Unmarshal(v.Value, &r)
		}
	}
	text, title := "", ""
	if r != nil {
		text, title = records.SearchText(*e, r), records.TitleOf(*e, r)
	}
	if text == "" {
		had, err := s.Hash(ctx, key, false)
		if err != nil || had == "" {
			return false, err
		}
		return true, s.Delete(ctx, key)
	}
	sum := sha256.Sum256([]byte(title + "\n" + text))
	hash := hex.EncodeToString(sum[:8])
	had, err := s.Hash(ctx, key, x.embed != nil)
	if err != nil {
		return false, err
	}
	if had == hash {
		return false, nil
	}
	row := Row{Entity: entity, RecordID: id, Hash: hash, Title: title, Text: text}
	if x.embed != nil {
		if v, err := x.embed(args, text, title, false); err == nil {
			row.Vector = v
		} else {
			log.With("entity", entity).Debug(ctx, "record %s is searched by keywords only: %v", id, err)
		}
	}
	return true, s.Put(ctx, row)
}

// republished indexes an entity type's records again when a publication
// changes which of its fields are searched.
func (x *Index) republished(e live.Event) error {
	var p entities.Publication
	if err := json.Unmarshal(e.Data, &p); err != nil {
		return fmt.Errorf("cannot read the publication: %w", err)
	}
	if !p.SearchChanged {
		return nil
	}
	ctx := context.Background()
	args := features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipal(e.TenantID)}
	s, err := x.For(ctx, e.TenantID)
	if err != nil {
		return err
	}
	if err := s.DeleteEntity(ctx, e.Target.ID); err != nil {
		return err
	}
	d := x.deps()
	all, err := data.RecordsOf(d, e.TenantID, e.Target.ID)
	if err != nil {
		return err
	}
	for _, r := range all {
		var id string
		_ = json.Unmarshal(r[data.MetaID], &id)
		if _, err := x.indexRecord(args, d, &p.Entity, e.Target.ID, id); err != nil {
			return err
		}
	}
	x.changed(e.TenantID, live.Position{})
	return nil
}

// Search ranks the tenant's records for a text. Toward keywords (the dial
// at 0 or below) only records with a keyword match are found, the vectors
// only ordering them; above 0, records near in meaning join them.
func (x *Index) Search(args features.ServiceArgs, q data.SearchQuery) ([]data.SearchHit, error) {
	ctx := args.Context()
	s, err := x.For(ctx, args.Principal.TenantID)
	if err != nil {
		return nil, err
	}
	keyword, err := s.KeywordSearch(ctx, q.Entity, q.Text, q.Limit)
	if err != nil {
		return nil, err
	}
	var vector []Match
	if x.embed != nil && q.LexicalSemantic > -5 {
		if v, err := x.embed(args, q.Text, "", true); err == nil {
			if vector, err = s.VectorSearch(ctx, q.Entity, v, q.Limit); err != nil {
				return nil, err
			}
		}
	}
	weight := float64(max(-5, min(5, q.LexicalSemantic))+5) / 10
	fused := fuse.Fuse(vector, keyword, func(m Match) string { return m.Entity + "." + m.RecordID }, weight, fuse.DefaultK)
	out := make([]data.SearchHit, 0, len(fused))
	for _, f := range fused {
		if q.LexicalSemantic <= 0 && f.KeywordRank == 0 {
			continue
		}
		out = append(out, data.SearchHit{Entity: f.Item.Entity, ID: f.Item.RecordID, Score: f.Score})
	}
	return out, nil
}
