package data

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/google/btree"
	"github.com/nats-io/nats.go/jetstream"
)

// Indexes (Forge's part 3d) are kept in one side bucket per tenant,
// PRJ_<tenant>_records_index, by the records.record projector itself: its
// Keep writes a record's index entries before the record is written, so
// an index is never behind the records (03-DataModel question 2). An entry
// is the key <entityId>.<indexId>.<token>….<id>, a token per field and the
// record's id after its length in three digits (EntryID), so ids sort as
// compareRecordIds does: a shorter TSID is an earlier one
// (records.SortTokens), with a list-like field's record entered once per
// item. Each index a query uses is loaded into an ordered tree in memory
// on first use and kept current by watching the bucket (03d question 1).

// IndexBucket is the side bucket of the index entries.
const IndexBucket = "records.index"

// Indexer is the durable consumer that rebuilds indexes a published
// version adds or changes.
const Indexer = "records.indexer"

// state is what the records keep in memory per set of projections: the
// published entity types the projector indexes by, and the loaded indexes.
type state struct {
	p     Projections
	mu    sync.Mutex
	types map[string]records.EntityType // <tenant>/<entityId>
	trees map[string]*tree              // <tenant>/<entityId>/<indexId>
}

var states sync.Map // Projections → *state

func stateOf(p Projections) *state {
	if s, ok := states.Load(p); ok {
		return s.(*state)
	}
	s, _ := states.LoadOrStore(p, &state{p: p, types: map[string]records.EntityType{}, trees: map[string]*tree{}})
	return s.(*state)
}

// published is the entity type records are indexed by; ok is false before
// it is first published.
func (s *state) published(ctx context.Context, tenant uint64, entity string) (records.EntityType, bool, error) {
	k := fmt.Sprintf("%d/%s", tenant, entity)
	s.mu.Lock()
	e, ok := s.types[k]
	s.mu.Unlock()
	if ok {
		return e, true, nil
	}
	v, ok, err := s.p.Get(ctx, tenant, entities.PublishedProjector, entities.TargetType+"."+entity)
	if err != nil || !ok {
		return e, false, err
	}
	if err := json.Unmarshal(v.Value, &e); err != nil {
		return e, false, err
	}
	s.mu.Lock()
	if _, set := s.types[k]; !set { // the indexer may have set a newer one meanwhile
		s.types[k] = e
	}
	e = s.types[k]
	s.mu.Unlock()
	return e, true, nil
}

func (s *state) setPublished(tenant uint64, e records.EntityType) {
	s.mu.Lock()
	s.types[fmt.Sprintf("%d/%s", tenant, e.ID)] = e
	s.mu.Unlock()
}

// IndexPrefix is where an index's entries start.
func IndexPrefix(entity, index string) string { return entity + "." + index + "." }

// IndexKeys are a record's entries in each index of e.
func IndexKeys(e records.EntityType, r Record) []string {
	var id string
	_ = json.Unmarshal(r[MetaID], &id)
	var out []string
	for _, x := range e.Indexes {
		for _, t := range EntryTokens(e, x, r) {
			out = append(out, IndexPrefix(e.ID, x.ID)+t+"."+EntryID(id))
		}
	}
	return out
}

// EntryID is a record id as an index entry ends with it.
func EntryID(id string) string { return fmt.Sprintf("%03d%s", len(id), id) }

// splitEntry is an entry's tokens and its record's id.
func splitEntry(entry string) (tokens, id string) {
	i := strings.LastIndexByte(entry, '.')
	if len(entry) < i+4 {
		return entry[:max(i, 0)], ""
	}
	return entry[:i], entry[i+4:]
}

// compareIDs orders record ids as their entries do: by length, then text.
func compareIDs(a, b string) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}

// EntryTokens are a record's entries in index x without the prefix and
// record id: its fields' tokens joined by '.', one per item of a
// list-like field.
func EntryTokens(e records.EntityType, x records.Index, r Record) []string {
	out := []string{""}
	for _, id := range x.Fields {
		f, _, _ := e.FieldByID(id)
		ts, err := records.SortTokens(f, r[id])
		if err != nil { // a value the field no longer fits sorts last
			ts = []string{records.Token([]byte{0xFF})}
		}
		next := make([]string, 0, len(out)*len(ts))
		for _, p := range out {
			for _, t := range ts {
				if p != "" {
					t = p + "." + t
				}
				next = append(next, t)
			}
		}
		out = next
	}
	return out
}

// keep is the records.record projector's Keep: the record's index entries
// under its type's published version. Every entry is written each time,
// so an attempt whose write was refused (the record changed meanwhile)
// cannot leave one missing; those no longer wanted are deleted.
func keep(d Deps, k live.Keep) ([]string, error) {
	if d.Projections == nil {
		return nil, nil
	}
	entity, _, _ := strings.Cut(k.Key, ".")
	return keepFor(k.Ctx, stateOf(d.Projections), k.Tenant, entity, k.Value, k.Kept)
}

func keepFor(ctx context.Context, s *state, tenant uint64, entity string, value json.RawMessage, kept []string) ([]string, error) {
	var want []string
	if value != nil {
		e, ok, err := s.published(ctx, tenant, entity)
		if err != nil {
			return nil, err
		}
		var r Record
		if err := json.Unmarshal(value, &r); err != nil {
			return nil, err
		}
		if ok {
			want = IndexKeys(e, r)
		}
	}
	if len(want) == 0 && len(kept) == 0 {
		return nil, nil
	}
	kv, err := s.p.Side(ctx, tenant, IndexBucket)
	if err != nil {
		return nil, err
	}
	for _, key := range want {
		if _, err := kv.Put(ctx, key, []byte("1")); err != nil {
			return nil, err
		}
	}
	for _, key := range kept {
		if !slices.Contains(want, key) {
			if err := kv.Delete(ctx, key); err != nil {
				return nil, err
			}
		}
	}
	return want, nil
}

// tree is one index in memory: its entries without the prefix, in order.
type tree struct {
	mu     sync.RWMutex
	bt     *btree.BTreeG[string]
	ready  chan struct{}
	err    error
	cancel context.CancelFunc
}

// index is an index's tree, loaded on first use and waited for.
func (s *state) index(ctx context.Context, tenant uint64, entity, index string) (*tree, error) {
	k := fmt.Sprintf("%d/%s/%s", tenant, entity, index)
	s.mu.Lock()
	t := s.trees[k]
	if t == nil {
		t = &tree{bt: btree.NewG(32, func(a, b string) bool { return a < b }), ready: make(chan struct{})}
		s.trees[k] = t
		go t.load(s, tenant, IndexPrefix(entity, index))
	}
	s.mu.Unlock()
	select {
	case <-t.ready:
		if t.err != nil {
			s.drop(tenant, entity, index)
		}
		return t, t.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (t *tree) load(s *state, tenant uint64, prefix string) {
	ctx, cancel := context.WithCancel(s.p.Context())
	t.cancel = cancel
	fail := func(err error) {
		t.err = err
		close(t.ready)
		cancel()
	}
	kv, err := s.p.Side(ctx, tenant, IndexBucket)
	if err != nil {
		fail(err)
		return
	}
	// One watch, so a key's put and delete arrive in order; a deleted key's
	// marker deletes nothing in a tree that never had it
	w, err := kv.Watch(ctx, prefix+">", jetstream.MetaOnly())
	if err != nil {
		fail(err)
		return
	}
	defer func() { _ = w.Stop() }()
	started := false
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-w.Updates():
			if !ok {
				return
			}
			if e == nil {
				if !started {
					started = true
					close(t.ready)
				}
				continue
			}
			key := strings.TrimPrefix(e.Key(), prefix)
			t.mu.Lock()
			if e.Operation() == jetstream.KeyValuePut {
				t.bt.ReplaceOrInsert(key)
			} else {
				t.bt.Delete(key)
			}
			t.mu.Unlock()
		}
	}
}

// drop forgets an index's tree.
func (s *state) drop(tenant uint64, entity, index string) {
	k := fmt.Sprintf("%d/%s/%s", tenant, entity, index)
	s.mu.Lock()
	t := s.trees[k]
	delete(s.trees, k)
	s.mu.Unlock()
	if t != nil && t.cancel != nil {
		t.cancel()
	}
}

// walk calls fn on the entries from from (inclusive) to to (exclusive;
// "" for no end), backwards when desc, until fn returns false. Entries are
// copied out a page at a time, so fn runs without the tree's lock.
func (t *tree) walk(from, to string, desc bool, fn func(entry string) bool) {
	const pageSize = 256
	lo, hi := from, to // what is left is [lo, hi)
	for {
		page := make([]string, 0, pageSize)
		collect := func(e string) bool {
			switch {
			case !desc && hi != "" && e >= hi, desc && e < lo:
				return false
			case desc && hi != "" && e >= hi:
				return true
			}
			page = append(page, e)
			return len(page) < pageSize
		}
		t.mu.RLock()
		switch {
		case !desc:
			t.bt.AscendGreaterOrEqual(lo, collect)
		case hi == "":
			t.bt.Descend(collect)
		default:
			t.bt.DescendLessOrEqual(hi, collect)
		}
		t.mu.RUnlock()
		for _, e := range page {
			if !fn(e) {
				return
			}
		}
		if len(page) < pageSize {
			return
		}
		if desc {
			hi = page[len(page)-1]
		} else {
			lo = page[len(page)-1] + "\x00"
		}
	}
}

// followIndexes starts the indexer: a follower of the def stream's
// published events, like the converter, that keeps the projector's entity
// types current and rebuilds the indexes a version changes.
func followIndexes(args features.ServiceArgs, deps Deps) error {
	tenant := args.Principal.TenantID
	subject := live.Target{Area: live.AreaDefinitions, Type: entities.TargetType, ID: "*"}.Subject(tenant, entities.EventPublished)
	return deps.Projections.Follow(args.Context(), tenant, live.AreaDefinitions, Indexer, []string{subject}, func(e live.Event) error {
		return reindex(deps, e)
	})
}

// reindex brings every record of a newly published entity type to the
// version's indexes, when they changed. Rekeep writes each record's
// entries and replaces its kept keys unless the record changed meanwhile,
// so it is safe beside the projector and idempotent when redelivered.
func reindex(deps Deps, e live.Event) error {
	var p entities.Publication
	if err := json.Unmarshal(e.Data, &p); err != nil {
		return fmt.Errorf("cannot read the publication: %w", err)
	}
	s := stateOf(deps.Projections)
	s.setPublished(e.TenantID, p.Entity)
	if len(p.IndexChanges) == 0 {
		return nil
	}
	for _, id := range p.IndexChanges {
		if !slices.ContainsFunc(p.Entity.Indexes, func(x records.Index) bool { return x.ID == id }) {
			s.drop(e.TenantID, p.Entity.ID, id)
		}
	}
	// A record folded while the version was being set may have kept its
	// entries for the one before; once the projector has settled, a second
	// pass takes the records that changed since the first
	seen := map[string]uint64{}
	for pass := 0; pass < 2; pass++ {
		if pass == 1 {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			err := deps.Projections.Settle(ctx, e.TenantID, Projector)
			cancel()
			if err != nil {
				return err
			}
		}
		keys, err := recordKeys(deps.Projections, e.TenantID, p.Entity.ID)
		if err != nil {
			return err
		}
		for key, seq := range keys {
			if pass == 1 && seen[key] == seq {
				continue
			}
			seen[key] = seq
			err := deps.Projections.Rekeep(context.Background(), e.TenantID, Projector, key, func(value json.RawMessage, kept []string) ([]string, error) {
				return keepFor(context.Background(), s, e.TenantID, p.Entity.ID, value, kept)
			})
			if err != nil {
				return fmt.Errorf("cannot reindex %s: %w", key, err)
			}
		}
	}
	return nil
}

// recordKeys are an entity type's records' keys, each with the sequence
// its value is at.
func recordKeys(p Projections, tenant uint64, entity string) (map[string]uint64, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entries, err := p.Watch(ctx, tenant, Projector, entity+".*")
	if err != nil {
		return nil, err
	}
	out := map[string]uint64{}
	for en := range entries {
		if en == nil {
			break
		}
		if !en.Deleted {
			out[en.Key] = en.Value.Seq
		}
	}
	return out, nil
}
