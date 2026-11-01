package data_test

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records/data"
)

// findSink keeps a records.find subscription's result as a browser does,
// and checks that every change applies where it says.
type findSink struct {
	mu      sync.Mutex
	data    any
	pos     live.Position
	deltas  []live.Delta
	shownAt map[string]live.Position // record id → the position it was first shown at
	bad     string
}

func (s *findSink) Snapshot(d any, pos live.Position) {
	raw, _ := json.Marshal(d)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = nil
	_ = json.Unmarshal(raw, &s.data)
	s.pos = pos
	for _, r := range s.list() {
		s.shownAt[idOf(r)] = pos
	}
}

func (s *findSink) list() []any {
	m, _ := s.data.(map[string]any)
	l, _ := m[data.ListName].([]any)
	return l
}

func idOf(r any) string {
	m, _ := r.(map[string]any)
	id, _ := m[data.MetaID].(string)
	return id
}

func (s *findSink) Change(d live.Delta, pos live.Position) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pos = s.pos.Merge(pos)
	s.deltas = append(s.deltas, d)
	if d.List != "" {
		l := s.list()
		switch {
		case d.From != nil && (*d.From >= len(l) || idOf(l[*d.From]) != d.Key):
			s.bad = fmt.Sprintf("%s %s from %d does not name it in %d rows", d.Op, d.Key, *d.From, len(l))
		case d.Op == "insert" && (*d.Index > len(l) || slices.ContainsFunc(l, func(r any) bool { return idOf(r) == d.Key })):
			s.bad = fmt.Sprintf("insert %s at %d of %d rows", d.Key, *d.Index, len(l))
		}
		if d.Op == "insert" {
			if _, ok := s.shownAt[d.Key]; !ok {
				s.shownAt[d.Key] = s.pos
			}
		}
	}
	s.data = live.Apply(s.data, d)
}

func (s *findSink) Failed(resp features.ServiceResponse) {
	s.mu.Lock()
	s.bad = "failed: " + resp.Message
	s.mu.Unlock()
}

// view is the sink's records' ids and next.
func (s *findSink) view() (ids []string, next string, bad string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.list() {
		ids = append(ids, idOf(r))
	}
	m, _ := s.data.(map[string]any)
	next, _ = m["next"].(string)
	return ids, next, s.bad
}

func (f *fixture) find(q data.Query) *findSink {
	f.t.Helper()
	s := &findSink{shownAt: map[string]live.Position{}}
	raw, _ := json.Marshal(q)
	if fail := f.subs.Open(subscribe.OpenArgs{ServiceArgs: args(), Query: "records.find", Params: raw, Sink: s}); fail != nil {
		f.t.Fatalf("records.find %s: %s", raw, fail.Message)
	}
	return s
}

// matches waits until the subscription equals a fresh read of its query:
// the same records in the same order, each with the same values, and the
// same next.
func (f *fixture) matches(t *testing.T, s *findSink, q data.Query) {
	t.Helper()
	eventually(t, func() string {
		res, err := data.Find(args(), data.Deps{Projections: f.p}, q)
		if err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, r := range res.Records {
			var id string
			_ = json.Unmarshal(r[data.MetaID], &id)
			want = append(want, id)
		}
		got, next, bad := s.view()
		if bad != "" {
			t.Fatalf("%+v: %s", q, bad)
		}
		if !slices.Equal(got, want) || (next == "") != (res.Next == "") {
			return fmt.Sprintf("%s sort %v limit %d: live %v next %t, fresh %v next %t", jsonOf(q.Filter), q.Sort, q.Limit, got, next != "", want, res.Next != "")
		}
		s.mu.Lock()
		raw, _ := json.Marshal(s.list())
		s.mu.Unlock()
		fresh, _ := json.Marshal(res.Records)
		var a, b any
		_ = json.Unmarshal(raw, &a)
		_ = json.Unmarshal(fresh, &b)
		if fmt.Sprint(a) != fmt.Sprint(b) {
			return fmt.Sprintf("%+v: values differ:\n live %s\nfresh %s", q, raw, fresh)
		}
		return ""
	})
}

func jsonOf(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

var seed uint64 = 3

// TestLiveQueriesEndAsAFreshRead is the acceptance test: live queries fed
// randomized concurrent commands from several connections end equal to a
// fresh read of the same query.
func TestLiveQueriesEndAsAFreshRead(t *testing.T) {
	f := setup(t)
	f.define("item", itemIndexes)
	f.define("plain", nil)
	r := rand.New(rand.NewPCG(seed, 5))
	var ids []string
	for range 80 {
		v := randomValues(r)
		res := f.ok("record.create", "", map[string]any{"entity": "item", "values": v})
		f.ok("record.create", res.Target, map[string]any{"entity": "plain", "values": v})
		ids = append(ids, res.Target)
	}
	f.settle()

	type sub struct {
		q data.Query
		s *findSink
	}
	var subs []sub
	for i := range 60 {
		q := data.Query{Entity: []string{"item", "plain"}[i%2], Sort: sorts[r.IntN(len(sorts))], Limit: []int{0, 4, 9, 9, 20}[r.IntN(5)]}
		if i%5 != 0 {
			raw, _ := json.Marshal(randomFilter(r, 0))
			q.Filter = &data.Filter{}
			_ = json.Unmarshal(raw, q.Filter)
		}
		if i%3 == 0 { // a later page
			first, err := data.Find(args(), data.Deps{Projections: f.p}, data.Query{Entity: q.Entity, Filter: q.Filter, Sort: q.Sort, Limit: 5})
			if err != nil {
				t.Fatal(err)
			}
			q.Cursor = first.Next
		}
		if i%7 == 0 {
			q.Fields = []string{"n", "title"}
		}
		subs = append(subs, sub{q, f.find(q)})
	}

	// Three connections, each with its own bus, change the records at once
	var wg sync.WaitGroup
	var mu sync.Mutex
	for c := range 3 {
		b := bus.New(f.p)
		cr := rand.New(rand.NewPCG(seed+uint64(c), 9))
		wg.Go(func() {
			for range 120 {
				entity := []string{"item", "plain"}[cr.IntN(2)]
				switch k := cr.IntN(10); {
				case k < 3:
					res := f.on(b, "record.create", "", map[string]any{"entity": entity, "values": randomValues(cr)})
					mu.Lock()
					ids = append(ids, res.Target)
					mu.Unlock()
				case k < 4:
					mu.Lock()
					id := ids[cr.IntN(len(ids))]
					mu.Unlock()
					f.on(b, "record.delete", id, map[string]any{"entity": entity})
				default:
					mu.Lock()
					id := ids[cr.IntN(len(ids))]
					mu.Unlock()
					set := randomValues(cr)
					for k := range set {
						if cr.IntN(3) > 0 {
							delete(set, k)
						}
					}
					if cr.IntN(4) == 0 {
						set["n"] = nil
					}
					f.on(b, "record.update", id, map[string]any{"entity": entity, "set": set})
				}
			}
		})
	}
	wg.Wait()
	// A subscription follows its own type's records: one last create each
	last := map[string]uint64{}
	for _, entity := range []string{"item", "plain"} {
		last[entity] = f.ok("record.create", "", map[string]any{"entity": entity, "values": map[string]any{"n": 1}}).Seq
	}
	f.settle()
	moves, windows := 0, 0
	for _, s := range subs {
		f.matches(t, s.s, s.q)
		eventually(t, func() string {
			s.s.mu.Lock()
			defer s.s.mu.Unlock()
			if at := s.s.pos[live.StreamName(features.LocalTenantID, live.AreaRecords)]; at < last[s.q.Entity] {
				return fmt.Sprintf("%+v reached %d of %d", s.q, at, last[s.q.Entity])
			}
			return ""
		})
		for _, d := range s.s.deltas {
			if d.Op == "move" {
				moves++
			}
		}
		s.s.mu.Lock()
		plan, _ := s.s.data.(map[string]any)["plan"].(map[string]any)
		if plan["ordered"] == true && s.q.Limit > 0 {
			windows++
		}
		s.s.mu.Unlock()
	}
	if moves == 0 || windows < 5 {
		t.Errorf("%d moves, and %d ordered windows", moves, windows)
	}
}

// TestALiveQuerysChanges checks each kind of change on its own.
func TestALiveQuerysChanges(t *testing.T) {
	f := setup(t)
	f.define("item", [][]string{{"done", "n"}, {"n"}})
	id := map[int]string{}
	for n := 1; n <= 6; n++ {
		id[n] = f.ok("record.create", "", map[string]any{"entity": "item", "values": map[string]any{"n": n, "done": false}}).Target
	}
	f.settle()
	open := data.Query{Entity: "item", Where: map[string]json.RawMessage{"done": json.RawMessage(`false`)}, Sort: []data.Sort{{Field: "n"}}, Limit: 3}
	s := f.find(open)
	f.matches(t, s, open)
	step := func(n int, set map[string]any, op, key string) {
		t.Helper()
		before := len(s.deltas)
		f.ok("record.update", id[n], map[string]any{"entity": "item", "set": set})
		f.settle()
		f.matches(t, s, open)
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, d := range s.deltas[before:] {
			if d.Op == op && d.Key == key {
				return
			}
		}
		t.Errorf("updating %d by %v sent %+v, not %s %s", n, set, s.deltas[before:], op, key)
	}
	// Stops matching: removed, and the next one enters at the end
	step(2, map[string]any{"done": true}, "remove", id[2])
	if !slices.ContainsFunc(s.deltas, func(d live.Delta) bool { return d.Op == "insert" && d.Key == id[4] && *d.Index == 2 }) {
		t.Errorf("4 did not enter at 2: %+v", s.deltas)
	}
	// Starts matching again: inserted
	step(2, map[string]any{"done": false}, "insert", id[2])
	// The sorted field changes: a move
	step(3, map[string]any{"n": 0.5}, "move", id[3])
	// A value alone: an update
	step(1, map[string]any{"title": "first"}, "update", id[1])
	// Out past the window: removed, and the next enters
	step(1, map[string]any{"n": 10}, "remove", id[1])

	// A later page keeps its window when a record enters above it
	first, _ := data.Find(args(), data.Deps{Projections: f.p}, data.Query{Entity: "item", Sort: []data.Sort{{Field: "n"}}, Limit: 2})
	later := data.Query{Entity: "item", Sort: []data.Sort{{Field: "n"}}, Limit: 2, Cursor: first.Next}
	p := f.find(later)
	f.matches(t, p, later)
	before, _, _ := p.view()
	f.ok("record.create", "", map[string]any{"entity": "item", "values": map[string]any{"n": -5}})
	f.settle()
	f.matches(t, p, later)
	after, _, _ := p.view()
	if !slices.Equal(before, after) {
		t.Errorf("the window moved from %v to %v", before, after)
	}
	p.mu.Lock()
	for _, d := range p.deltas {
		if d.List != "" {
			t.Errorf("a record above the window sent %+v", d)
		}
	}
	p.mu.Unlock()
}

// TestALiveQueryReportsItsPosition: a subscription reaches a command's
// sequence before it shows the change, and also when the change is not
// one it shows.
func TestALiveQueryReportsItsPosition(t *testing.T) {
	f := setup(t)
	f.define("item", [][]string{{"n"}})
	for n := range 3 {
		f.ok("record.create", "", map[string]any{"entity": "item", "values": map[string]any{"n": n}})
	}
	hidden := f.ok("record.create", "", map[string]any{"entity": "item", "values": map[string]any{"n": 50}})
	f.settle()
	q := data.Query{Entity: "item", Filter: &data.Filter{Field: "n", Op: "lt", Value: json.RawMessage(`10`)}, Sort: []data.Sort{{Field: "n"}}, Limit: 5}
	s := f.find(q)
	f.matches(t, s, q)
	stream := live.StreamName(features.LocalTenantID, live.AreaRecords)
	// The page reflects every write before it, shown or not
	s.mu.Lock()
	if s.pos[stream] < hidden.Seq {
		t.Errorf("the page is at %d, after a write at %d", s.pos[stream], hidden.Seq)
	}
	s.mu.Unlock()
	for i := range 20 {
		n := i
		if i%2 == 1 {
			n = 100 + i // never shown
		}
		ack := f.ok("record.create", "", map[string]any{"entity": "item", "values": map[string]any{"n": n}})
		eventually(t, func() string {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.pos[stream] < ack.Seq {
				return fmt.Sprintf("record %d at %d: the subscription reached %d", n, ack.Seq, s.pos[stream])
			}
			return ""
		})
		s.mu.Lock()
		if at, shown := s.shownAt[ack.Target]; shown && at[stream] < ack.Seq {
			t.Errorf("record %d at %d was shown at %d", n, ack.Seq, at[stream])
		}
		s.mu.Unlock()
	}
	f.matches(t, s, q)
}
