package data_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/records/data"
)

func (f *fixture) contacts() {
	f.ok("entity.create", "contact", map[string]any{"name": "Contact", "titleField": "name", "fields": []any{
		map[string]any{"name": "name", "type": "text", "required": true},
		map[string]any{"name": "email", "type": "text"},
		map[string]any{"name": "phone", "type": "text"},
		map[string]any{"name": "number", "type": "autoNumber"},
	}, "indexes": []any{map[string]any{"id": "byEmail", "fields": []any{"email"}}}})
	f.publish("contact", 1)
}

func (f *fixture) all(entity string) map[string]data.Record {
	f.t.Helper()
	res, err := data.Find(args(), *f.deps, data.Query{Entity: entity})
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]data.Record{}
	for _, r := range res.Records {
		var email string
		_ = json.Unmarshal(r["email"], &email)
		out[email] = r
	}
	return out
}

// record.upsert (forge 40c) creates rows, or updates the record holding a
// row's match value, as one batch: a refusal names its rows and commits
// nothing, and a retried command commits nothing twice.
func TestUpsert(t *testing.T) {
	f := setup(t)
	f.contacts()
	res := f.ok(data.UpsertID, "", map[string]any{"entity": "contact", "match": "email", "rows": []any{
		map[string]any{"name": "Ada", "email": "ada@example.com"},
		map[string]any{"name": "Bob", "email": "bob@example.com", "phone": "1"},
		map[string]any{"name": "Bobby", "email": "bob@example.com"}, // merged into the row above, the later winning
	}})
	f.settle()
	got := f.all("contact")
	if len(got) != 2 || str(got["bob@example.com"]["name"]) != `"Bobby"` || str(got["bob@example.com"]["phone"]) != `"1"` {
		t.Fatalf("first import: %v", got)
	}
	if str(got["ada@example.com"]["number"]) == str(got["bob@example.com"]["number"]) {
		t.Fatalf("autoNumbers repeat: %v", got)
	}
	adaID := str(got["ada@example.com"]["_id"])

	// A second import updates Ada in place and creates Cyd
	res = f.ok(data.UpsertID, "", map[string]any{"entity": "contact", "match": "email", "rows": []any{
		map[string]any{"name": "Ada Lovelace", "email": "ada@example.com"},
		map[string]any{"name": "Cyd", "email": "cyd@example.com"},
	}})
	f.settle()
	got = f.all("contact")
	if len(got) != 3 || str(got["ada@example.com"]["_id"]) != adaID || str(got["ada@example.com"]["name"]) != `"Ada Lovelace"` {
		t.Fatalf("second import: %v", got)
	}

	// The same command again (a retry) commits nothing
	raw, _ := json.Marshal(map[string]any{"entity": "contact", "rows": []any{map[string]any{"name": "Dee", "email": "dee@example.com"}}})
	for range 2 {
		if r := f.bus.Execute(bus.Request{ServiceArgs: args(), ID: "import-batch-1", Type: data.UpsertID, Args: raw}); r.Common.Code >= 300 {
			t.Fatalf("retried import: %+v", r)
		}
	}
	f.settle()
	if got = f.all("contact"); len(got) != 4 {
		t.Fatalf("a retried command doubled: %d", len(got))
	}

	// A row missing a required value refuses the batch by row, committing nothing
	res = f.rejected(data.UpsertID, "", map[string]any{"entity": "contact", "match": "email", "rows": []any{
		map[string]any{"name": "Eve", "email": "eve@example.com"},
		map[string]any{"email": "fay@example.com"},
		map[string]any{"name": "Gus"},
	}}, "args.rows.1.name")
	if !strings.Contains(fmt.Sprint(res.Problems), "args.rows.2.email") {
		t.Fatalf("the row without a match value: %+v", res.Problems)
	}
	if got = f.all("contact"); len(got) != 4 {
		t.Fatalf("a refused batch committed: %d", len(got))
	}

	// values alone is one row; match on a list or unknown field is refused
	res = f.ok(data.UpsertID, "", map[string]any{"entity": "contact", "match": "email", "values": map[string]any{"name": "Ada L.", "email": "ada@example.com"}})
	f.settle()
	if got = f.all("contact"); str(got["ada@example.com"]["name"]) != `"Ada L."` {
		t.Fatalf("one row: %v", got["ada@example.com"])
	}
	f.rejected(data.UpsertID, "", map[string]any{"entity": "contact", "match": "nope", "rows": []any{map[string]any{"name": "X"}}}, "no field")
	rows := make([]any, 501)
	for i := range rows {
		rows[i] = map[string]any{"name": fmt.Sprint(i)}
	}
	f.rejected(data.UpsertID, "", map[string]any{"entity": "contact", "rows": rows}, "rows")
}
