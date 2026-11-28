package recordsnodes_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
)

// defineGraph is a server action of owner's app that makes a
// user-defined type held by a database record and adds a field to it,
// or, given an entity type, only adds the field.
func defineGraph(owner, existing string) cognition.Graph {
	g := cognition.Graph{ID: "define", Kind: livenodes.KindAction, Owner: owner,
		Nodes: []cognition.Node{
			node("event", cognition.EventCall, nil),
			node("parent", "data.expression", map[string]any{"expr": `{entity: "database", id: "db1"}`, "type": "json"}),
			node("fields", "data.expression", map[string]any{"expr": `[{name: "Name", type: "text"}]`, "type": "json"}),
			node("field", "data.expression", map[string]any{"expr": `{name: "Status", type: "enum", options: [{id: "todo", name: "To do"}, {id: "done", name: "Done"}]}`, "type": "json"}),
			node("create", "records.createEntityType", map[string]any{"name": "Projects"}),
			node("add", "records.addEntityField", nil),
			node("ret", "flow.return", map[string]any{"outputs": []any{map[string]any{"id": "entity", "type": "string"}}}),
			node("bad", "flow.return", map[string]any{"outputs": []any{map[string]any{"id": "message", "type": "string"}}}),
		},
	}
	if existing == "" {
		g.Edges = edges("event.then -> create.then", "create.ok -> add.then", "add.ok -> ret.then", "add.rejected -> bad.then", "create.rejected -> bad.then",
			"parent.value -> create.parent", "fields.value -> create.fields", "create.entity -> add.entity", "field.value -> add.field",
			"create.entity -> ret.entity", "add.message -> bad.message")
		return g
	}
	keep := g.Nodes[:0]
	for _, n := range g.Nodes {
		if n.ID != "create" && n.ID != "parent" && n.ID != "fields" {
			keep = append(keep, n)
		}
	}
	g.Nodes = append(keep, node("id", "data.expression", map[string]any{"expr": `"` + existing + `"`, "type": "string"}))
	g.Edges = edges("event.then -> add.then", "add.ok -> ret.then", "add.rejected -> bad.then",
		"id.value -> add.entity", "field.value -> add.field", "id.value -> ret.entity", "add.message -> bad.message")
	return g
}

// A running app defines its users' entity types (forge 35c-1): the type
// is made, a field added, both published at once, records follow; another
// app's graph and an app's own type are refused.
func TestDefineEntityTypesAtRuntime(t *testing.T) {
	f := setup(t)
	run := func(g cognition.Graph) cognition.Values {
		t.Helper()
		out, err := f.eng.Run(engine.RunArgs{ServiceArgs: args(), Graph: &g, Event: cognition.EventCall})
		if err != nil {
			t.Fatal(err)
		}
		return out.Outputs
	}
	out := run(defineGraph("app:docs", ""))
	id, _ := out["entity"].(string)
	if !strings.HasPrefix(id, "ud_") {
		t.Fatalf("made %v", out)
	}
	if err := f.p.Settle(context.Background(), features.LocalTenantID, entities.PublishedProjector); err != nil {
		t.Fatal(err)
	}
	v, ok, _ := f.p.Get(context.Background(), features.LocalTenantID, entities.PublishedProjector, entities.TargetType+"."+id)
	var e records.EntityType
	if !ok || json.Unmarshal(v.Value, &e) != nil {
		t.Fatal("not published")
	}
	if !e.UserDefined || e.Owner != "app:docs" || e.Parent == nil || e.Parent.ID != "db1" || e.Version != 2 || len(e.Visible()) != 2 {
		t.Fatalf("published %+v", e)
	}
	// Records follow the new column
	f.ok("record.create", "p1", map[string]any{"entity": id, "values": map[string]any{"name": "Docs", "status": "todo"}})
	if err := f.p.Settle(context.Background(), features.LocalTenantID, data.Projector); err != nil {
		t.Fatal(err)
	}
	res, err := data.Find(args(), f.deps, data.Query{Entity: id})
	if err != nil || len(res.Records) != 1 || string(res.Records[0]["status"]) != `"todo"` {
		t.Fatalf("records %+v %v", res, err)
	}

	// Another app's graph, and an app's own type, are refused
	if out := run(defineGraph("app:other", id)); !strings.Contains(out["message"].(string), "belongs to app:docs") {
		t.Errorf("another app: %v", out)
	}
	if out := run(defineGraph("app:docs", "task")); !strings.Contains(out["message"].(string), "not a user-defined") {
		t.Errorf("an app's own type: %v", out)
	}

	// An import cannot turn it into, or make, a user-defined type
	if res := f.on("entity.import", "smuggled", map[string]any{"entity": map[string]any{"name": "X", "userDefined": true, "parent": map[string]any{"entity": "database", "id": "db1"}, "fields": []any{}}}); res.Common.Code < 300 {
		t.Error("an import made a user-defined type")
	}
	// An anonymous principal may not define the database's types
	anon := features.ServiceArgs{Ctx: context.Background(), Principal: features.AnonymousPrincipal()}
	raw, _ := json.Marshal(map[string]any{"field": "status"})
	if res := f.bus.Execute(bus.Request{ServiceArgs: anon, ID: "anon-1", Type: "entity.removeField", Target: id, Args: raw}); res.Common.Code < 300 {
		t.Error("an anonymous principal changed a user-defined type")
	}
}
