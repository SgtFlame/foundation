package installs_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/apps/services/appservice"
	appimpl "github.com/SgtFlame/foundation/backend/features/apps/services/appservice/impl"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
)

// in are the test's ServiceArgs in another tenant.
func in(tenant uint64) features.ServiceArgs {
	a := args()
	a.Principal.TenantID = tenant
	return a
}

func (f *fixture) okIn(tenant uint64, typ, target string, a any) bus.Result {
	f.t.Helper()
	raw, _ := json.Marshal(a)
	res := f.bus.Execute(bus.Request{ServiceArgs: in(tenant), ID: fmt.Sprintf("c%d", f.n.Add(1)), Type: typ, Target: target, Args: raw})
	if res.Common.Code >= 300 {
		f.t.Fatalf("%s: %s %+v", typ, res.Common.Message, res.Problems)
	}
	return res
}

func (f *fixture) waitIn(tenant uint64, projector, key string, seq uint64) live.Projected {
	f.t.Helper()
	v, err := f.p.WaitFor(in(tenant).Context(), tenant, projector, key, seq, 3*time.Second)
	if err != nil || v.Seq < seq {
		f.t.Fatalf("%s %s did not reach %d in tenant %d: %v", projector, key, seq, tenant, err)
	}
	return v
}

// publishIn publishes a part of a tenant at its draft's rev.
func (f *fixture) publishIn(tenant uint64, typ, id string, seq uint64) {
	f.t.Helper()
	k := map[string][3]string{
		"prefab": {prefabs.DraftProjector, prefabs.PublishedProjector, "prefab.publish"},
		"graph":  {drafts.DraftProjector, drafts.PublishedProjector, "bp.publish"},
		"entity": {entities.DraftProjector, entities.PublishedProjector, "entity.publish"},
	}[typ]
	var d struct{ Rev int }
	_ = json.Unmarshal(f.waitIn(tenant, k[0], typ+"."+id, seq).Value, &d)
	res := f.okIn(tenant, k[2], id, map[string]any{"rev": d.Rev})
	f.waitIn(tenant, k[1], typ+"."+id, res.Seq)
}

func (f *fixture) service() appservice.Service {
	return appimpl.New(appimpl.WithDeps(func() appimpl.Deps {
		return appimpl.Deps{Projections: f.p, DB: f.db, Bus: f.bus, Subscriptions: f.subs, Registry: f.reg}
	}))
}

// note is a tenant-library prefab; its text differs per tenant in tests.
func note(text string) map[string]any {
	return map[string]any{"name": "Note", "props": map[string]any{"text": map[string]any{"type": "string", "default": text}},
		"template": map[string]any{"type": "mui.text", "key": "root", "bind": map[string]any{"text": "props.text"}}}
}

// customer is a tenant's shared entity type.
var customer = map[string]any{"name": "Customer", "owner": "tenant", "fields": []any{map[string]any{"id": "name", "name": "name", "type": "text"}}}

// exportable makes the Tasks app in tenant 1, with a tenant prefab (Note,
// rendered by My tasks) and a tenant entity type (Customer, which a task
// relates to), roles, settings and two tasks; published, not installed.
func (f *fixture) exportable() {
	own := "app:tasks"
	f.publishIn(1, "entity", "customer", f.ok("entity.create", "customer", customer).Seq)
	f.publishIn(1, "entity", "task", f.ok("entity.create", "task", map[string]any{"name": "Task", "owner": own, "fields": []any{
		map[string]any{"id": "title", "name": "title", "type": "text"}, map[string]any{"id": "assignee", "name": "assignee", "type": "text"},
		map[string]any{"id": "customer", "name": "customer", "type": "relation", "relation": map[string]any{"entity": "customer"}}}}).Seq)
	f.publishIn(1, "prefab", "note", f.ok("prefab.import", "note", map[string]any{"prefab": note("A note")}).Seq)
	f.publishIn(1, "prefab", "myTasks", f.ok("prefab.import", "myTasks", map[string]any{"prefab": map[string]any{"name": "My tasks", "owner": own,
		"template": map[string]any{"type": "mui.stack", "key": "root", "children": []any{
			map[string]any{"type": "prefab.note", "key": "note", "props": map[string]any{"text": "Mine"}}}}}}).Seq)
	f.publishIn(1, "prefab", "taskDetail", f.ok("prefab.import", "taskDetail", map[string]any{"prefab": map[string]any{"name": "Task detail", "owner": own,
		"props":    map[string]any{"id": map[string]any{"type": "string", "required": true}},
		"template": map[string]any{"type": "mui.text", "key": "id", "bind": map[string]any{"text": `="Task " + props.id`}}}}).Seq)
	f.publishGraph("cmd-tasks-task-reassign", reassign(""))
	f.publishGraph("qry-tasks-myTasks", myTasks(1))

	f.ok("app.create", "", map[string]any{"name": "Tasks", "icon": "checklist"})
	f.ok("app.addEntity", "tasks", map[string]any{"entity": "task"})
	f.ok("app.addPage", "tasks", map[string]any{"route": "/mine", "prefab": "myTasks", "title": "My tasks"})
	f.ok("app.addPage", "tasks", map[string]any{"route": "/task/:id", "prefab": "taskDetail"})
	f.ok("app.setNav", "tasks", map[string]any{"nav": []any{map[string]any{"title": "My tasks", "route": "/mine", "icon": "person"}}})
	f.ok("app.addEditorKind", "tasks", map[string]any{"kind": "task", "page": "/task/:id", "title": "{record.title}"})
	f.ok("app.addCommand", "tasks", map[string]any{"type": "task.reassign", "graph": "cmd-tasks-task-reassign", "action": "entity:task.update"})
	f.ok("app.addQuery", "tasks", map[string]any{"name": "myTasks", "graph": "qry-tasks-myTasks"})
	f.ok("app.addRole", "tasks", map[string]any{"id": "manager", "title": "Manager", "actions": []string{"entity:task.*"}, "installer": true})
	f.ok("app.addRole", "tasks", map[string]any{"id": "member", "title": "Member", "actions": []string{"entity:task.read", "entity:task.update"},
		"rowRules": map[string]any{"task": "record.assignee == principal.user"}})
	f.ok("app.setSettingsSchema", "tasks", map[string]any{"schema": map[string]any{"type": "object", "properties": map[string]any{"defaultDueDays": map[string]any{"type": "integer"}}},
		"defaults": map[string]any{"defaultDueDays": 7}})
	seq := f.ok("app.setInfo", "tasks", map[string]any{"requires": map[string]any{"primitives": []string{"mui"}}}).Seq
	f.publish(seq)
	f.ok("record.create", "t1", map[string]any{"entity": "task", "values": map[string]any{"title": "Write docs", "assignee": "1"}})
	f.ok("record.create", "t2", map[string]any{"entity": "task", "values": map[string]any{"title": "Ship it"}})
}

// export exports Tasks from a tenant, through JSON as a file would carry it.
func (f *fixture) export(tenant uint64, seed bool) apps.Document {
	f.t.Helper()
	resp := f.service().Export(appservice.ExportArgs{ServiceArgs: in(tenant), App: "tasks", Seed: seed})
	if resp.Common.Code != 200 {
		f.t.Fatalf("export: %s %+v", resp.Common.Message, resp.Problems)
	}
	raw, _ := json.Marshal(resp.Document)
	var doc apps.Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		f.t.Fatal(err)
	}
	return doc
}

func ids[T any](parts []T, id func(T) string) []string {
	out := []string{}
	for _, p := range parts {
		out = append(out, id(p))
	}
	return out
}

// comparable is a document's JSON without what differs by when and where
// it was made.
func comparable(t *testing.T, doc apps.Document) string {
	raw, _ := json.Marshal(doc)
	var v any
	_ = json.Unmarshal(raw, &v)
	var strip func(v any)
	strip = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			delete(x, "publishedAt")
			delete(x, "exportedAt")
			for _, c := range x {
				strip(c)
			}
		case []any:
			for _, c := range x {
				strip(c)
			}
		}
	}
	strip(v)
	out, _ := json.MarshalIndent(v, "", " ")
	return string(out)
}

func TestExportImportInstall(t *testing.T) {
	f := setup(t)
	f.exportable()
	doc := f.export(1, true)

	// The document: the app's own parts, the tenant's apart, the primitives
	// and the sample records
	if got := ids(doc.Parts.Prefabs, func(p apps.PrefabPart) string { return p.ID }); !slices.Equal(got, []string{"myTasks", "taskDetail"}) {
		t.Errorf("own prefabs = %v", got)
	}
	if got := ids(doc.Parts.Graphs, func(p apps.GraphPart) string { return p.ID }); !slices.Equal(got, []string{"cmd-tasks-task-reassign", "qry-tasks-myTasks"}) {
		t.Errorf("own graphs = %v", got)
	}
	if got := ids(doc.TenantParts.Prefabs, func(p apps.PrefabPart) string { return p.ID }); !slices.Equal(got, []string{"note"}) {
		t.Errorf("tenant prefabs = %v", got)
	}
	if got := ids(doc.TenantParts.Entities, func(p apps.EntityPart) string { return p.ID }); !slices.Equal(got, []string{"customer"}) {
		t.Errorf("tenant entity types = %v (the task's relation names customer)", got)
	}
	if doc.TenantParts.Prefabs[0].Version != 1 || doc.TenantParts.Prefabs[0].PublishedAt.IsZero() {
		t.Errorf("the note's version and date: %+v", doc.TenantParts.Prefabs[0])
	}
	if !slices.Equal(doc.Primitives.Components, []string{"mui.stack", "mui.text"}) || len(doc.Primitives.Nodes) == 0 {
		t.Errorf("primitives = %+v", doc.Primitives)
	}
	if got := ids(doc.Seed["task"], func(r apps.SeedRecord) string { return r.ID }); !slices.Equal(got, []string{"t1", "t2"}) {
		t.Errorf("seed = %v", got)
	}
	if without := f.export(1, false); without.Seed != nil {
		t.Errorf("seed without asking: %v", without.Seed)
	}

	// Into tenant 3, where the app runs as it does in tenant 1
	resp := f.service().Import(appservice.ImportArgs{ServiceArgs: in(3), Document: doc})
	if !resp.Imported || resp.Version != 1 {
		t.Fatalf("import: %d %s %+v", resp.Common.Code, resp.Common.Message, resp.Problems)
	}
	for _, p := range resp.Plan.Parts {
		if p.Status != appservice.PartNew {
			t.Errorf("%s %s is %s in an empty tenant", p.Type, p.ID, p.Status)
		}
	}
	res := f.okIn(3, "app.install", "tasks", map[string]any{})
	f.waitIn(3, installs.Projector, "appInstall.tasks", res.Seq)
	res = f.ok("app.install", "tasks", map[string]any{})
	f.wait(installs.Projector, "appInstall.tasks", res.Seq)
	src, _ := manifests.GetVersion(in(1), f.p, "tasks", 0)
	dst, ok := manifests.GetVersion(in(3), f.p, "tasks", 0)
	if !ok {
		t.Fatal("no version in tenant 3")
	}
	for name, pair := range map[string][2]any{"pages": {src.Manifest.Pages, dst.Manifest.Pages}, "nav": {src.Manifest.Nav, dst.Manifest.Nav},
		"roles": {src.Manifest.Roles, dst.Manifest.Roles}, "settings": {src.Manifest.Settings, dst.Manifest.Settings},
		"editor kinds": {src.Manifest.EditorKinds, dst.Manifest.EditorKinds}, "commands": {src.Manifest.Commands, dst.Manifest.Commands},
		"prefabs": {slices.Sorted(maps.Keys(src.Prefabs)), slices.Sorted(maps.Keys(dst.Prefabs))}, "owned": {src.Owned, dst.Owned}} {
		a, _ := json.Marshal(pair[0])
		b, _ := json.Marshal(pair[1])
		if string(a) != string(b) {
			t.Errorf("%s differ:\n%s\n%s", name, a, b)
		}
	}
	// Its sample records, created at install
	eventually(t, "the sample records", func() bool {
		raw, _ := json.Marshal(map[string]any{"entity": "task"})
		v, _, fail := f.subs.Once(in(3), "records.list", raw)
		b, _ := json.Marshal(v)
		return fail == nil && strings.Contains(string(b), "Write docs") && strings.Contains(string(b), "Ship it")
	})
	// A round trip gives the same document but for when it was made
	again := f.export(3, true)
	if a, b := comparable(t, doc), comparable(t, again); a != b {
		t.Errorf("the round trip changed the document:\n%s\n---\n%s", a, b)
	}
	// Its command, decided by the imported graph
	f.okIn(3, "tasks.task.reassign", "t2", map[string]any{"assignee": "mia"})
}

func TestImportAsksAboutDuplicates(t *testing.T) {
	f := setup(t)
	f.exportable()
	doc := f.export(1, false)
	// Tenant 3 has its own Note, and the same Customer
	f.publishIn(3, "prefab", "note", f.okIn(3, "prefab.import", "note", map[string]any{"prefab": note("Tenant 3's note")}).Seq)
	f.publishIn(3, "entity", "customer", f.okIn(3, "entity.create", "customer", customer).Seq)

	svc := f.service()
	resp := svc.Import(appservice.ImportArgs{ServiceArgs: in(3), Document: doc})
	if resp.Imported || resp.Common.Code != 409 || len(resp.Problems) != 1 {
		t.Fatalf("an undecided duplicate: %d %+v", resp.Common.Code, resp.Problems)
	}
	p := resp.Problems[0]
	if p.Code != "app_import_duplicate" || p.Path != "tenantParts.prefab[note]" || !strings.Contains(p.Message, "version 1, published ") || !strings.Contains(p.Fix, `overwrite: ["prefab:note"]`) {
		t.Errorf("the problem: %+v", p)
	}
	status := map[string]appservice.PartPlan{}
	for _, pp := range resp.Plan.Parts {
		status[pp.Type+":"+pp.ID] = pp
	}
	if status["prefab:note"].Status != appservice.PartDuplicate || status["prefab:note"].Target == nil || status["prefab:note"].Target.PublishedAt.IsZero() ||
		status["entity:customer"].Status != appservice.PartSame || status["prefab:myTasks"].Status != appservice.PartNew {
		t.Errorf("plan = %+v", resp.Plan.Parts)
	}
	if _, ok, _ := f.p.Get(in(3).Context(), 3, manifests.DraftProjector, "app.tasks"); ok {
		t.Fatal("a refused import made the app")
	}
	// Keeping the tenant's in a dry run, then overwriting it
	if r := svc.Import(appservice.ImportArgs{ServiceArgs: in(3), Document: doc, Keep: []string{"prefab:note"}, DryRun: true}); r.Common.Code != 200 || r.Imported {
		t.Errorf("dry run: %d %+v", r.Common.Code, r.Problems)
	}
	resp = svc.Import(appservice.ImportArgs{ServiceArgs: in(3), Document: doc, Overwrite: []string{"prefab:note"}})
	if !resp.Imported {
		t.Fatalf("overwrite: %s %+v", resp.Common.Message, resp.Problems)
	}
	raw, _, _ := f.p.Get(in(3).Context(), 3, prefabs.PublishedProjector, "prefab.note")
	if !strings.Contains(string(raw.Value), `"default":"A note"`) || !strings.Contains(string(raw.Value), `"version":2`) {
		t.Errorf("the note was not overwritten: %s", raw.Value)
	}

	// The app's id is taken now; under another its graphs follow it
	resp = svc.Import(appservice.ImportArgs{ServiceArgs: in(3), Document: doc})
	if resp.Common.Code != 409 || len(resp.Problems) != 1 || resp.Problems[0].Path != "id" {
		t.Errorf("a taken id: %d %+v", resp.Common.Code, resp.Problems)
	}
	resp = svc.Import(appservice.ImportArgs{ServiceArgs: in(3), Document: doc, ID: "tasks2", DryRun: true})
	if !slices.ContainsFunc(resp.Plan.Parts, func(p appservice.PartPlan) bool {
		return p.ID == "cmd-tasks2-task-reassign" && p.Status == appservice.PartNew && p.Document.Owner == "app:tasks2"
	}) {
		t.Errorf("renamed plan = %+v", resp.Plan.Parts)
	}
}

func TestCheckNamesWhatIsOutside(t *testing.T) {
	f := setup(t)
	f.exportable()
	doc := f.export(1, false)
	svc := f.service()
	check := func(what string, change func(d *apps.Document), path, want string) {
		t.Helper()
		raw, _ := json.Marshal(doc)
		var d apps.Document
		_ = json.Unmarshal(raw, &d)
		change(&d)
		resp := svc.Check(appservice.CheckArgs{ServiceArgs: args(), Document: d})
		if !slices.ContainsFunc(resp.Problems, func(p live.Problem) bool { return strings.HasPrefix(p.Path, path) && strings.Contains(p.Message, want) }) {
			t.Errorf("%s: no problem at %s with %q in %+v", what, path, want, resp.Problems)
		}
	}
	if resp := svc.Check(appservice.CheckArgs{ServiceArgs: args(), Document: doc}); len(resp.Problems) != 0 {
		t.Fatalf("the export fails its own check: %+v", resp.Problems)
	}
	check("a missing primitive", func(d *apps.Document) {
		d.Parts.Prefabs[1].Prefab.Template.Type = "mui.hologram"
		d.Primitives.Components = append(d.Primitives.Components, "mui.hologram")
	}, "primitives.components[mui.hologram]", "no component mui.hologram")
	check("a missing node type", func(d *apps.Document) {
		d.Parts.Graphs[0].Graph.Nodes[0].Type = "flow.teleport"
	}, "parts.graph[cmd-tasks-task-reassign].graph.nodes.0.type", "not among the document's primitives")
	check("a tenant prefab left out", func(d *apps.Document) {
		d.TenantParts.Prefabs = nil
	}, "parts.prefab[myTasks]", "note")
	check("an entity type outside the app", func(d *apps.Document) {
		d.Parts.Graphs[0].Graph.Nodes[2].Config = json.RawMessage(`{"entity":"invoice"}`)
	}, "parts.graph[cmd-tasks-task-reassign].graph.nodes.2.config.entity", "invoice is not in the document")
	check("another app's part", func(d *apps.Document) {
		d.Parts.Prefabs[0].Prefab.Owner = "app:crm"
	}, "parts.prefab[myTasks].owner", "app:crm")
	check("a pinned version", func(d *apps.Document) {
		d.Parts.Prefabs[0].Prefab.Template.Children[0].Type = "prefab.note@1"
	}, "parts.prefab[myTasks].template.root/note.type", "pins version 1")

	// Export refuses an app whose handler reads an entity type it cannot
	// carry: one never published
	f.publishGraph("taskDetail-onOpen", map[string]any{"name": "Open", "kind": "ui.handler", "owner": "app:tasks",
		"nodes": []any{node("event", "ui.onEvent", nil), node("id", "data.expression", map[string]any{"expr": `"i1"`, "type": "string"}),
			node("get", "records.get", map[string]any{"entity": "invoice"})},
		"edges": edges("id.value -> get.id")})
	f.publishPrefab("taskDetail", f.ok("prefab.attachGraph", "taskDetail", map[string]any{"name": "onOpen", "graph": "taskDetail-onOpen"}).Seq)
	f.publish(f.ok("app.setInfo", "tasks", map[string]any{"description": "v2"}).Seq)
	resp := svc.Export(appservice.ExportArgs{ServiceArgs: args(), App: "tasks"})
	if resp.Common.Code != 422 || !slices.ContainsFunc(resp.Problems, func(p live.Problem) bool { return strings.Contains(p.Message, "invoice") }) {
		t.Errorf("export of an app outside itself: %d %+v", resp.Common.Code, resp.Problems)
	}
}
