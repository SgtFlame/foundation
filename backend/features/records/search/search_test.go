package search_test

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/records/search"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
)

const dims = 16

// embed is a stand-in embedder: a unit vector of the text's hashed words.
func embed(_ features.ServiceArgs, text, _ string, _ bool) ([]float32, error) {
	v := make([]float32, dims)
	for _, w := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(w))
		v[h.Sum32()%dims]++
	}
	var n float64
	for _, x := range v {
		n += float64(x * x)
	}
	if n == 0 {
		v[0], n = 1, 1
	}
	for i := range v {
		v[i] /= float32(math.Sqrt(n))
	}
	return v, nil
}

type fixture struct {
	t     *testing.T
	bus   *bus.Bus
	subs  *subscribe.Subscriptions
	index *search.Index
	deps  data.Deps
	reg   *extensions.Registry
	n     atomic.Int64
}

func setup(t *testing.T) *fixture {
	reg := extensions.NewRegistry()
	f := &fixture{t: t, reg: reg}
	var entityDeps entities.Deps
	entities.Contribute(reg, func() entities.Deps { return entityDeps })
	data.Contribute(reg, func() data.Deps { return f.deps })
	search.Contribute(reg, func() *search.Index { return f.index })
	srv := natsd.StartTest(t)
	p := project.New(srv.JS(), reg)
	t.Cleanup(p.Close)
	f.bus, f.subs = bus.New(p), subscribe.New(p, reg)
	f.index = search.New(t.TempDir(), dims, embed, func() data.Deps { return f.deps })
	t.Cleanup(f.index.Close)
	f.deps = data.Deps{Projections: p, Bus: f.bus, Search: f.index}
	entityDeps = entities.Deps{Projections: p, OnPublish: func(args features.ServiceArgs) error { return data.Follow(args, f.deps) }}
	return f
}

func as(user uint64) features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, UserID: user, TenantID: features.LocalTenantID}}
}

func (f *fixture) ok(typ, target string, a any) bus.Result {
	f.t.Helper()
	raw, _ := json.Marshal(a)
	res := f.bus.Execute(bus.Request{ServiceArgs: as(1), ID: fmt.Sprintf("c%d", f.n.Add(1)), Type: typ, Target: target, Args: raw})
	if res.Common.Code >= 300 {
		f.t.Fatalf("%s: %s %+v", typ, res.Common.Message, res.Problems)
	}
	return res
}

// found is the ids a search finds for user.
func (f *fixture) found(user uint64, q data.SearchQuery) []string {
	f.t.Helper()
	res, err := data.SearchRecords(as(user), f.deps, q)
	if err != nil {
		f.t.Fatal(err)
	}
	var ids []string
	for _, r := range res.Records {
		var id string
		_ = json.Unmarshal(r[data.MetaID], &id)
		ids = append(ids, id)
	}
	return ids
}

// eventually waits until cond holds.
func (f *fixture) eventually(what string, cond func() bool) {
	f.t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !cond(); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			f.t.Fatalf("%s did not happen", what)
		}
	}
}

func doc(text string) map[string]any {
	return map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}}}}}
}

func TestSearchFollowsTheRecords(t *testing.T) {
	f := setup(t)
	f.ok("entity.create", "page", map[string]any{"name": "Page", "fields": []any{
		map[string]any{"name": "title", "type": "text", "searchable": true},
		map[string]any{"name": "body", "type": "richText", "searchable": true},
		map[string]any{"name": "note", "type": "text"},
		map[string]any{"name": "owner", "type": "relation", "relation": map[string]any{"entity": "user"}},
	}})
	f.ok("entity.setAccess", "page", map[string]any{"access": map[string]any{"read": "record.owner == principal.user"}})
	f.ok("entity.publish", "page", map[string]any{"rev": 2})
	f.eventually("publishing", func() bool { _, problems := data.Entity(as(1), f.deps.Projections, "page"); return problems == nil })

	zebra := f.ok("record.create", "", map[string]any{"entity": "page", "values": map[string]any{"title": "Zebra crossing", "body": doc("black and white stripes"), "note": "giraffe", "owner": "1"}}).Target
	other := f.ok("record.create", "", map[string]any{"entity": "page", "values": map[string]any{"title": "Zebra of user 2", "owner": "2"}}).Target
	f.ok("record.create", "", map[string]any{"entity": "page", "values": map[string]any{"title": "Lion", "owner": "1"}})

	// A word in a searchable field finds the record, rich text included;
	// a field not marked searchable is not searched
	f.eventually("finding zebra", func() bool { return slices.Equal(f.found(1, data.SearchQuery{Entity: "page", Text: "zebra"}), []string{zebra}) })
	if got := f.found(1, data.SearchQuery{Text: "stripes"}); !slices.Equal(got, []string{zebra}) {
		t.Errorf("stripes found %v", got)
	}
	if got := f.found(1, data.SearchQuery{Text: "giraffe"}); len(got) != 0 {
		t.Errorf("an unsearchable field was searched: %v", got)
	}
	// The row rule hides user 2's page from user 1, and user 1's from 2
	if got := f.found(2, data.SearchQuery{Text: "zebra"}); !slices.Equal(got, []string{other}) {
		t.Errorf("user 2 found %v", got)
	}
	// Toward meaning, records without the word may join it
	if got := f.found(1, data.SearchQuery{Text: "zebra", LexicalSemantic: 5}); !slices.Contains(got, zebra) {
		t.Errorf("a semantic search found %v", got)
	}

	// A live search sees the record leave when the word does
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sink := &listSink{}
	a := as(1)
	a.Ctx = ctx
	if fail := f.subs.Open(subscribe.OpenArgs{ServiceArgs: a, Query: "records.search", Params: json.RawMessage(`{"entity":"page","text":"zebra"}`), Sink: sink}); fail != nil {
		t.Fatal(fail.Message)
	}
	f.eventually("the live search's snapshot", func() bool { return slices.Equal(sink.ids(), []string{"page." + zebra}) })

	f.ok("record.update", zebra, map[string]any{"entity": "page", "set": map[string]any{"title": "Pedestrian crossing"}})
	f.eventually("losing zebra", func() bool { return len(f.found(1, data.SearchQuery{Entity: "page", Text: "zebra"})) == 0 })
	f.eventually("the live search's remove", func() bool { return len(sink.ids()) == 0 })
	if got := f.found(1, data.SearchQuery{Text: "pedestrian"}); !slices.Equal(got, []string{zebra}) {
		t.Errorf("the new word found %v", got)
	}
	// And joins it again when a record gains it
	lion := f.ok("record.create", "", map[string]any{"entity": "page", "values": map[string]any{"title": "Zebra-striped lion", "owner": "1"}}).Target
	f.eventually("the live search's insert", func() bool { return slices.Equal(sink.ids(), []string{"page." + lion}) })

	// A deleted record is not found
	f.ok("record.delete", lion, map[string]any{"entity": "page"})
	f.eventually("forgetting a deleted record", func() bool { return len(f.found(1, data.SearchQuery{Text: "lion"})) == 1 })

	// Records are a retrieval source chat searches only when switched on
	var src retrieval.Source
	for _, s := range retrieval.Sources.All(f.reg) {
		if s.ID == search.SourceID {
			src = s
		}
	}
	if !src.Optional {
		t.Fatalf("the records source is not optional: %+v", src)
	}
	segs, err := src.KeywordSearch(as(1).CallContext(), "pedestrian", 5, retrieval.Filter{})
	if err != nil || len(segs) != 1 || segs[0].Location.RecordID != zebra || !strings.Contains(segs[0].Title, "Pedestrian crossing") {
		t.Errorf("the source found %+v (%v)", segs, err)
	}
	if segs, _ := src.KeywordSearch(as(2).CallContext(), "pedestrian", 5, retrieval.Filter{}); len(segs) != 0 {
		t.Errorf("the source showed user 2 a hidden record: %+v", segs)
	}
}

// listSink keeps a live list's keys in order.
type listSink struct {
	mu   sync.Mutex
	keys []string
}

func (s *listSink) ids() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.keys)
}

func (s *listSink) Snapshot(d any, _ live.Position) {
	raw, _ := json.Marshal(d)
	var res data.SearchResult
	_ = json.Unmarshal(raw, &res)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = nil
	for _, r := range res.Records {
		var id, entity string
		_ = json.Unmarshal(r[data.MetaID], &id)
		_ = json.Unmarshal(r[data.MetaEntity], &entity)
		s.keys = append(s.keys, entity+"."+id)
	}
}

func (s *listSink) Change(d live.Delta, _ live.Position) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch d.Op {
	case "insert":
		s.keys = slices.Insert(s.keys, *d.Index, d.Key)
	case "remove":
		s.keys = slices.DeleteFunc(s.keys, func(k string) bool { return k == d.Key })
	case "move":
		s.keys = slices.DeleteFunc(s.keys, func(k string) bool { return k == d.Key })
		s.keys = slices.Insert(s.keys, *d.Index, d.Key)
	}
}

func (s *listSink) Failed(resp features.ServiceResponse) { panic(resp.Message) }
