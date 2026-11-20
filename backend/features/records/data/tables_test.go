package data_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	recordssettings "github.com/SgtFlame/foundation/backend/features/records/settings"
	"gorm.io/gorm"
)

// withTables gives the fixture's records a tenant database with tables.
func (f *fixture) withTables() (*data.Tables, *gorm.DB) {
	f.t.Helper()
	gdb, err := db.Open(db.WithPath(filepath.Join(f.t.TempDir(), "tenant.db")))
	if err != nil {
		f.t.Fatal(err)
	}
	tables, err := data.NewTables(gdb, f.p)
	if err != nil {
		f.t.Fatal(err)
	}
	f.deps.Tables = tables
	return tables, gdb
}

// large defines an item type that is a large table.
func (f *fixture) large(id string, indexes [][]string) {
	f.t.Helper()
	f.ok("entity.create", id, map[string]any{"name": id, "fields": itemFields})
	rev := 1
	for _, x := range indexes {
		f.ok("entity.addIndex", id, map[string]any{"fields": x})
		rev++
	}
	f.ok("entity.setStorage", id, map[string]any{"storage": map[string]any{"projection": "sql"}})
	f.publish(id, rev+1)
}

// load publishes created events for the same records in each type, as
// BenchmarkQueries does, and waits until they are projected.
func (f *fixture) load(n int, seed uint64, types ...string) {
	f.t.Helper()
	ctx := context.Background()
	if _, err := f.p.Stream(ctx, features.LocalTenantID, live.AreaRecords); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.p.Bucket(ctx, features.LocalTenantID, data.Projector); err != nil {
		f.t.Fatal(err)
	}
	r := rand.New(rand.NewPCG(seed, 2))
	for i := range n {
		id := fmt.Sprint(features.NewTSID())
		if r.IntN(10) == 0 {
			id = fmt.Sprint(1000 + i) // shorter ids sort first
		}
		raw, _ := json.Marshal(map[string]any{"values": randomValues(r)})
		for _, typ := range types {
			t := live.Target{Area: live.AreaRecords, Type: typ, ID: id}
			e, _ := json.Marshal(live.Event{Type: data.EventCreated, Target: t, Data: raw, CommandID: "load-" + typ + id, TenantID: features.LocalTenantID, UserID: 1, Time: time.Now()})
			if _, err := f.js.PublishAsync(t.Subject(features.LocalTenantID, data.EventCreated), e); err != nil {
				f.t.Fatal(err)
			}
		}
		if i%2048 == 2047 {
			<-f.js.PublishAsyncComplete()
		}
	}
	<-f.js.PublishAsyncComplete()
	f.settle()
}

// change sends an entity command and, with publish, publishes the draft
// it leaves.
func (f *fixture) change(typ, id string, a any, publish bool) {
	f.t.Helper()
	res := f.ok(typ, id, a)
	if !publish {
		return
	}
	v := f.wait("records.entity", "entity."+id, res.Seq)
	var dr struct{ Rev int }
	_ = json.Unmarshal(v.Value, &dr)
	f.publish(id, dr.Rev)
}

// pagesWith reads every page of a query, ids and next cursors.
func pagesWith(t *testing.T, d data.Deps, a features.ServiceArgs, q data.Query) ([]string, []data.Plan) {
	t.Helper()
	var ids []string
	var plans []data.Plan
	for range 1000 {
		res, err := data.Find(a, d, q)
		if err != nil {
			t.Fatalf("%+v: %v", q, err)
		}
		plans = append(plans, res.Plan)
		for _, r := range res.Records {
			var id string
			_ = json.Unmarshal(r[data.MetaID], &id)
			ids = append(ids, id)
		}
		if res.Next == "" {
			return ids, plans
		}
		q.Cursor = res.Next
	}
	t.Fatalf("%+v never ended", q)
	return nil, nil
}

func roomy(d data.Deps) data.Deps {
	d.Settings = func(features.ServiceArgs) recordssettings.Records {
		return recordssettings.Records{ScanLimit: 100_000, DebounceMs: 20}
	}
	return d
}

// TestTableAgreesWithKV reads random queries from a large table and from
// its KV twin: the same records, in the same order, page by page.
func TestTableAgreesWithKV(t *testing.T) {
	f := setup(t)
	f.withTables()
	f.define("kv", itemIndexes)
	f.large("big", itemIndexes[:3])
	f.load(600, 7, "kv", "big")
	d := roomy(*f.deps)
	r := rand.New(rand.NewPCG(11, 3))
	var kvTime, sqlTime time.Duration
	defer func() { t.Logf("KV %v, table %v", kvTime, sqlTime) }()
	for i := range 120 {
		q := data.Query{Sort: sorts[r.IntN(len(sorts))]}
		if r.IntN(4) > 0 {
			raw, _ := json.Marshal(randomFilter(r, 0))
			q.Filter = &data.Filter{}
			_ = json.Unmarshal(raw, q.Filter)
		}
		if r.IntN(3) > 0 {
			q.Limit = []int{3, 7, 50}[r.IntN(3)]
		}
		q.Entity = "kv"
		start := time.Now()
		want, _ := pagesWith(t, d, args(), q)
		kvTime += time.Since(start)
		q.Entity = "big"
		start = time.Now()
		got, plans := pagesWith(t, d, args(), q)
		sqlTime += time.Since(start)
		if !slices.Equal(got, want) {
			raw, _ := json.Marshal(q)
			t.Fatalf("query %d %s: the table gave %d records, KV %d\n got %v\nwant %v", i, raw, len(got), len(want), head(got), head(want))
		}
		if plans[0].Index != "sql" {
			t.Fatalf("query %d was not read from the table: %+v", i, plans[0])
		}
	}
}

func head(ids []string) []string { return ids[:min(len(ids), 12)] }

// TestTableAggregatesAgreeWithKV compares grouped aggregates.
func TestTableAggregatesAgreeWithKV(t *testing.T) {
	f := setup(t)
	f.withTables()
	f.define("kv", nil)
	f.large("big", nil)
	f.load(400, 9, "kv", "big")
	d := roomy(*f.deps)
	aggs := []data.Aggregate{{Op: "count"}, {Op: "count", Field: "due"}, {Op: "sum", Field: "n"}, {Op: "avg", Field: "amount"},
		{Op: "sum", Field: "price"}, {Op: "min", Field: "title"}, {Op: "max", Field: "at"}, {Op: "min", Field: "level"}}
	r := rand.New(rand.NewPCG(5, 5))
	for i, g := range []string{"", "done", "level", "tags", "title", "due", "n"} {
		for j := range 6 {
			q := data.Aggregation{GroupBy: g, Aggregates: aggs}
			if j > 0 {
				raw, _ := json.Marshal(randomFilter(r, 0))
				q.Filter = &data.Filter{}
				_ = json.Unmarshal(raw, q.Filter)
			}
			q.Entity = "kv"
			want, err := data.Summarize(args(), d, q)
			if err != nil {
				t.Fatal(err)
			}
			q.Entity = "big"
			got, err := data.Summarize(args(), d, q)
			if err != nil {
				t.Fatal(err)
			}
			if !sameGroups(got.Groups, want.Groups) {
				raw, _ := json.Marshal(q)
				gs, _ := json.Marshal(got.Groups)
				ws, _ := json.Marshal(want.Groups)
				t.Fatalf("aggregation %d.%d %s:\n got %s\nwant %s", i, j, raw, gs, ws)
			}
		}
	}
}

// sameGroups compares groups as JSON, numbers within float rounding.
func sameGroups(a, b []data.Group) bool {
	var x, y any
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	_ = json.Unmarshal(ra, &x)
	_ = json.Unmarshal(rb, &y)
	return near(x, y)
}

func near(a, b any) bool {
	switch x := a.(type) {
	case float64:
		y, ok := b.(float64)
		return ok && (x == y || abs(x-y) <= 1e-9*max(abs(x), abs(y)))
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k := range x {
			if !near(x[k], y[k]) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !near(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// TestTableFollowsCommands: a record changed by a command shows in an open
// records.find window over the table, and in a one-time read at once.
func TestTableFollowsCommands(t *testing.T) {
	f := setup(t)
	f.withTables()
	f.large("big", [][]string{{"n"}})
	f.deps.Settings = func(features.ServiceArgs) recordssettings.Records {
		return recordssettings.Records{ScanLimit: 1000, DebounceMs: 20}
	}
	for i := range 20 {
		f.ok("record.create", "", map[string]any{"entity": "big", "values": map[string]any{"title": fmt.Sprint("r", i), "n": i}})
	}
	q := data.Query{Entity: "big", Sort: []data.Sort{{Field: "n", Desc: true}}, Limit: 5}
	s := f.find(q)
	f.matches(t, s, q)
	res := f.ok("record.create", "", map[string]any{"entity": "big", "values": map[string]any{"title": "top", "n": 100}})
	id := res.Target
	page, err := data.Find(args(), *f.deps, q)
	if err != nil || len(page.Records) == 0 || string(page.Records[0][data.MetaID]) != fmt.Sprintf("%q", id) {
		t.Fatalf("a read right after the command does not show it: %v %v", err, page.Records)
	}
	f.matches(t, s, q)
	f.ok("record.update", id, map[string]any{"entity": "big", "set": map[string]any{"n": -1}})
	f.matches(t, s, q)
	f.ok("record.addTo", id, map[string]any{"entity": "big", "field": "tags", "items": []any{"red"}})
	contains := data.Query{Entity: "big", Filter: &data.Filter{Field: "tags", Op: "contains", Value: json.RawMessage(`"red"`)}}
	got, err := data.Find(args(), *f.deps, contains)
	if err != nil || len(got.Records) != 1 {
		t.Fatalf("tags contains red: %v %v", err, got.Records)
	}
	if s.bad != "" {
		t.Fatal(s.bad)
	}
}

// dump is a table's rows as they are stored, but for the table's name.
func dump(t *testing.T, gdb *gorm.DB, entity string) []map[string]any {
	t.Helper()
	var st records.ProjectedTable
	if err := gdb.Where("entity = ?", entity).Take(&st).Error; err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := gdb.Raw(fmt.Sprintf(`SELECT * FROM %q ORDER BY id`, st.Name)).Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

// TestTableRebuildsTheSame: a table dropped and rebuilt from the stream
// gives the same rows; a change of what a column holds rebuilds it, and an
// added field only adds columns.
func TestTableRebuildsTheSame(t *testing.T) {
	f := setup(t)
	tables, gdb := f.withTables()
	f.large("big", [][]string{{"level"}})
	f.load(200, 3, "big")
	some, err := data.Find(args(), *f.deps, data.Query{Entity: "big", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for i, r := range some.Records { // changes after the creates, folded by the follower
		var id string
		_ = json.Unmarshal(r[data.MetaID], &id)
		ids = append(ids, id)
		f.ok("record.update", id, map[string]any{"entity": "big", "set": map[string]any{"title": fmt.Sprint("changed ", i)}})
	}
	f.ok("record.delete", ids[9], map[string]any{"entity": "big"})
	if _, err := data.Find(args(), *f.deps, data.Query{Entity: "big", Limit: 1}); err != nil {
		t.Fatal(err)
	}
	before := dump(t, gdb, "big")
	if err := tables.Rebuild(args(), "big"); err != nil {
		t.Fatal(err)
	}
	after := dump(t, gdb, "big")
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("the rebuilt table differs: %d rows before, %d after", len(before), len(after))
	}
	name := func() string {
		var st records.ProjectedTable
		_ = gdb.Where("entity = ?", "big").Take(&st).Error
		return st.Name
	}
	// An added field adds a column; the table stays
	was := name()
	f.change("entity.addField", "big", map[string]any{"name": "note", "type": "text"}, true)
	f.ok("record.update", ids[0], map[string]any{"entity": "big", "set": map[string]any{"note": "hello"}})
	res, err := data.Find(args(), *f.deps, data.Query{Entity: "big", Where: map[string]json.RawMessage{"note": json.RawMessage(`"hello"`)}})
	if err != nil || len(res.Records) != 1 {
		t.Fatalf("the added field is not queried: %v %v", err, res.Records)
	}
	if name() != was {
		t.Fatal("an added field rebuilt the table")
	}
	// Reordered options change their order: the table is rebuilt
	f.change("entity.updateField", "big", map[string]any{"field": "level", "options": []any{map[string]any{"id": "high", "name": "high"}, map[string]any{"id": "medium", "name": "medium"}, map[string]any{"id": "low", "name": "low"}}}, true)
	eventually(t, func() string { // the KV index is rebuilt in the background
		kv, _ := pagesWith(t, roomy(data.Deps{Projections: f.p}), args(), data.Query{Entity: "big", Sort: []data.Sort{{Field: "level"}}})
		sql, _ := pagesWith(t, roomy(*f.deps), args(), data.Query{Entity: "big", Sort: []data.Sort{{Field: "level"}}})
		if !slices.Equal(kv, sql) {
			return fmt.Sprintf("after reordering options the table sorts differently from KV: %d %d", len(kv), len(sql))
		}
		return ""
	})
	if name() == was {
		t.Fatal("reordered options did not rebuild the table")
	}
	// No longer a large table: the table is dropped
	f.change("entity.setStorage", "big", map[string]any{"storage": map[string]any{}}, true)
	if _, err := data.Find(args(), *f.deps, data.Query{Entity: "big", Limit: 1}); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() string {
		if gdb.Migrator().HasTable(name()) || name() != "" {
			return "the table is still there"
		}
		return ""
	})
}

// TestTableRowRules: a row rule hides rows in SQL as it does in KV, for
// rules that compile to SQL and for those checked in Go.
func TestTableRowRules(t *testing.T) {
	f := setup(t)
	f.withTables()
	fields := []any{
		map[string]any{"name": "title", "type": "text"},
		map[string]any{"name": "owner", "type": "user"},
		map[string]any{"name": "shared", "type": "bool"},
		map[string]any{"name": "n", "type": "number"},
	}
	for _, id := range []string{"kvdoc", "sqldoc"} {
		f.ok("entity.create", id, map[string]any{"name": id, "fields": fields})
		rev := 1
		if id == "sqldoc" {
			f.ok("entity.setStorage", id, map[string]any{"storage": map[string]any{"projection": "sql"}})
			rev++
		}
		f.publish(id, rev)
	}
	r := rand.New(rand.NewPCG(1, 9))
	for i := range 120 {
		values := map[string]any{"title": fmt.Sprint("doc ", i), "owner": fmt.Sprint(2 + r.IntN(3)), "shared": r.IntN(4) == 0, "n": r.IntN(10)}
		for _, id := range []string{"kvdoc", "sqldoc"} {
			f.ok("record.create", "", map[string]any{"entity": id, "values": values})
		}
	}
	rules := []string{
		"record.owner == principal.user || record.shared == true",
		"!(record.owner != principal.user) && record.n >= 3",
		"record.title startsWith 'doc 1' || record.owner == principal.user", // not compiled: checked in Go
		"record.owner in ['2', '3'] and not record.shared",
	}
	for _, rule := range rules {
		for _, id := range []string{"kvdoc", "sqldoc"} {
			f.change("entity.setAccess", id, map[string]any{"access": map[string]any{"read": rule}}, true)
		}
		for _, user := range []uint64{2, 3, 4} {
			for _, q := range []data.Query{{Sort: []data.Sort{{Field: "title"}}, Limit: 9}, {Filter: &data.Filter{Field: "n", Op: "lt", Value: json.RawMessage(`5`)}}} {
				q.Entity = "kvdoc"
				want, _ := pagesWith(t, roomy(*f.deps), as(user), q)
				q.Entity = "sqldoc"
				got, _ := pagesWith(t, roomy(*f.deps), as(user), q)
				if len(got) != len(want) {
					t.Fatalf("rule %q, user %d: the table shows %d records, KV %d", rule, user, len(got), len(want))
				}
			}
			agg := data.Aggregation{GroupBy: "owner", Aggregates: []data.Aggregate{{Op: "count"}, {Op: "sum", Field: "n"}}}
			agg.Entity = "kvdoc"
			want, err := data.Summarize(as(user), roomy(*f.deps), agg)
			if err != nil {
				t.Fatal(err)
			}
			agg.Entity = "sqldoc"
			got, err := data.Summarize(as(user), roomy(*f.deps), agg)
			if err != nil || !sameGroups(got.Groups, want.Groups) {
				t.Fatalf("rule %q, user %d: aggregates differ: %v %+v %+v", rule, user, err, got.Groups, want.Groups)
			}
		}
	}
	e := records.EntityType{ID: "x", Fields: []records.Field{{ID: "owner", Type: "user"}, {ID: "title", Type: "text"}, {ID: "shared", Type: "bool"}}}
	if !data.RuleCompiles(e, rules[0]) || data.RuleCompiles(e, rules[2]) {
		t.Fatal("RuleCompiles is wrong about which rules compile")
	}
}

// TestTableAtScale: 50 000 records list, count and page within budget.
// The full-text budget is the LanceDB index's (records/search).
func TestTableAtScale(t *testing.T) {
	if testing.Short() {
		t.Skip("loads 50 000 records")
	}
	const n = 50_000
	f := setup(t)
	f.withTables()
	f.ok("entity.create", "mail", map[string]any{"name": "mail", "fields": itemFields})
	f.ok("entity.addIndex", "mail", map[string]any{"fields": []string{"at"}})
	f.ok("entity.addIndex", "mail", map[string]any{"fields": []string{"done", "at"}})
	f.ok("entity.addIndex", "mail", map[string]any{"fields": []string{"level"}})
	f.load(n, 1, "mail")
	start := time.Now()
	f.ok("entity.setStorage", "mail", map[string]any{"storage": map[string]any{"projection": "sql"}})
	f.publish("mail", 5)
	d := roomy(*f.deps)
	if _, err := data.Find(args(), d, data.Query{Entity: "mail", Limit: 1}); err != nil {
		t.Fatal(err)
	}
	t.Logf("table of %d records built in %v", n, time.Since(start).Round(time.Millisecond))
	best := func(fn func() error) time.Duration {
		b := time.Hour
		for range 5 {
			s := time.Now()
			if err := fn(); err != nil {
				t.Fatal(err)
			}
			b = min(b, time.Since(s))
		}
		return b
	}
	page := best(func() error {
		res, err := data.Find(args(), d, data.Query{Entity: "mail", Where: map[string]json.RawMessage{"done": json.RawMessage(`false`)}, Sort: []data.Sort{{Field: "at", Desc: true}}, Limit: 50})
		if err == nil && len(res.Records) != 50 {
			err = fmt.Errorf("%d records", len(res.Records))
		}
		return err
	})
	count := best(func() error {
		_, err := data.Summarize(args(), d, data.Aggregation{Entity: "mail", GroupBy: "level", Aggregates: []data.Aggregate{{Op: "count"}}})
		return err
	})
	t.Logf("sorted page of 50: %v, grouped count: %v", page, count)
	if page > 20*time.Millisecond {
		t.Errorf("a sorted page of 50 took %v, over 20 ms", page)
	}
	if count > 50*time.Millisecond {
		t.Errorf("a grouped count took %v, over 50 ms", count)
	}
}
