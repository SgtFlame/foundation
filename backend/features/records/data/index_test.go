package data_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
	recordssettings "github.com/SgtFlame/foundation/backend/features/records/settings"
)

// itemFields are the fields of the property test's types: every sortable
// kind, and a list.
var itemFields = []any{
	map[string]any{"name": "title", "type": "text"},
	map[string]any{"name": "n", "type": "number"},
	map[string]any{"name": "amount", "type": "decimal", "scale": 3},
	map[string]any{"name": "price", "type": "money", "currency": "USD"},
	map[string]any{"name": "done", "type": "bool"},
	map[string]any{"name": "due", "type": "date"},
	map[string]any{"name": "at", "type": "dateTime"},
	map[string]any{"name": "level", "type": "enum", "options": []any{map[string]any{"id": "low", "name": "low"}, map[string]any{"id": "medium", "name": "medium"}, map[string]any{"id": "high", "name": "high"}}},
	map[string]any{"name": "tags", "type": "tags"},
}

var itemIndexes = [][]string{{"n"}, {"amount"}, {"price"}, {"title"}, {"done", "due"}, {"at"}, {"level"}, {"tags", "n"}}

// define makes an entity type with the item fields and these indexes.
func (f *fixture) define(id string, indexes [][]string) {
	f.t.Helper()
	f.ok("entity.create", id, map[string]any{"name": id, "fields": itemFields})
	rev := 1
	for _, x := range indexes {
		f.ok("entity.addIndex", id, map[string]any{"fields": x})
		rev++
	}
	f.publish(id, rev)
}

// settle waits until the projector has folded every command so far.
func (f *fixture) settle() {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := f.p.Settle(ctx, features.LocalTenantID, data.Projector); err != nil {
		f.t.Fatal(err)
	}
}

// randomValues are an item's values, some left out.
func randomValues(r *rand.Rand) map[string]any {
	v := map[string]any{}
	maybe := func(field string, value any) {
		if r.IntN(6) > 0 {
			v[field] = value
		}
	}
	words := []string{"apple", "Apricot", "banana", "Berry", "cherry", "apple pie", "Ápple", "date"}
	maybe("title", words[r.IntN(len(words))]+fmt.Sprint(r.IntN(3)))
	maybe("n", float64(r.IntN(41)-20)/4)
	maybe("amount", fmt.Sprintf("%.3f", float64(r.IntN(20001)-10000)/1000))
	maybe("price", fmt.Sprintf("%d.%02d", r.IntN(50), r.IntN(100)))
	maybe("done", r.IntN(2) == 0)
	maybe("due", time.Date(2020+r.IntN(10), time.Month(r.IntN(12)+1), r.IntN(28)+1, 0, 0, 0, 0, time.UTC).Format(time.DateOnly))
	maybe("at", time.Date(1960+r.IntN(80), 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(r.Int64N(int64(365*24*time.Hour)))).Format(time.RFC3339Nano))
	maybe("level", []string{"low", "medium", "high"}[r.IntN(3)])
	var tags []string
	for _, t := range []string{"red", "green", "blue"} {
		if r.IntN(3) == 0 {
			tags = append(tags, t)
		}
	}
	maybe("tags", tags)
	return v
}

// randomFilter is a condition the planner may answer from an index, or not.
func randomFilter(r *rand.Rand, depth int) map[string]any {
	if depth < 2 && r.IntN(4) == 0 {
		op := []string{"and", "or"}[r.IntN(2)]
		return map[string]any{op: []any{randomFilter(r, depth+1), randomFilter(r, depth+1)}}
	}
	v := randomValues(rand.New(rand.NewPCG(r.Uint64(), 0)))
	pick := func(field string) any {
		if x, ok := v[field]; ok {
			return x
		}
		return nil
	}
	switch r.IntN(13) {
	case 0:
		return map[string]any{"field": "done", "value": r.IntN(2) == 0}
	case 1:
		return map[string]any{"field": "n", "op": []string{"lt", "lte", "gt", "gte"}[r.IntN(4)], "value": float64(r.IntN(41)-20) / 4}
	case 2:
		return map[string]any{"field": "amount", "op": []string{"lt", "gte"}[r.IntN(2)], "value": fmt.Sprintf("%.1f", float64(r.IntN(201)-100)/10)}
	case 3:
		return map[string]any{"field": "title", "op": "prefix", "value": []string{"ap", "AP", "b", "apple ", "Á", "zz"}[r.IntN(6)]}
	case 4:
		return map[string]any{"field": "level", "op": "in", "value": []string{"low", "high"}[:r.IntN(2)+1]}
	case 5:
		return map[string]any{"field": "tags", "op": "contains", "value": []string{"red", "green", "blue"}[r.IntN(3)]}
	case 6:
		return map[string]any{"field": "due", "op": "gte", "value": fmt.Sprintf("%d-06-01", 2020+r.IntN(10))}
	case 7:
		if at := pick("at"); at != nil {
			return map[string]any{"field": "at", "op": "lt", "value": at}
		}
		return map[string]any{"field": "at", "op": "gt", "value": "2000-01-01T00:00:00Z"}
	case 8:
		return map[string]any{"field": "level", "value": pick("level")} // eq, or no value
	case 9:
		return map[string]any{"field": "price", "op": "gt", "value": fmt.Sprintf("%d", r.IntN(50))}
	case 10:
		return map[string]any{"field": "n", "op": "ne", "value": float64(r.IntN(9) - 4)}
	case 11:
		return map[string]any{"field": "title", "op": "contains", "value": "rr"}
	}
	return map[string]any{"and": []any{map[string]any{"field": "done", "value": true}, map[string]any{"field": "due", "op": "lt", "value": "2025-01-01"}}}
}

var sorts = [][]data.Sort{
	nil,
	{{Field: "n"}},
	{{Field: "n", Desc: true}},
	{{Field: "amount", Desc: true}},
	{{Field: "title"}},
	{{Field: "done"}, {Field: "due"}},
	{{Field: "done", Desc: true}, {Field: "due", Desc: true}},
	{{Field: "done"}, {Field: "due", Desc: true}},
	{{Field: "at"}},
	{{Field: "level", Desc: true}},
	{{Field: "price"}},
	{{Field: "_createdAt", Desc: true}},
}

// pages reads every page of a query, and the plans they were read by.
func (f *fixture) pages(q data.Query) (ids []string, plans []data.Plan) {
	f.t.Helper()
	for range 1000 {
		res, err := data.Find(args(), data.Deps{Projections: f.p}, q)
		if err != nil {
			f.t.Fatalf("%+v: %v", q, err)
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
	f.t.Fatalf("%+v did not end", q)
	return nil, nil
}

// TestIndexesAnswerAsAScanDoes is the acceptance test: an indexed type and
// an unindexed one hold the same random records, and every random query,
// read page by page, gives the same records in the same order from both.
func TestIndexesAnswerAsAScanDoes(t *testing.T) {
	f := setup(t)
	f.define("item", itemIndexes)
	f.define("plain", nil)
	r := rand.New(rand.NewPCG(7, 11))

	const n = 300
	ids := make([]string, n)
	for i := range n {
		v := randomValues(r)
		res := f.ok("record.create", "", map[string]any{"entity": "item", "values": v})
		ids[i] = res.Target
		f.ok("record.create", res.Target, map[string]any{"entity": "plain", "values": v})
	}

	compare := func(round string, queries int) {
		t.Helper()
		f.settle()
		indexed := 0
		for i := range queries {
			raw, _ := json.Marshal(randomFilter(r, 0))
			var filter data.Filter
			_ = json.Unmarshal(raw, &filter)
			q := data.Query{Filter: &filter, Sort: sorts[r.IntN(len(sorts))], Limit: []int{0, 0, 13, 60}[r.IntN(4)]}
			if i%7 == 0 {
				q.Filter = nil
			}
			q.Entity = "plain"
			want, plans := f.pages(q)
			if !plans[0].Scan {
				t.Fatalf("plain was not scanned: %+v", plans[0])
			}
			q.Entity = "item"
			// The trees follow the bucket a moment behind the projector
			var got []string
			for deadline := time.Now().Add(3 * time.Second); ; {
				got, plans = f.pages(q)
				if slices.Equal(got, want) || time.Now().After(deadline) {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("%s: %s sort %v limit %d by %+v:\n got %d %v\nwant %d %v", round, raw, q.Sort, q.Limit, plans[0], len(got), got, len(want), want)
			}
			if plans[0].Index != "" {
				indexed++
			}
		}
		if indexed < queries/2 {
			t.Errorf("%s: only %d of %d queries used an index", round, indexed, queries)
		}
	}
	compare("created", 300)

	// Changes and deletes move and remove entries
	for range 150 {
		id := ids[r.IntN(n)]
		if r.IntN(10) == 0 {
			f.ok("record.delete", id, map[string]any{"entity": "item"})
			f.ok("record.delete", id, map[string]any{"entity": "plain"})
			continue
		}
		set := randomValues(r)
		for k := range set {
			if r.IntN(2) == 0 {
				delete(set, k)
			}
		}
		if r.IntN(4) == 0 {
			set["n"] = nil
		}
		f.on(f.bus, "record.update", id, map[string]any{"entity": "item", "set": set})
		f.on(f.bus, "record.update", id, map[string]any{"entity": "plain", "set": set})
	}
	compare("changed", 300)
}

func TestAScanStopsAtTheLimitAndSaysSo(t *testing.T) {
	f := setup(t)
	f.define("item", [][]string{{"n"}})
	rows := make([]map[string]json.RawMessage, 150)
	for i := range rows {
		rows[i] = map[string]json.RawMessage{"n": json.RawMessage(fmt.Sprint(i)), "title": json.RawMessage(`"x"`)}
	}
	f.svc.Import(recordservice.ImportArgs{ServiceArgs: args(), Entity: "item", Rows: rows})
	f.settle()
	d := data.Deps{Projections: f.p, Settings: func(features.ServiceArgs) recordssettings.Records { return recordssettings.Records{ScanLimit: 100} }}

	res, err := data.Find(args(), d, data.Query{Entity: "item", Filter: &data.Filter{Field: "title", Value: json.RawMessage(`"x"`)}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Plan.Scan || res.Plan.Complete || res.Plan.Read != 100 || len(res.Records) != 100 {
		t.Errorf("unindexed filter: %+v, %d records", res.Plan, len(res.Records))
	}
	res, err = data.Find(args(), d, data.Query{Entity: "item", Filter: &data.Filter{Field: "n", Op: "gte", Value: json.RawMessage(`20`)}})
	if err != nil || res.Plan.Index != "by_n" || !res.Plan.Complete || len(res.Records) != 130 {
		t.Errorf("indexed filter: %+v, %d records, %v", res.Plan, len(res.Records), err)
	}
}

func TestAnIndexAddedLaterIsBuiltAndFollows(t *testing.T) {
	f := setup(t)
	f.define("item", nil)
	for i := range 60 {
		f.ok("record.create", "", map[string]any{"entity": "item", "values": map[string]any{"n": i % 10, "title": fmt.Sprint("t", i)}})
	}
	f.settle()
	q := data.Query{Entity: "item", Filter: &data.Filter{Field: "n", Value: json.RawMessage(`3`)}, Sort: []data.Sort{{Field: "n"}}}
	res, _ := data.Find(args(), data.Deps{Projections: f.p}, q)
	if !res.Plan.Scan || len(res.Records) != 6 {
		t.Fatalf("before the index: %+v, %d records", res.Plan, len(res.Records))
	}

	f.ok("entity.addIndex", "item", map[string]any{"fields": []string{"n"}})
	f.publish("item", 3)
	eventually(t, func() string {
		res, _ = data.Find(args(), data.Deps{Projections: f.p}, q)
		if res.Plan.Index != "by_n" || len(res.Records) != 6 {
			return fmt.Sprintf("%+v, %d records", res.Plan, len(res.Records))
		}
		return ""
	})

	// It follows new commands: a record made, one changed, one deleted
	made := f.ok("record.create", "", map[string]any{"entity": "item", "values": map[string]any{"n": 3}}).Target
	var moved, gone string
	_ = json.Unmarshal(res.Records[0][data.MetaID], &moved)
	_ = json.Unmarshal(res.Records[1][data.MetaID], &gone)
	f.ok("record.update", moved, map[string]any{"entity": "item", "set": map[string]any{"n": 4}})
	f.ok("record.delete", gone, map[string]any{"entity": "item"})
	eventually(t, func() string {
		res, _ = data.Find(args(), data.Deps{Projections: f.p}, q)
		var got []string
		for _, r := range res.Records {
			var id string
			_ = json.Unmarshal(r[data.MetaID], &id)
			got = append(got, id)
		}
		if len(got) != 5 || !slices.Contains(got, made) || slices.Contains(got, moved) || slices.Contains(got, gone) || res.Plan.Read != 5 {
			return fmt.Sprintf("%v by %+v", got, res.Plan)
		}
		return ""
	})

	// Removing it removes the entries
	f.ok("entity.removeIndex", "item", map[string]any{"index": "by_n"})
	f.publish("item", 5)
	eventually(t, func() string {
		kv, _ := f.p.Side(context.Background(), features.LocalTenantID, data.IndexBucket)
		keys, _ := kv.ListKeysFiltered(context.Background(), "item.by_n.>")
		n := 0
		for range keys.Keys() {
			n++
		}
		if n != 0 {
			return fmt.Sprintf("%d entries left", n)
		}
		return ""
	})
}

func TestACursorWalksEveryPageOnce(t *testing.T) {
	f := setup(t)
	f.define("item", [][]string{{"done", "due"}, {"n"}})
	for i := range 100 {
		v := map[string]any{"done": i%3 == 0, "due": fmt.Sprintf("2026-01-%02d", i%5+1), "n": i % 4}
		if i%9 == 0 {
			delete(v, "due")
		}
		f.ok("record.create", "", map[string]any{"entity": "item", "values": v})
	}
	f.settle()
	for _, q := range []data.Query{
		{Entity: "item", Sort: []data.Sort{{Field: "n"}}, Limit: 7},
		{Entity: "item", Sort: []data.Sort{{Field: "n", Desc: true}}, Limit: 7},
		{Entity: "item", Filter: &data.Filter{Field: "done", Value: json.RawMessage(`false`)}, Sort: []data.Sort{{Field: "due"}}, Limit: 6},
		{Entity: "item", Filter: &data.Filter{Field: "done", Value: json.RawMessage(`false`)}, Sort: []data.Sort{{Field: "due", Desc: true}}, Limit: 6},
		{Entity: "item", Sort: []data.Sort{{Field: "due"}}, Limit: 9}, // no index sorts by due alone
	} {
		want, _ := f.pages(data.Query{Entity: q.Entity, Filter: q.Filter, Sort: q.Sort})
		var got []string
		var plans []data.Plan
		eventually(t, func() string {
			got, plans = f.pages(q)
			if !slices.Equal(got, want) {
				return fmt.Sprintf("pages of %v give %v, want %v", q.Sort, got, want)
			}
			return ""
		})
		if len(plans) < 2 {
			t.Errorf("%v read in %d pages", q.Sort, len(plans))
		}
		if ordered := plans[0].Ordered; ordered != (q.Sort[0].Field != "due" || q.Filter != nil) {
			t.Errorf("%v: plan %+v", q.Sort, plans[0])
		} else if ordered && plans[0].Read > q.Limit+1 {
			t.Errorf("%v: an ordered page read %d records", q.Sort, plans[0].Read)
		}
	}
}

func TestBadQueriesAreRefused(t *testing.T) {
	f := setup(t)
	f.define("item", nil)
	for _, c := range []struct {
		q    data.Query
		want string
	}{
		{data.Query{Entity: "item", Filter: &data.Filter{Field: "nope"}}, "no field"},
		{data.Query{Entity: "item", Filter: &data.Filter{Field: "tags", Op: "lt", Value: json.RawMessage(`"a"`)}}, "no order"},
		{data.Query{Entity: "item", Filter: &data.Filter{Field: "n", Op: "like", Value: json.RawMessage(`1`)}}, "no such operator"},
		{data.Query{Entity: "item", Filter: &data.Filter{Field: "n", Value: json.RawMessage(`"one"`)}}, "not a number"},
		{data.Query{Entity: "item", Sort: []data.Sort{{Field: "tags"}}}, "no order"},
		{data.Query{Entity: "item", Cursor: "garbage"}, "cursor"},
	} {
		if _, err := data.Find(args(), data.Deps{Projections: f.p}, c.q); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v, want %q", c.q, err, c.want)
		}
	}
	// An index over a field without an order is refused
	f.rejected("entity.addIndex", "item", map[string]any{"fields": []string{"tags", "level", "tags"}}, "more than one list-like")
}

func eventually(t *testing.T, check func() string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		msg := check()
		if msg == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
