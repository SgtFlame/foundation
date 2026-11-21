package data_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/files"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
	recordimpl "github.com/SgtFlame/foundation/backend/features/records/services/recordservice/impl"
)

// get reads a record of entity once its commit at seq is projected.
func (f *fixture) get(entity, id string, seq uint64) data.Record {
	f.t.Helper()
	v := f.wait(data.Projector, data.Key(entity, id), seq)
	var r data.Record
	_ = json.Unmarshal(v.Value, &r)
	return r
}

func TestRichText(t *testing.T) {
	f := setup(t)
	f.ok("entity.create", "page", map[string]any{"name": "Page", "fields": []any{
		map[string]any{"name": "title", "type": "text"},
		map[string]any{"name": "content", "type": "richText"},
	}})
	f.publish("page", 1)
	id := f.ok("record.create", "", map[string]any{"entity": "page", "values": map[string]any{"title": "Plan"}}).Target

	doc := `{"type":"doc","content":[{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Goals"}]},` +
		`{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Ship ","marks":[{"type":"bold"}]},{"type":"text","text":"3j"}]}]}]}]}`
	res := f.ok("record.setRichText", id, map[string]any{"entity": "page", "field": "content", "content": json.RawMessage(doc)})
	r := f.get("page", id, res.Seq)
	var want, got any
	_ = json.Unmarshal([]byte(doc), &want)
	_ = json.Unmarshal(r["content"], &got)
	if fmt.Sprint(want) != fmt.Sprint(got) {
		t.Errorf("the blocks read back differ:\n%s\n%s", doc, r["content"])
	}
	if text := records.PlainText(r["content"]); text != "Goals\nShip 3j" {
		t.Errorf("plain text = %q", text)
	}
	// The same document again changes nothing
	if again := f.ok("record.setRichText", id, map[string]any{"entity": "page", "field": "content", "content": json.RawMessage(doc)}); again.Seq != res.Seq {
		t.Errorf("saving the same document committed %d", again.Seq)
	}
	f.rejected("record.setRichText", id, map[string]any{"entity": "page", "field": "content", "content": map[string]any{"type": "paragraph"}}, "is not a document")
	f.rejected("record.setRichText", id, map[string]any{"entity": "page", "field": "content", "content": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "text"}}}}, "needs its text")
	f.rejected("record.setRichText", id, map[string]any{"entity": "page", "field": "title", "content": json.RawMessage(doc)}, "not rich text")
	// record.update takes rich text too, checked the same way
	f.rejected("record.update", id, map[string]any{"entity": "page", "set": map[string]any{"content": "plain"}}, "is not a document")

	// A text field turned rich text keeps its text, a paragraph per line
	f.ok("entity.updateField", "page", map[string]any{"field": "title", "type": "richText"})
	f.publish("page", 3)
	deadline := time.Now().Add(5 * time.Second)
	for {
		v, _, _ := f.p.Get(context.Background(), features.LocalTenantID, data.Projector, data.Key("page", id))
		var r data.Record
		_ = json.Unmarshal(v.Value, &r)
		if records.PlainText(r["title"]) == "Plan" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the title was not converted: %s", r["title"])
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestFormulasAndMetadataFields(t *testing.T) {
	f := setup(t)
	f.ok("entity.create", "line", map[string]any{"name": "Line", "fields": []any{
		map[string]any{"name": "item", "type": "text"},
		map[string]any{"name": "quantity", "type": "number"},
		map[string]any{"name": "price", "type": "decimal"},
		map[string]any{"name": "total", "type": "formula", "formula": "quantity * price"},
		map[string]any{"name": "big", "type": "formula", "formula": "total > 100"},
		map[string]any{"name": "created", "type": "createdAt"},
		map[string]any{"name": "updated", "type": "updatedAt"},
		map[string]any{"name": "author", "type": "createdBy"},
	}})
	// A formula that does not type check is refused as it is defined
	f.rejected("entity.addField", "line", map[string]any{"name": "bad", "type": "formula", "formula": "item * 2"}, "does not type check")
	f.rejected("entity.addField", "line", map[string]any{"name": "later", "type": "formula", "formula": "nothing + 1"}, "does not type check")
	f.rejected("entity.addField", "line", map[string]any{"name": "n", "type": "number", "searchable": true}, "not searched as text")
	f.publish("line", 1)

	res := f.ok("record.create", "", map[string]any{"entity": "line", "values": map[string]any{"item": "pen", "quantity": 3, "price": "1.50"}})
	id := res.Target
	r := f.get("line", id, res.Seq)
	if str(r["total"]) != "4.5" || str(r["big"]) != "false" {
		t.Errorf("total, big = %s, %s", r["total"], r["big"])
	}
	if str(r["created"]) != str(r[data.MetaCreatedAt]) || str(r["updated"]) != str(r[data.MetaUpdatedAt]) || str(r["author"]) != `"1"` {
		t.Errorf("metadata fields = %s %s %s, record's %s %s %s", r["created"], r["updated"], r["author"], r[data.MetaCreatedAt], r[data.MetaUpdatedAt], r[data.MetaCreatedBy])
	}
	// A computed field cannot be set
	f.rejected("record.update", id, map[string]any{"entity": "line", "set": map[string]any{"total": 9}}, "computed and cannot be set")

	// Its value changes when a field it reads changes
	time.Sleep(5 * time.Millisecond)
	res = f.ok("record.update", id, map[string]any{"entity": "line", "set": map[string]any{"quantity": 100}})
	r = f.get("line", id, res.Seq)
	if str(r["total"]) != "150" || str(r["big"]) != "true" {
		t.Errorf("after the change, total, big = %s, %s", r["total"], r["big"])
	}
	if str(r["updated"]) != str(r[data.MetaUpdatedAt]) || str(r["created"]) == str(r["updated"]) {
		t.Errorf("updated = %s, record's %s, created %s", r["updated"], r[data.MetaUpdatedAt], r["created"])
	}
	// A field it reads cleared clears it
	res = f.ok("record.update", id, map[string]any{"entity": "line", "set": map[string]any{"price": nil}})
	if r = f.get("line", id, res.Seq); r["total"] != nil {
		t.Errorf("total without a price = %s", r["total"])
	}
	f.ok("record.update", id, map[string]any{"entity": "line", "set": map[string]any{"price": "2"}})

	// Formulas are sortable once published, as numbers
	e, _ := data.Entity(args(), f.p, "line")
	total, _, _ := e.FieldByID("total")
	if total.Returns != "number" || !records.Sortable(total) {
		t.Errorf("total returns %q, sortable %v", total.Returns, records.Sortable(total))
	}

	// Publishing a changed formula writes every record's value again
	f.ok("entity.updateField", "line", map[string]any{"field": "total", "formula": "quantity * price * 2"})
	f.publish("line", 3)
	deadline := time.Now().Add(5 * time.Second)
	for {
		v, _, _ := f.p.Get(context.Background(), features.LocalTenantID, data.Projector, data.Key("line", id))
		var r data.Record
		_ = json.Unmarshal(v.Value, &r)
		if str(r["total"]) == "400" {
			if str(r["updated"]) != str(r[data.MetaUpdatedAt]) {
				t.Errorf("a recompute moved updated away from _updatedAt")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the republished formula was not recomputed: total = %s", r["total"])
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Entity type ids starting with '_' are the records' own
	f.rejected("entity.create", "_counter", map[string]any{"name": "Counter"}, "starts with '_'")
}

func TestConcurrentAutoNumbersHaveNoGaps(t *testing.T) {
	f := setup(t)
	f.ok("entity.create", "ticket", map[string]any{"name": "Ticket", "fields": []any{
		map[string]any{"name": "title", "type": "text"},
		map[string]any{"name": "no", "type": "autoNumber"},
	}})
	f.publish("ticket", 1)
	// Creates at once through one server's bus, which holds the counter
	// while each decides
	const n = 60
	var wg sync.WaitGroup
	var mu sync.Mutex
	var ids []string
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := f.on(f.bus, "record.create", "", map[string]any{"entity": "ticket", "values": map[string]any{"title": fmt.Sprint(i)}})
			if res.Common.Code >= 300 {
				t.Errorf("create %d: %s", i, res.Common.Message)
				return
			}
			mu.Lock()
			ids = append(ids, res.Target)
			mu.Unlock()
		}()
	}
	wg.Wait()
	f.settle()
	var numbers []int
	for _, id := range ids {
		v, _, _ := f.p.Get(context.Background(), features.LocalTenantID, data.Projector, data.Key("ticket", id))
		var r data.Record
		_ = json.Unmarshal(v.Value, &r)
		k, err := strconv.Atoi(str(r["no"]))
		if err != nil {
			t.Fatalf("ticket %s has no number: %s", id, r["no"])
		}
		numbers = append(numbers, k)
	}
	slices.Sort(numbers)
	for i, k := range numbers {
		if k != i+1 {
			t.Fatalf("numbers are not 1..%d without gaps: %v", n, numbers)
		}
	}
	// Listing the type's records shows no counter
	if l := f.svc.List(recordservice.ListArgs{ServiceArgs: args(), Entity: "ticket"}); len(l.Records) != n {
		t.Errorf("list has %d records, want %d", len(l.Records), n)
	}
}

func TestRollupsEqualAQuerysAggregate(t *testing.T) {
	f := setup(t)
	f.ok("entity.create", "item", map[string]any{"name": "Item", "fields": []any{
		map[string]any{"name": "points", "type": "number"},
		map[string]any{"name": "cost", "type": "decimal"},
	}})
	f.publish("item", 1)
	f.ok("entity.create", "project", map[string]any{"name": "Project", "fields": []any{
		map[string]any{"name": "name", "type": "text"},
		map[string]any{"name": "items", "type": "relation", "relation": map[string]any{"entity": "item", "many": true}},
		map[string]any{"name": "lead", "type": "relation", "relation": map[string]any{"entity": "item"}},
		map[string]any{"name": "points", "type": "rollup", "rollup": map[string]any{"relation": "items", "field": "points", "aggregate": "sum"}},
		map[string]any{"name": "count", "type": "rollup", "rollup": map[string]any{"relation": "items", "aggregate": "count"}},
		map[string]any{"name": "cost", "type": "rollup", "rollup": map[string]any{"relation": "items", "field": "cost", "aggregate": "sum"}},
		map[string]any{"name": "most", "type": "rollup", "rollup": map[string]any{"relation": "items", "field": "points", "aggregate": "max"}},
		map[string]any{"id": "leadCost", "name": "Lead cost", "type": "rollup", "rollup": map[string]any{"relation": "lead", "field": "cost", "aggregate": "min"}},
		map[string]any{"name": "double", "type": "formula", "formula": "points * 2"},
	}})
	f.publish("project", 1)
	e, _ := data.Entity(args(), f.p, "project")
	if cost, _, _ := e.FieldByID("cost"); cost.Returns != "decimal" || cost.Scale == nil || *cost.Scale != 2 {
		t.Errorf("cost's rollup returns %q scale %v", cost.Returns, cost.Scale)
	}

	rnd := rand.New(rand.NewPCG(3, 9))
	var items, projects []string
	value := func() map[string]any {
		return map[string]any{"points": rnd.IntN(20), "cost": fmt.Sprintf("%d.%02d", rnd.IntN(50), rnd.IntN(100))}
	}
	for range 30 {
		items = append(items, f.ok("record.create", "", map[string]any{"entity": "item", "values": value()}).Target)
	}
	pick := func() []string {
		var out []string
		for _, id := range items {
			if rnd.IntN(4) == 0 {
				out = append(out, id)
			}
		}
		return out
	}
	for i := range 8 {
		projects = append(projects, f.ok("record.create", "", map[string]any{"entity": "project", "values": map[string]any{"name": fmt.Sprint(i), "items": pick(), "lead": items[rnd.IntN(len(items))]}}).Target)
	}
	for range 120 {
		switch rnd.IntN(5) {
		case 0, 1:
			f.ok("record.update", items[rnd.IntN(len(items))], map[string]any{"entity": "item", "set": value()})
		case 2:
			p := projects[rnd.IntN(len(projects))]
			f.ok("record.addTo", p, map[string]any{"entity": "project", "field": "items", "items": []string{items[rnd.IntN(len(items))]}})
		case 3:
			p := projects[rnd.IntN(len(projects))]
			f.ok("record.removeFrom", p, map[string]any{"entity": "project", "field": "items", "items": []string{items[rnd.IntN(len(items))]}})
		case 4:
			if rnd.IntN(3) == 0 {
				i := rnd.IntN(len(items))
				f.ok("record.delete", items[i], map[string]any{"entity": "item"})
				items = slices.Delete(items, i, i+1)
			} else {
				items = append(items, f.ok("record.create", "", map[string]any{"entity": "item", "values": value()}).Target)
			}
		}
	}

	// The rollups settle on what a query aggregates over the same items
	check := func() []string {
		var wrong []string
		for _, id := range projects {
			v, _, _ := f.p.Get(context.Background(), features.LocalTenantID, data.Projector, data.Key("project", id))
			var p data.Record
			_ = json.Unmarshal(v.Value, &p)
			var ids []string
			_ = json.Unmarshal(p["items"], &ids)
			sum, err := data.Summarize(args(), *f.deps, data.Aggregation{Entity: "item", Filter: &data.Filter{Field: data.MetaID, Op: "in", Value: raw(ids)},
				Aggregates: []data.Aggregate{{Op: "sum", Field: "points", As: "points"}, {Op: "count", As: "count"}, {Op: "sum", Field: "cost", As: "cost"}, {Op: "max", Field: "points", As: "most"}}})
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]json.RawMessage{"points": raw(0), "count": raw(0), "cost": raw("0.00"), "most": nil}
			if len(sum.Groups) > 0 {
				want = sum.Groups[0].Values
			}
			for _, k := range []string{"points", "count", "cost", "most"} {
				if orEmpty(p[k]) != orEmpty(want[k]) {
					wrong = append(wrong, fmt.Sprintf("%s %s: %s, the query's %s", id, k, p[k], want[k]))
				}
			}
			if pts, _ := strconv.ParseFloat(str(p["points"]), 64); str(p["double"]) != fmt.Sprint(pts*2) {
				wrong = append(wrong, fmt.Sprintf("%s double: %s for points %s", id, p["double"], p["points"]))
			}
			var lead string
			_ = json.Unmarshal(p["lead"], &lead)
			lv, ok, _ := f.p.Get(context.Background(), features.LocalTenantID, data.Projector, data.Key("item", lead))
			var l data.Record
			if ok {
				_ = json.Unmarshal(lv.Value, &l)
			}
			if orEmpty(p["leadCost"]) != orEmpty(l["cost"]) {
				wrong = append(wrong, fmt.Sprintf("%s leadCost: %s, the lead's %s", id, p["leadCost"], l["cost"]))
			}
		}
		return wrong
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		wrong := check()
		if len(wrong) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rollups differ from the query's aggregates:\n%s", strings.Join(wrong, "\n"))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func raw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func TestFilesAreServedToReadersOnly(t *testing.T) {
	f := setup(t)
	store := files.New(f.js)
	f.deps.Files = store
	svc := recordimpl.New(recordimpl.WithRecords(f.bus, f.subs, *f.deps), recordimpl.WithFiles(store))
	f.ok("entity.create", "doc", map[string]any{"name": "Doc", "fields": []any{
		map[string]any{"name": "title", "type": "text"},
		map[string]any{"name": "owner", "type": "relation", "relation": map[string]any{"entity": "user"}},
		map[string]any{"name": "shared", "type": "bool"},
		map[string]any{"name": "attachment", "type": "file"},
	}})
	f.ok("entity.setAccess", "doc", map[string]any{"access": records.Access{Read: "record.owner == principal.user || record.shared == true"}})
	f.publish("doc", 2)

	gdb, err := db.Open(db.WithPath(t.TempDir() + "/app.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, authz.Models()...); err != nil {
		t.Fatal(err)
	}
	reg := extensions.NewRegistry()
	authz.Contribute(reg)
	authz.ResourceTypes.Register(reg, authz.ResourceType{Descriptor: extensions.Descriptor{ID: "tenant"}, Parent: "app", Actions: []authz.Action{{Name: "read", Class: authz.ClassRead}}})
	records.ContributeAccess(reg)
	pol := authz.NewPolicy(gdb, reg)
	ctx := context.Background()
	for _, user := range []uint64{5, 6} {
		if _, err := pol.Grant(ctx, nil, authz.GrantArgs{UserID: user, RoleID: authz.RoleEditor, Resource: "tenant:1"}); err != nil {
			t.Fatal(err)
		}
	}
	authz.Install(pol)
	t.Cleanup(func() { authz.Install(nil) })

	up := svc.Upload(recordservice.UploadArgs{ServiceArgs: as(5), Entity: "doc", Name: "notes.txt", ContentType: "text/plain", Content: strings.NewReader("the notes")})
	if up.Common.Code != 200 || up.File.Size != 9 {
		t.Fatalf("upload: %d %s %+v", up.Common.Code, up.Common.Message, up.File)
	}
	// Someone without a grant cannot upload; a file over the limit is refused
	if no := svc.Upload(recordservice.UploadArgs{ServiceArgs: as(7), Entity: "doc", Name: "x.txt", Content: strings.NewReader("x")}); no.Common.Code != authz.Status {
		t.Errorf("a principal without a grant uploaded: %d", no.Common.Code)
	}
	f.deps.Settings = nil
	res := f.by(f.bus, 5, "record.create", "", map[string]any{"entity": "doc", "values": map[string]any{"title": "mine", "owner": "5", "attachment": up.File}})
	if res.Common.Code >= 300 {
		t.Fatalf("create with the file: %s %+v", res.Common.Message, res.Problems)
	}
	doc := res.Target
	f.get("doc", doc, res.Seq)

	read := func(user uint64, file string) (int, string) {
		r := svc.OpenFile(recordservice.OpenFileArgs{ServiceArgs: as(user), Entity: "doc", Record: doc, Field: "attachment", File: file})
		if r.Content == nil {
			return int(r.Common.Code), ""
		}
		defer r.Content.Close()
		b, _ := io.ReadAll(r.Content)
		return int(r.Common.Code), string(b)
	}
	if code, body := read(5, up.File.ID); code != 200 || body != "the notes" {
		t.Errorf("the owner read %d %q", code, body)
	}
	// User 6 may read docs, but the row rule hides this one
	if code, _ := read(6, up.File.ID); code != 404 {
		t.Errorf("a user the row rule hides it from got %d", code)
	}
	if code, _ := read(7, up.File.ID); code != authz.Status {
		t.Errorf("a user without a grant got %d", code)
	}
	// Another file id on the same record is not served through it
	other := svc.Upload(recordservice.UploadArgs{ServiceArgs: as(6), Entity: "doc", Name: "theirs.txt", Content: strings.NewReader("theirs")})
	if code, _ := read(5, other.File.ID); code != 404 {
		t.Errorf("a file the record does not hold was served: %d", code)
	}
	// A file someone else uploaded cannot be set on a record, nor one
	// that does not exist
	if res := f.by(f.bus, 6, "record.create", "", map[string]any{"entity": "doc", "values": map[string]any{"owner": "6", "attachment": up.File}}); res.Common.Code != 422 || !strings.Contains(fmt.Sprint(res.Problems), "you uploaded") {
		t.Errorf("user 6 set user 5's file: %d %+v", res.Common.Code, res.Problems)
	}
	missing := up.File
	missing.ID = "123"
	if res := f.by(f.bus, 5, "record.update", doc, map[string]any{"entity": "doc", "set": map[string]any{"attachment": missing}}); res.Common.Code != 422 {
		t.Errorf("a missing file was set: %d %+v", res.Common.Code, res.Problems)
	}
	// Shared, user 6 reads it too
	res = f.by(f.bus, 5, "record.update", doc, map[string]any{"entity": "doc", "set": map[string]any{"shared": true}})
	f.get("doc", doc, res.Seq)
	if code, body := read(6, up.File.ID); code != 200 || body != "the notes" {
		t.Errorf("once shared, user 6 read %d %q", code, body)
	}
	if u := files.URL("doc", doc, "attachment", up.File.ID); u != "/api/v1/records/files/doc/"+doc+"/attachment/"+up.File.ID {
		t.Errorf("URL = %s", u)
	}
}
