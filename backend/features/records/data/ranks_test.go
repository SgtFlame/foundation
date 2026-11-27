package data_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
)

// card defines and publishes Card: a title, a status and a position
// within it, indexed by status then position.
func (f *fixture) card(id string, sql bool) {
	f.t.Helper()
	f.ok("entity.create", id, map[string]any{"name": id, "fields": []any{
		map[string]any{"id": "title", "name": "Title", "type": "text"},
		map[string]any{"id": "status", "name": "Status", "type": "text"},
		map[string]any{"id": "pos", "name": "Position", "type": "rank", "scope": "status"},
	}})
	f.ok("entity.addIndex", id, map[string]any{"fields": []string{"status", "pos"}})
	rev := 2
	if sql {
		f.ok("entity.setStorage", id, map[string]any{"storage": map[string]any{"projection": "sql"}})
		rev++
	}
	f.publish(id, rev)
}

// column is a status's cards' titles in position order, as a query sorted
// by position gives them, once it is one of want (an index follows its
// bucket by a watch, so a read just after a commit may lag): the last
// read when none matches within three seconds.
func (f *fixture) column(entity, status string, want ...[]string) []string {
	f.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := f.readColumn(entity, status)
		if len(want) == 0 || slices.ContainsFunc(want, func(w []string) bool { return slices.Equal(w, got) }) || time.Now().After(deadline) {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (f *fixture) readColumn(entity, status string) []string {
	f.t.Helper()
	res, err := data.Find(args(), *f.deps, data.Query{Entity: entity, Filter: &data.Filter{Field: "status", Value: raw(status)}, Sort: []data.Sort{{Field: "pos"}}, Limit: 1000})
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for _, r := range res.Records {
		var s string
		_ = json.Unmarshal(r["title"], &s)
		out = append(out, s)
	}
	return out
}

func (f *fixture) makeCard(entity, title, status, after string) string {
	f.t.Helper()
	res := f.ok("record.create", "", map[string]any{"entity": entity, "values": map[string]any{"title": title, "status": status}})
	res = f.ok("record.move", res.Target, map[string]any{"entity": entity, "field": "pos", "after": after})
	f.waitRecord(entity, res.Target, res.Seq)
	return res.Target
}

func (f *fixture) waitRecord(entity, id string, seq uint64) data.Record {
	f.t.Helper()
	v, err := f.p.WaitFor(context.Background(), features.LocalTenantID, data.Projector, data.Key(entity, id), seq, 3*time.Second)
	if err != nil || v.Seq < seq {
		f.t.Fatalf("%s %s did not reach %d: %v", entity, id, seq, err)
	}
	var r data.Record
	_ = json.Unmarshal(v.Value, &r)
	return r
}

func TestMoveOrdersRecords(t *testing.T) {
	f := setup(t)
	f.card("card", false)
	a := f.makeCard("card", "A", "todo", "")
	b := f.makeCard("card", "B", "todo", a)
	c := f.makeCard("card", "C", "todo", b)
	if got := f.column("card", "todo", []string{"A", "B", "C"}); !slices.Equal(got, []string{"A", "B", "C"}) {
		t.Fatalf("todo = %v", got)
	}
	// C to the top: before A, with nothing after
	res := f.ok("record.move", c, map[string]any{"entity": "card", "field": "pos", "before": a})
	f.waitRecord("card", c, res.Seq)
	if got := f.column("card", "todo", []string{"C", "A", "B"}); !slices.Equal(got, []string{"C", "A", "B"}) {
		t.Fatalf("after moving C up, todo = %v", got)
	}
	// A dropped in another column, status and position in one event
	res = f.ok("record.move", a, map[string]any{"entity": "card", "field": "pos", "set": map[string]any{"status": "doing"}})
	r := f.waitRecord("card", a, res.Seq)
	if str(r["status"]) != `"doing"` || len(f.events(a)) == 0 {
		t.Fatalf("A = %v", r)
	}
	if got := f.column("card", "todo", []string{"C", "B"}); !slices.Equal(got, []string{"C", "B"}) {
		t.Fatalf("todo = %v", got)
	}
	if got := f.column("card", "doing", []string{"A"}); !slices.Equal(got, []string{"A"}) {
		t.Fatalf("doing = %v", got)
	}
	// A rank is set only by moving
	f.rejected("record.update", b, map[string]any{"entity": "card", "set": map[string]any{"pos": "V"}}, "record.move")
	f.rejected("record.move", b, map[string]any{"entity": "card", "field": "title"}, "not a position")
	f.rejected("record.move", b, map[string]any{"entity": "card", "field": "pos", "after": b}, "beside itself")
	f.rejected("record.move", b, map[string]any{"entity": "card", "field": "pos", "after": "404"}, "no card record")
	// A typed rank is allowed on create (import, Rank between)
	f.ok("record.create", "", map[string]any{"entity": "card", "values": map[string]any{"title": "D", "status": "done", "pos": "V"}})
}

// TestConcurrentMoves moves one card from two connections at once: it ends
// in one place, the later commit's; two cards dropped in one gap at once
// both stay, between the neighbours.
func TestConcurrentMoves(t *testing.T) {
	f := setup(t)
	f.card("card", false)
	a := f.makeCard("card", "A", "todo", "")
	b := f.makeCard("card", "B", "todo", a)
	c := f.makeCard("card", "C", "todo", b)
	x := f.makeCard("card", "X", "doing", "")
	y := f.makeCard("card", "Y", "doing", x)

	one, two := bus.New(f.p), bus.New(f.p)
	var wg sync.WaitGroup
	results := make([]bus.Result, 2)
	moves := []map[string]any{
		{"entity": "card", "field": "pos", "after": a, "before": b, "set": map[string]any{"status": "todo"}}, // between A and B in todo
		{"entity": "card", "field": "pos", "after": x, "before": y, "set": map[string]any{"status": "doing"}},
	}
	for i, conn := range []*bus.Bus{one, two} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = f.on(conn, "record.move", c, moves[i])
		}()
	}
	wg.Wait()
	for _, r := range results {
		if r.Common.Code >= 300 {
			t.Fatalf("a move failed: %s", r.Common.Message)
		}
	}
	later := slices.MaxFunc(results, func(p, q bus.Result) int { return int(p.Seq) - int(q.Seq) })
	f.waitRecord("card", c, later.Seq)
	var todo, doing []string
	if later.Seq == results[0].Seq {
		todo, doing = f.column("card", "todo", []string{"A", "C", "B"}), f.column("card", "doing", []string{"X", "Y"})
		if !slices.Equal(todo, []string{"A", "C", "B"}) || !slices.Equal(doing, []string{"X", "Y"}) {
			t.Fatalf("the later move to todo lost: todo %v, doing %v", todo, doing)
		}
	} else if todo, doing = f.column("card", "todo", []string{"A", "B"}), f.column("card", "doing", []string{"X", "C", "Y"}); !slices.Equal(todo, []string{"A", "B"}) || !slices.Equal(doing, []string{"X", "C", "Y"}) {
		t.Fatalf("the later move to doing lost: todo %v, doing %v", todo, doing)
	}

	// Two different cards dropped in the same gap at once both stay there
	p := f.makeCard("card", "P", "review", "")
	q := f.makeCard("card", "Q", "review", p)
	m := f.makeCard("card", "M", "review", q)
	n := f.makeCard("card", "N", "review", m)
	for i, id := range []string{m, n} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = f.on([]*bus.Bus{one, two}[i], "record.move", id, map[string]any{"entity": "card", "field": "pos", "after": p, "before": q})
		}()
	}
	wg.Wait()
	for i, id := range []string{m, n} {
		f.waitRecord("card", id, results[i].Seq)
	}
	got := f.column("card", "review", []string{"P", "M", "N", "Q"}, []string{"P", "N", "M", "Q"})
	if len(got) != 4 || got[0] != "P" || got[3] != "Q" {
		t.Fatalf("review = %v, want P, then M and N, then Q", got)
	}
}

// TestRebalance moves cards into one gap until a rank passes the length
// at which the follower rebalances: the scope is rewritten evenly, in the
// same order, in one batch, and other scopes are left alone.
func TestRebalance(t *testing.T) {
	f := setup(t)
	f.card("card", false)
	ids := []string{f.makeCard("card", "first", "todo", "")}
	last := f.makeCard("card", "last", "todo", ids[0])
	other := f.makeCard("card", "other", "doing", "")
	otherRank := str(f.waitRecord("card", other, 0)["pos"])
	want := []string{"first"}
	// Each new card goes just before last: the gap shrinks every time
	var r data.Record
	for i := 0; ; i++ {
		res := f.ok("record.create", "", map[string]any{"entity": "card", "values": map[string]any{"title": fmt.Sprint(i), "status": "todo"}})
		res = f.ok("record.move", res.Target, map[string]any{"entity": "card", "field": "pos", "after": ids[len(ids)-1], "before": last})
		r = f.waitRecord("card", res.Target, res.Seq)
		ids = append(ids, res.Target)
		want = append(want, fmt.Sprint(i))
		if len(str(r["pos"])) > records.RankRebalanceAt+2 {
			break
		}
		if i > 2000 {
			t.Fatal("the rank never grew")
		}
	}
	want = append(want, "last")
	deadline := time.Now().Add(10 * time.Second)
	for {
		longest := 0
		res, _ := data.Find(args(), *f.deps, data.Query{Entity: "card", Filter: &data.Filter{Field: "status", Value: raw("todo")}, Limit: 5000})
		for _, rec := range res.Records {
			longest = max(longest, len(str(rec["pos"])))
		}
		if longest <= 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never rebalanced: longest rank %d", longest)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := f.column("card", "todo", want); !slices.Equal(got, want) {
		t.Fatalf("the order changed: %v", got)
	}
	if got := str(f.waitRecord("card", other, 0)["pos"]); got != otherRank {
		t.Errorf("another scope's rank changed: %s → %s", otherRank, got)
	}
	// One batch: the rebalance's events share it
	batches := map[string]bool{}
	for _, id := range ids[:3] {
		evs := f.events(id)
		e := evs[len(evs)-1]
		if e.Batch == nil {
			t.Fatalf("%s's last event is not in a batch: %+v", id, e)
		}
		batches[e.CommandID] = true
	}
	if len(batches) != 1 {
		t.Errorf("the rebalance's events came from %d commands", len(batches))
	}
}

// TestRankOrderAgrees reads one shuffled column by an index walk, a scan
// and a large table: the same order.
func TestRankOrderAgrees(t *testing.T) {
	f := setup(t)
	f.withTables()
	f.card("kv", false)
	f.card("big", true)
	f.ok("entity.create", "scan", map[string]any{"name": "scan", "fields": []any{
		map[string]any{"id": "title", "name": "Title", "type": "text"},
		map[string]any{"id": "status", "name": "Status", "type": "text"},
		map[string]any{"id": "pos", "name": "Position", "type": "rank", "scope": "status"},
	}})
	f.publish("scan", 1)
	order := []string{}
	for i := range 40 {
		title := fmt.Sprint(i)
		at := (i * 7) % (len(order) + 1)
		var after, before string
		if at > 0 {
			after = order[at-1]
		}
		if at < len(order) {
			before = order[at]
		}
		rank, err := records.RankBetween(after, before)
		if err != nil {
			t.Fatal(err)
		}
		order = slices.Insert(order, at, rank)
		for _, e := range []string{"kv", "big", "scan"} {
			res := f.ok("record.create", "", map[string]any{"entity": e, "values": map[string]any{"title": title, "status": "todo", "pos": rank}})
			f.waitRecord(e, res.Target, res.Seq)
		}
	}
	d := roomy(*f.deps)
	var cols [][]string
	var plans []string
	for _, e := range []string{"kv", "big", "scan"} {
		res, err := data.Find(args(), d, data.Query{Entity: e, Filter: &data.Filter{Field: "status", Value: raw("todo")}, Sort: []data.Sort{{Field: "pos"}}, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		var col []string
		for _, r := range res.Records {
			col = append(col, str(r["pos"]))
		}
		cols = append(cols, col)
		plans = append(plans, res.Plan.Index)
	}
	want := make([]string, len(order))
	for i, x := range order {
		want[i] = `"` + x + `"`
	}
	for i, col := range cols {
		if !slices.Equal(col, want) {
			t.Fatalf("%d (%s) gave %v", i, plans[i], head(col))
		}
	}
	if plans[1] != "sql" || plans[2] != "" {
		t.Errorf("plans = %v", plans)
	}
}
