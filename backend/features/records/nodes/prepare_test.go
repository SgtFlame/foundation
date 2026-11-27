package recordsnodes_test

import (
	"encoding/json"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
)

func TestPrepareRows(t *testing.T) {
	f := setup(t)
	f.ok("entity.create", "company", map[string]any{"name": "Company", "titleField": "name", "fields": []any{
		map[string]any{"name": "name", "type": "text", "required": true},
	}})
	res := f.ok("entity.publish", "company", map[string]any{"rev": 1})
	f.wait(entities.PublishedProjector, "entity.company", res.Seq)
	f.ok("entity.create", "contact", map[string]any{"name": "Contact", "titleField": "name", "fields": []any{
		map[string]any{"name": "name", "type": "text", "required": true},
		map[string]any{"name": "email", "type": "text"},
		map[string]any{"name": "age", "type": "number"},
		map[string]any{"name": "company", "type": "relation", "relation": map[string]any{"entity": "company"}},
	}})
	res = f.ok("entity.publish", "contact", map[string]any{"rev": 1})
	f.wait(entities.PublishedProjector, "entity.contact", res.Seq)
	f.ok("record.create", "acme", map[string]any{"entity": "company", "values": map[string]any{"name": "Acme"}})
	f.ok("record.create", "c1", map[string]any{"entity": "contact", "values": map[string]any{"name": "Ada", "email": "ada@example.com"}})
	f.tasks() // settles the records

	rows := []any{
		map[string]any{"name": "Ada L", "email": "ada@example.com", "company": "acme"},     // updates Ada; title ignoring case
		map[string]any{"email": "bob@example.com", "company": "Globex"},                    // created without a name: required
		map[string]any{"name": "Cy", "email": "cy@example.com", "age": "twelve"},           // a bad number
		map[string]any{"email": "ada@example.com", "age": 40},                              // matched, so no name needed
		map[string]any{"name": "Di", "email": "di@example.com", "company": "acme", "x": 1}, // an unknown field
	}
	raw, _ := json.Marshal(rows)
	g := cognition.Graph{ID: "on-prepare", Kind: uinodes.KindHandler,
		Nodes: []cognition.Node{
			node("event", uinodes.OnEvent, nil),
			node("rows", "data.expression", map[string]any{"expr": string(raw), "type": "json"}),
			node("match", "data.expression", map[string]any{"expr": "\"email\"", "type": "string"}),
			node("prep", "records.prepareRows", map[string]any{"entity": "contact", "resolve": true}),
			node("mk", "data.expression", map[string]any{"expr": "{ok: ok, okIndex: okIndex, problems: problems, failed: failed, missing: missing, matched: matched}", "type": "json"}),
			node("s", "ui.setState", map[string]any{"path": "out"}),
		},
		Edges: edges("event.then -> s.then", "rows.value -> prep.rows", "match.value -> prep.match",
			"prep.ok -> mk.ok", "prep.okIndex -> mk.okIndex", "prep.problems -> mk.problems", "prep.failed -> mk.failed",
			"prep.missing -> mk.missing", "prep.matched -> mk.matched", "mk.value -> s.value"),
	}
	h := &host{state: map[string]any{}}
	if _, err := f.eng.Run(engine.RunArgs{ServiceArgs: args(), Graph: &g, Event: uinodes.OnEvent, Host: h}); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(h.state["out"])
	var got struct {
		OK       []map[string]any
		OKIndex  []int
		Problems []struct {
			Row   int
			Field string
		}
		Failed  []int
		Missing []struct {
			Field, Entity, TitleField string
			Titles                    []string
		}
		Matched int
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	if len(got.OK) != 2 || got.OKIndex[0] != 0 || got.OKIndex[1] != 3 || got.OK[0]["company"] != "acme" {
		t.Errorf("ok rows: %s", out)
	}
	if got.Matched != 2 {
		t.Errorf("matched %d, want 2: %s", got.Matched, out)
	}
	want := map[int]string{1: "name", 2: "age", 4: "x"}
	for _, p := range got.Problems {
		if want[p.Row] == p.Field {
			delete(want, p.Row)
		}
	}
	if len(want) > 0 || len(got.Failed) != 3 {
		t.Errorf("problems missing %v: %s", want, out)
	}
	if len(got.Missing) != 1 || got.Missing[0].Field != "company" || got.Missing[0].TitleField != "name" || len(got.Missing[0].Titles) != 1 || got.Missing[0].Titles[0] != "Globex" {
		t.Errorf("missing: %s", out)
	}
}
