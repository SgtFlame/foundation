package data_test

import (
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	recordssettings "github.com/SgtFlame/foundation/backend/features/records/settings"
)

// people defines person and job: a job has an owner (a person), helpers
// (people) and an assignee (a user).
func (f *fixture) people() {
	f.t.Helper()
	f.ok("entity.create", "person", map[string]any{"name": "Person", "fields": []any{map[string]any{"name": "name", "type": "text"}}})
	f.publish("person", 1)
	f.ok("entity.create", "job", map[string]any{"name": "Job", "fields": []any{
		map[string]any{"name": "title", "type": "text"},
		map[string]any{"name": "owner", "type": "relation", "relation": map[string]any{"entity": "person"}},
		map[string]any{"name": "helpers", "type": "relation", "relation": map[string]any{"entity": "person", "many": true}},
		map[string]any{"name": "assignee", "type": "relation", "relation": map[string]any{"entity": "user"}},
	}})
	f.publish("job", 1)
	f.deps.Users = func(tenant uint64, ids []uint64) map[uint64]data.User {
		out := map[uint64]data.User{}
		for _, id := range ids {
			if id == 1 && tenant == features.LocalTenantID {
				out[id] = data.User{Username: "ada", DisplayName: "Ada"}
			}
		}
		return out
	}
}

// expanded is a record's _expanded.<field>, decoded.
func expanded(r any, field string) any {
	m, _ := r.(map[string]any)
	e, _ := m[data.MetaExpanded].(map[string]any)
	return e[field]
}

func nameOf(v any) string {
	m, _ := v.(map[string]any)
	s, _ := m["name"].(string)
	return s
}

func TestExpansionsFollowChanges(t *testing.T) {
	f := setup(t)
	f.people()
	ann := f.ok("record.create", "", map[string]any{"entity": "person", "values": map[string]any{"name": "Ann"}}).Target
	bob := f.ok("record.create", "", map[string]any{"entity": "person", "values": map[string]any{"name": "Bob"}}).Target
	j1 := f.ok("record.create", "", map[string]any{"entity": "job", "values": map[string]any{"title": "one", "owner": ann, "helpers": []string{bob, ann}, "assignee": "1"}}).Target
	f.ok("record.create", "", map[string]any{"entity": "job", "values": map[string]any{"title": "two", "owner": bob, "assignee": "9"}})
	f.settle()

	q := data.Query{Entity: "job", Sort: []data.Sort{{Field: "title"}}, Expand: []string{"owner", "helpers", "assignee"}}
	res, err := data.Find(args(), *f.deps, q)
	if err != nil {
		t.Fatal(err)
	}
	var recs []any
	raw, _ := json.Marshal(res.Records)
	_ = json.Unmarshal(raw, &recs)
	if len(recs) != 2 || nameOf(expanded(recs[0], "owner")) != "Ann" || nameOf(expanded(recs[1], "owner")) != "Bob" {
		t.Fatalf("owners: %s", raw)
	}
	// A to-many relation gives every related record, in the field's order
	helpers, _ := expanded(recs[0], "helpers").([]any)
	if len(helpers) != 2 || nameOf(helpers[0]) != "Bob" || nameOf(helpers[1]) != "Ann" {
		t.Fatalf("helpers: %v", expanded(recs[0], "helpers"))
	}
	if h, _ := expanded(recs[1], "helpers").([]any); h == nil || len(h) != 0 {
		t.Fatalf("no helpers is an empty list: %v", expanded(recs[1], "helpers"))
	}
	// A user is read from identity; one not in the tenant is not there
	if u, _ := expanded(recs[0], "assignee").(map[string]any); u["displayName"] != "Ada" || u[data.MetaID] != "1" {
		t.Fatalf("assignee: %v", expanded(recs[0], "assignee"))
	}
	if expanded(recs[1], "assignee") != nil {
		t.Fatalf("an unknown user expands to null: %v", expanded(recs[1], "assignee"))
	}
	// The records themselves are not changed
	if r := f.record(j1, 0); r[data.MetaExpanded] != nil {
		t.Fatalf("the projection holds an expansion: %v", r)
	}

	// A page returning only some fields still expands relations it leaves out
	only, err := data.Find(args(), *f.deps, data.Query{Entity: "job", Sort: q.Sort, Fields: []string{"title"}, Expand: []string{"owner"}})
	if err != nil {
		t.Fatal(err)
	}
	var e map[string]any
	_ = json.Unmarshal(only.Records[0][data.MetaExpanded], &e)
	if _, has := only.Records[0]["owner"]; has || nameOf(e["owner"]) != "Ann" {
		t.Fatalf("with fields: %v", only.Records[0])
	}

	// A live page runs again when a related record changes
	s := f.find(q)
	f.ok("record.update", ann, map[string]any{"entity": "person", "set": map[string]any{"name": "Anne"}})
	eventually(t, func() string {
		s.mu.Lock()
		defer s.mu.Unlock()
		if l := s.list(); len(l) != 2 || nameOf(expanded(l[0], "owner")) != "Anne" {
			return fmt.Sprintf("the rename did not reach the page: %v", l)
		}
		return ""
	})
	f.ok("record.delete", bob, map[string]any{"entity": "person"})
	f.ok("record.create", "", map[string]any{"entity": "job", "values": map[string]any{"title": "a first", "owner": ann}})
	f.matches(t, s, q)
	s.mu.Lock()
	l := s.list()
	s.mu.Unlock()
	if h, _ := expanded(l[1], "helpers").([]any); len(h) != 1 || expanded(l[2], "owner") != nil {
		t.Fatalf("a deleted person stays expanded: %v", l)
	}
	if _, _, bad := s.view(); bad != "" {
		t.Fatal(bad)
	}

	// Bad expansions are refused
	for _, field := range []string{"title", "nope"} {
		if _, err := data.Find(args(), *f.deps, data.Query{Entity: "job", Expand: []string{field}}); err == nil || !strings.Contains(err.Error(), "no relation field") {
			t.Errorf("expanding %s: %v", field, err)
		}
	}
}

// aggSink keeps a records.aggregate subscription's result.
type aggSink struct {
	mu     sync.Mutex
	data   any
	deltas []live.Delta
	bad    string
}

func (s *aggSink) Snapshot(d any, _ live.Position) {
	raw, _ := json.Marshal(d)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = nil
	_ = json.Unmarshal(raw, &s.data)
}

func (s *aggSink) Change(d live.Delta, _ live.Position) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deltas = append(s.deltas, d)
	s.data = live.Apply(s.data, d)
}

func (s *aggSink) Failed(resp features.ServiceResponse) {
	s.mu.Lock()
	s.bad = "failed: " + resp.Message
	s.mu.Unlock()
}

func (s *aggSink) groups() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _ := s.data.(map[string]any)
	return canon(m[data.GroupsName])
}

func canon(v any) string {
	raw, _ := json.Marshal(v)
	var x any
	_ = json.Unmarshal(raw, &x)
	out, _ := json.Marshal(x)
	return string(out)
}

func (f *fixture) aggregate(a features.ServiceArgs, q data.Aggregation) *aggSink {
	f.t.Helper()
	s := &aggSink{}
	raw, _ := json.Marshal(q)
	if fail := f.subs.Open(subscribe.OpenArgs{ServiceArgs: a, Query: "records.aggregate", Params: raw, Sink: s}); fail != nil {
		f.t.Fatalf("records.aggregate %s: %s", raw, fail.Message)
	}
	return s
}

// expect computes an aggregation from every record a scan reads, apart
// from the planner: count, sum and avg with exact rationals, min and max
// by comparing the values themselves.
func (f *fixture) expect(t *testing.T, q data.Aggregation) string {
	t.Helper()
	res, err := data.Find(args(), *f.deps, data.Query{Entity: "plain", Where: q.Where, Filter: q.Filter})
	if err != nil {
		t.Fatal(err)
	}
	type group struct {
		key  any
		recs []data.Record
	}
	groups := map[string]*group{}
	put := func(key any, r data.Record) {
		k := canon(key)
		if groups[k] == nil {
			groups[k] = &group{key: key}
		}
		groups[k].recs = append(groups[k].recs, r)
	}
	if q.GroupBy == "" {
		groups["null"] = &group{}
	}
	for _, r := range res.Records {
		var v any
		_ = json.Unmarshal(r[q.GroupBy], &v)
		switch items := v.(type) {
		case []any:
			for _, it := range items {
				put(it, r)
			}
		default:
			put(v, r)
		}
	}
	var out []map[string]any
	for _, g := range groups {
		values := map[string]any{}
		for _, a := range q.Aggregates {
			var nums []*big.Rat
			var vals []any
			for _, r := range g.recs {
				var v any
				if json.Unmarshal(r[a.Field], &v) != nil || v == nil {
					continue
				}
				vals = append(vals, v)
				switch x := v.(type) {
				case float64:
					nums = append(nums, new(big.Rat).SetFloat64(x))
				case string:
					if q, ok := new(big.Rat).SetString(x); ok {
						nums = append(nums, q)
					}
				}
			}
			sum := new(big.Rat)
			for _, n := range nums {
				sum.Add(sum, n)
			}
			name := a.As
			switch a.Op {
			case "count":
				if a.Field == "" {
					values[name] = len(g.recs)
				} else {
					values[name] = len(vals)
				}
			case "sum":
				if a.Field == "n" {
					s, _ := sum.Float64()
					values[name] = s
				} else {
					values[name] = sum.FloatString(3)
				}
			case "avg":
				switch {
				case len(nums) == 0:
					values[name] = nil
				case a.Field == "n":
					s, _ := sum.Float64()
					values[name] = s / float64(len(nums))
				default:
					values[name] = new(big.Rat).Quo(sum, big.NewRat(int64(len(nums)), 1)).FloatString(5)
				}
			case "min", "max":
				sort.Slice(vals, func(i, j int) bool { return fmt.Sprint(vals[i]) < fmt.Sprint(vals[j]) }) // dates sort as text
				switch {
				case len(vals) == 0:
					values[name] = nil
				case a.Op == "min":
					values[name] = vals[0]
				default:
					values[name] = vals[len(vals)-1]
				}
			}
		}
		out = append(out, map[string]any{"key": g.key, "values": values})
	}
	// The order of groups is the field's; done and due sort as their text
	slices.SortFunc(out, func(a, b map[string]any) int {
		if a["key"] == nil || b["key"] == nil {
			return map[bool]int{true: -1, false: 1}[a["key"] == nil]
		}
		return strings.Compare(fmt.Sprint(a["key"]), fmt.Sprint(b["key"]))
	})
	return canon(out)
}

// aggregations are the property test's: count by a bool, a list and
// nothing; sum and avg over a number and a decimal; min and max of dates;
// some filtered.
var aggregations = []data.Aggregation{
	{GroupBy: "done", Aggregates: []data.Aggregate{{Op: "count", As: "count"}}},
	{GroupBy: "done", Aggregates: []data.Aggregate{{Op: "sum", Field: "amount", As: "s"}, {Op: "avg", Field: "amount", As: "a"}, {Op: "count", Field: "amount", As: "c"}}},
	{Aggregates: []data.Aggregate{{Op: "sum", Field: "n", As: "s"}, {Op: "avg", Field: "n", As: "a"}, {Op: "count", As: "count"}}},
	{GroupBy: "tags", Aggregates: []data.Aggregate{{Op: "count", As: "count"}, {Op: "min", Field: "due", As: "first"}, {Op: "max", Field: "due", As: "last"}}},
	{GroupBy: "done", Where: map[string]json.RawMessage{"tags": json.RawMessage(`null`)}, Aggregates: []data.Aggregate{{Op: "count", As: "count"}}},
	{Filter: &data.Filter{Field: "n", Op: "gte", Value: json.RawMessage(`0`)}, Aggregates: []data.Aggregate{{Op: "sum", Field: "amount", As: "s"}, {Op: "max", Field: "due", As: "last"}}},
}

// TestAggregatesEqualAScans: aggregates over random records, on a type
// with indexes and one without, equal the same aggregates computed from a
// scan's records, and live ones follow random changes.
func TestAggregatesEqualAScans(t *testing.T) {
	f := setup(t)
	f.deps.Settings = func(features.ServiceArgs) recordssettings.Records {
		s := recordssettings.Defaults
		s.DebounceMs = 30
		return s
	}
	f.define("item", itemIndexes)
	f.define("plain", nil)
	r := rand.New(rand.NewPCG(seed, 11))
	var ids []string
	for range 150 {
		v := randomValues(r)
		res := f.ok("record.create", "", map[string]any{"entity": "item", "values": v})
		f.ok("record.create", res.Target, map[string]any{"entity": "plain", "values": v})
		ids = append(ids, res.Target)
	}
	f.settle()

	check := func(when string) {
		for _, q := range aggregations {
			want := f.expect(t, q)
			for _, entity := range []string{"item", "plain"} {
				q.Entity = entity
				got, err := data.Summarize(args(), *f.deps, q)
				if err != nil {
					t.Fatal(err)
				}
				if canon(got.Groups) != want {
					t.Fatalf("%s, %s %+v:\n got %s\nwant %s", when, entity, q, canon(got.Groups), want)
				}
			}
		}
	}
	check("at first")
	var subs []*aggSink
	for _, q := range aggregations {
		q.Entity = "item"
		subs = append(subs, f.aggregate(args(), q))
	}

	b := bus.New(f.p)
	for range 80 {
		id := ids[r.IntN(len(ids))]
		if r.IntN(8) == 0 {
			f.on(b, "record.delete", id, map[string]any{"entity": "item"})
			f.on(b, "record.delete", id, map[string]any{"entity": "plain"})
			continue
		}
		set := map[string]any{}
		for k, v := range randomValues(r) {
			if r.IntN(3) == 0 {
				set[k] = v
			}
		}
		if r.IntN(4) == 0 {
			set["amount"] = nil
		}
		f.on(b, "record.update", id, map[string]any{"entity": "item", "set": set})
		f.on(b, "record.update", id, map[string]any{"entity": "plain", "set": set})
	}
	f.settle()
	check("after changes")
	for i, q := range aggregations {
		q.Entity = "item"
		eventually(t, func() string {
			if subs[i].bad != "" {
				t.Fatal(subs[i].bad)
			}
			fresh, _ := data.Summarize(args(), *f.deps, q)
			if got, want := subs[i].groups(), canon(fresh.Groups); got != want {
				return fmt.Sprintf("live %+v:\n got %s\nwant %s", q, got, want)
			}
			return ""
		})
	}

	// Bad aggregations are refused
	for _, q := range []data.Aggregation{
		{Entity: "item"},
		{Entity: "item", Aggregates: []data.Aggregate{{Op: "median", Field: "n"}}},
		{Entity: "item", Aggregates: []data.Aggregate{{Op: "sum", Field: "title"}}},
		{Entity: "item", Aggregates: []data.Aggregate{{Op: "max", Field: "tags"}}},
		{Entity: "item", Aggregates: []data.Aggregate{{Op: "sum"}}},
		{Entity: "item", GroupBy: "nope", Aggregates: []data.Aggregate{{Op: "count"}}},
		{Entity: "item", Aggregates: []data.Aggregate{{Op: "count"}, {Op: "count"}}},
	} {
		if _, err := data.Summarize(args(), *f.deps, q); err == nil {
			t.Errorf("%+v was not refused", q)
		}
	}
}

// TestABurstRunsAnAggregateOncePerWindow: commands sent faster than the
// debounce window run the aggregate once per window, and it ends equal
// to a fresh one.
func TestABurstRunsAnAggregateOncePerWindow(t *testing.T) {
	f := setup(t)
	const window = 400 * time.Millisecond
	f.deps.Settings = func(features.ServiceArgs) recordssettings.Records {
		s := recordssettings.Defaults
		s.DebounceMs = int(window / time.Millisecond)
		return s
	}
	f.task()
	q := data.Aggregation{Entity: "task", GroupBy: "done", Aggregates: []data.Aggregate{{Op: "count"}}}
	s := f.aggregate(args(), q)
	start := time.Now()
	var wg sync.WaitGroup
	for c := range 4 {
		b := bus.New(f.p)
		wg.Go(func() {
			for i := range 15 {
				f.on(b, "record.create", "", map[string]any{"entity": "task", "values": map[string]any{"title": fmt.Sprint(c, i), "done": i%3 == 0}})
			}
		})
	}
	wg.Wait()
	elapsed := time.Since(start)
	eventually(t, func() string {
		fresh, _ := data.Summarize(args(), *f.deps, q)
		if got, want := s.groups(), canon(fresh.Groups); got != want || !strings.Contains(want, `"count":20`) {
			return fmt.Sprintf("got %s want %s", got, want)
		}
		return ""
	})
	time.Sleep(window * 2) // any run still to come
	s.mu.Lock()
	defer s.mu.Unlock()
	// A run sends a group's update or insert for each group that changed,
	// so runs are at most the changes to the done=false group
	runs := 0
	for _, d := range s.deltas {
		if d.List == data.GroupsName && d.Key == "false" {
			runs++
		}
	}
	if most := int(elapsed/window) + 2; runs > most || runs == 0 {
		t.Fatalf("%d runs in %v with a window of %v (at most %d)", runs, elapsed, window, most)
	}
	t.Logf("60 commands in %v ran the aggregate %d times", elapsed, runs)
}
