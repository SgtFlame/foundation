package ops_test

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/apps/ops"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	cognitionnodes "github.com/SgtFlame/foundation/backend/features/cognition/nodes"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	recordsnodes "github.com/SgtFlame/foundation/backend/features/records/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui"
	uicomponents "github.com/SgtFlame/foundation/backend/features/ui/components"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
)

type fixture struct {
	t    *testing.T
	reg  *extensions.Registry
	bus  *bus.Bus
	subs *subscribe.Subscriptions
	p    *project.Projections
	n    atomic.Int64
}

func setup(t *testing.T) *fixture {
	reg := extensions.NewRegistry()
	cognitionnodes.Contribute(reg)
	uinodes.Contribute(reg)
	if err := uicomponents.Contribute(reg); err != nil {
		t.Fatal(err)
	}
	var (
		nodeDeps   livenodes.Deps
		draftDeps  drafts.Deps
		entityDeps entities.Deps
		dataDeps   data.Deps
		prefabDeps prefabs.Deps
		appDeps    manifests.Deps
		installDep installs.Deps
	)
	livenodes.Contribute(reg, func() livenodes.Deps { return nodeDeps })
	drafts.Contribute(reg, func() drafts.Deps { return draftDeps })
	entities.Contribute(reg, func() entities.Deps { return entityDeps })
	data.Contribute(reg, func() data.Deps { return dataDeps })
	recordsnodes.Contribute(reg, func() data.Deps { return dataDeps })
	prefabs.Contribute(reg, func() prefabs.Deps { return prefabDeps })
	manifests.Contribute(reg, func() manifests.Deps { return appDeps })
	installs.Contribute(reg, func() installs.Deps { return installDep })
	ui.ContributeOverlays(reg)
	ui.Pages.Register(reg, ui.Page{Descriptor: extensions.Descriptor{ID: "demo/main", Source: "test"}, App: "demo", Title: "Demo"})
	ops.Contribute(reg)
	var undoDeps ops.Deps
	ops.ContributeUndo(reg, func() ops.Deps { return undoDeps })
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, slices.Concat(cognition.Models(), records.Models(), ui.Models(), apps.Models())...); err != nil {
		t.Fatal(err)
	}
	srv := natsd.StartTest(t)
	p := project.New(srv.JS(), reg)
	t.Cleanup(p.Close)
	eng := engine.New(gdb, reg, nil)
	eng.SetSource(drafts.Source{Store: p})
	f := &fixture{t: t, reg: reg, bus: bus.New(p), subs: subscribe.New(p, reg), p: p}
	undoDeps = ops.Deps{Bus: f.bus, Store: p}
	nodeDeps = livenodes.Deps{Engine: eng, Bus: f.bus, Subscriptions: f.subs, Store: p}
	draftDeps = drafts.Deps{Engine: eng, Projections: p}
	dataDeps = data.Deps{Projections: p, Bus: f.bus}
	entityDeps = entities.Deps{Projections: p, DB: gdb, OnPublish: func(args features.ServiceArgs) error { return data.Follow(args, dataDeps) }}
	prefabDeps = prefabs.Deps{Projections: p, DB: gdb}
	appDeps = manifests.Deps{Projections: p, DB: gdb}
	installDep = installs.Deps{Projections: p, DB: gdb, Nodes: nodeDeps}
	f.bus.Resolve = installs.CommandFor(func() installs.Deps { return installDep }, livenodes.CommandFor(func() livenodes.Deps { return nodeDeps }))
	f.subs.Resolve = installs.QueryFor(func() installs.Deps { return installDep }, livenodes.QueryFor(func() livenodes.Deps { return nodeDeps }))
	return f
}

func as(user, tenant uint64) features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, UserID: user, TenantID: tenant}}
}

var ada = as(1, 1)

// send sends one command as args in request, for app.
func (f *fixture) send(args features.ServiceArgs, request, app, typ, target string, a any) bus.Result {
	f.t.Helper()
	raw, ok := a.(json.RawMessage)
	if !ok {
		raw, _ = json.Marshal(a)
	}
	res := f.bus.Execute(bus.Request{ServiceArgs: args, ID: fmt.Sprintf("c%d", f.n.Add(1)), Type: typ, Target: target, RequestID: request, App: app, Args: raw})
	if res.Common.Code >= 300 {
		f.t.Fatalf("%s on %s: %s %+v", typ, target, res.Common.Message, res.Problems)
	}
	return res
}

func (f *fixture) ok(typ, target string, a any) bus.Result {
	f.t.Helper()
	return f.send(ada, "", "", typ, target, a)
}

// parts makes what the Tasks app names, owned by app:tasks, and a prefab
// of the tenant library.
func (f *fixture) parts(args features.ServiceArgs, request string) {
	own := "app:tasks"
	ok := func(typ, target string, a any) { f.send(args, request, "tasks", typ, target, a) }
	ok("entity.create", "task", map[string]any{"name": "Task", "owner": own, "fields": []map[string]any{
		{"name": "title", "type": "text"}, {"name": "done", "type": "bool"}, {"name": "assignee", "type": "relation", "relation": map[string]any{"entity": "user"}}}})
	for _, id := range []string{"myTasks", "allTasks"} {
		ok("prefab.create", id, map[string]any{"name": id, "owner": own})
	}
	ok("prefab.create", "taskDetail", map[string]any{"name": "Task detail", "owner": own})
	ok("prefab.setInterface", "taskDetail", map[string]any{"props": map[string]any{"id": map[string]any{"type": "string", "required": true}}})
	ok("bp.create", "cmd-task-reassign", map[string]any{"kind": "command", "name": "Reassign a task", "owner": own, "spec": map[string]any{"command": "task.reassign",
		"title": "Reassign", "description": "Gives a task to someone else", "area": "rec", "targetType": "task", "projection": "records.record", "action": "record.update"}})
	ok("bp.create", "qry-myTasks", map[string]any{"kind": "data.query", "name": "My tasks", "owner": own, "spec": map[string]any{"query": "myTasks",
		"title": "My tasks", "description": "My open tasks", "action": "record.read"}})
	ok("prefab.create", "pageHeader", map[string]any{"name": "Page header"})
}

// manifest builds the Tasks manifest by app.* ops.
func (f *fixture) manifest(args features.ServiceArgs, request string) {
	ok := func(typ string, a any) { f.send(args, request, "", typ, "tasks", a) }
	f.send(args, request, "", "app.create", "", map[string]any{"name": "Tasks", "icon": "checklist"})
	ok("app.addEntity", map[string]any{"entity": "task"})
	ok("app.addPage", map[string]any{"route": "/mine", "prefab": "myTasks", "title": "My tasks"})
	ok("app.addPage", map[string]any{"route": "/all", "prefab": "allTasks", "title": "All tasks"})
	ok("app.addPage", map[string]any{"route": "/task/:id", "prefab": "taskDetail"})
	ok("app.setNav", map[string]any{"nav": []map[string]any{{"title": "My tasks", "route": "/mine"}, {"title": "All tasks", "route": "/all"}}})
	ok("app.setInfo", map[string]any{"home": "/mine"})
	ok("app.addCommand", map[string]any{"type": "task.reassign", "graph": "cmd-task-reassign", "action": "entity:task.update"})
	ok("app.addQuery", map[string]any{"name": "myTasks", "graph": "qry-myTasks"})
	ok("app.addRole", map[string]any{"id": "manager", "title": "Manager", "actions": []string{"entity:task.*"}})
}

// entries are the log's entries in the tenant, every event folded.
func (f *fixture) entries(args features.ServiceArgs, app string) []ops.Entry {
	f.t.Helper()
	out, err := ops.Entries(args, f.p, app)
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

// state is a target's definition as it shows, normalized: what undo must
// restore, leaving out revs, times and authors, and the order of a graph's
// nodes, edges and variables and of an entity type's indexes, which mean
// nothing.
func (f *fixture) state(command, target string, seq uint64) string {
	f.t.Helper()
	var projector, key string
	switch strings.SplitN(command, ".", 2)[0] {
	case "bp":
		projector, key = drafts.DraftProjector, drafts.TargetType+"."+target
	case "prefab":
		projector, key = prefabs.DraftProjector, prefabs.TargetType+"."+target
	case "entity":
		projector, key = entities.DraftProjector, entities.TargetType+"."+target
	case "ui":
		projector, key = "ui.overlay", ui.OverlayType+"."+target
	default:
		projector, key = manifests.DraftProjector, manifests.TargetType+"."+target
	}
	v, err := f.read(projector, key, seq)
	if err != nil {
		f.t.Fatal(err)
	}
	if v.Value == nil {
		return "null"
	}
	var out any
	switch projector {
	case drafts.DraftProjector:
		var d drafts.Draft
		_ = json.Unmarshal(v.Value, &d)
		g := d.Graph
		slices.SortFunc(g.Nodes, func(a, b cognition.Node) int { return cmp.Compare(a.ID, b.ID) })
		for i := range g.Nodes {
			if string(g.Nodes[i].Config) == "{}" {
				g.Nodes[i].Config = nil // an empty config is none
			}
		}
		slices.SortFunc(g.Edges, func(a, b cognition.Edge) int { return cmp.Compare(fmt.Sprint(a), fmt.Sprint(b)) })
		slices.SortFunc(g.Variables, func(a, b cognition.Variable) int { return cmp.Compare(a.ID, b.ID) })
		out = []any{g, d.Published}
	case prefabs.DraftProjector:
		var d prefabs.Draft
		_ = json.Unmarshal(v.Value, &d)
		if d.Deleted {
			return "null" // a deleted prefab is gone as far as anyone sees
		}
		out = []any{d.Prefab, d.Published}
	case entities.DraftProjector:
		var d entities.Draft
		_ = json.Unmarshal(v.Value, &d)
		x := d.Entity
		if x.Hidden {
			return "null" // a deleted type is gone as far as anyone sees
		}
		x.Fields = slices.DeleteFunc(x.Fields, func(f records.Field) bool { return f.Hidden })
		slices.SortFunc(x.Indexes, func(a, b records.Index) int { return cmp.Compare(a.ID, b.ID) })
		out = []any{x, d.Published}
	case manifests.DraftProjector:
		var d manifests.Draft
		_ = json.Unmarshal(v.Value, &d)
		if d.Deleted {
			return "null"
		}
		out = []any{d.Manifest, d.Seed, d.Published}
	default:
		var o ui.Overlay
		_ = json.Unmarshal(v.Value, &o)
		if len(o.Inserts) == 0 {
			return "null" // a page with no added nodes
		}
		slices.SortFunc(o.Inserts, func(a, b ui.Insert) int { return cmp.Compare(a.Node.Key, b.Node.Key) })
		out = o
	}
	raw, _ := json.Marshal(out)
	return string(raw)
}

// read is a projected value once it reflects the commit at seq, or with
// seq 0 once the projector has folded every event so far.
func (f *fixture) read(projector, key string, seq uint64) (live.Projected, error) {
	ctx := context.Background()
	if seq > 0 {
		return f.p.WaitFor(ctx, 1, projector, key, seq, 3*time.Second)
	}
	if err := f.p.Settle(ctx, 1, projector); err != nil {
		return live.Projected{}, err
	}
	v, _, err := f.p.Get(ctx, 1, projector, key)
	return v, err
}

// rev is a draft's rev, for the publish commands.
func (f *fixture) rev(command, target string, seq uint64) int {
	var projector, key string
	switch strings.SplitN(command, ".", 2)[0] {
	case "bp":
		projector, key = drafts.DraftProjector, drafts.TargetType+"."+target
	case "prefab":
		projector, key = prefabs.DraftProjector, prefabs.TargetType+"."+target
	case "entity":
		projector, key = entities.DraftProjector, entities.TargetType+"."+target
	default:
		projector, key = manifests.DraftProjector, manifests.TargetType+"."+target
	}
	v, _ := f.read(projector, key, seq)
	var d struct{ Rev int }
	_ = json.Unmarshal(v.Value, &d)
	return d.Rev
}

type step struct {
	command, target string
	args            any // "rev" in a map is filled with the draft's rev
}

func s(command, target string, args any) step { return step{command, target, args} }

type m = map[string]any

// steps change one of everything, each a definition command a table of
// states is built by: every logged command appears.
func steps() []step {
	own := "app:tasks"
	pin := func(node, p string) m { return m{"node": node, "pin": p} }
	return []step{
		// Entity types
		s("entity.create", "note", m{"name": "Note", "owner": own, "fields": []m{{"name": "body", "type": "text"}}}),
		s("entity.addField", "task", m{"name": "due", "type": "date"}),
		s("entity.updateField", "task", m{"field": "due", "name": "due date", "description": "When it is due"}),
		s("entity.updateField", "task", m{"field": "due", "type": "dateTime"}),
		s("entity.addField", "task", m{"name": "priority", "type": "enum", "options": []m{{"id": "low", "name": "Low"}, {"id": "high", "name": "High"}}, "default": "low"}),
		s("entity.updateField", "task", m{"field": "priority", "type": "text"}),
		s("entity.removeField", "task", m{"field": "due"}),
		s("entity.restoreField", "task", m{"field": "due"}),
		s("entity.addIndex", "task", m{"id": "byDone", "fields": []string{"done"}}),
		s("entity.addIndex", "task", m{"fields": []string{"title"}}),
		s("entity.removeIndex", "task", m{"index": "byDone"}),
		s("entity.setAccess", "task", m{"access": m{"update": "record.assignee == principal.user"}}),
		s("entity.setInfo", "task", m{"name": "Todo", "description": "Something to do"}),
		s("entity.import", "memo", m{"entity": m{"name": "Memo", "owner": own, "fields": []m{{"id": "text", "name": "text", "type": "text"}}}}),
		s("entity.import", "memo", m{"entity": m{"name": "Memo", "owner": own, "fields": []m{{"id": "text", "name": "text", "type": "text"}, {"id": "seen", "name": "seen", "type": "bool"}}}}),
		s("entity.delete", "memo", m{}),
		s("entity.restore", "memo", m{}),
		s("entity.publish", "task", m{"rev": 0}),
		// Graphs
		s("bp.create", "greet", m{"kind": "function", "name": "Greet", "owner": own}),
		s("bp.setConfig", "greet", m{"node": "event", "config": m{"outputs": []m{{"id": "name", "type": "string", "description": "who"}}}}),
		s("bp.addNode", "greet", m{"type": "flow.return", "id": "ret", "config": m{"outputs": []m{{"id": "out", "type": "string"}}}}),
		s("bp.connect", "greet", m{"from": pin("event", "then"), "to": pin("ret", "then")}),
		s("bp.connect", "greet", m{"from": pin("event", "name"), "to": pin("ret", "out")}),
		s("bp.setConfig", "greet", m{"node": "ret", "title": "Answer"}),
		s("bp.moveNodes", "greet", m{"moves": []m{{"node": "ret", "position": m{"x": 900, "y": 40}}}}),
		s("bp.addVariable", "greet", m{"name": "Attempts", "type": "number", "default": 0}),
		s("bp.addVariable", "greet", m{"id": "attempts", "name": "Tries", "type": "number", "default": 1}),
		s("bp.removeVariable", "greet", m{"id": "attempts"}),
		s("bp.disconnect", "greet", m{"from": pin("event", "name"), "to": pin("ret", "out")}),
		s("bp.remove", "greet", m{"nodes": []string{"ret"}}),
		s("bp.paste", "greet", m{"nodes": []m{{"id": "ret2", "type": "flow.return", "position": m{"x": 1, "y": 2}, "config": m{"outputs": []m{{"id": "out", "type": "string"}}}}},
			"edges": []m{{"from": pin("event", "then"), "to": pin("ret2", "then")}}}),
		s("bp.setInfo", "greet", m{"description": "Says hello", "name": "Greeting"}),
		s("bp.import", "greet", m{"graph": m{"name": "Greet", "kind": "function", "owner": own, "nodes": []m{}, "edges": []m{}}}),
		s("bp.import", "echo", m{"graph": m{"name": "Echo", "kind": "function", "nodes": []m{}, "edges": []m{}}}),
		s("bp.create", "tasks-onClick", m{"kind": "ui.handler", "name": "On click", "owner": own}),
		s("bp.publish", "tasks-onClick", m{"rev": 0}),
		s("bp.delete", "echo", m{}),
		s("bp.restore", "echo", m{}),
		// Prefabs
		s("prefab.create", "row", m{"name": "Row", "owner": own}),
		s("prefab.setInterface", "row", m{"props": m{"title": m{"type": "string"}}, "events": m{"clicked": m{"description": "Clicked"}}}),
		s("prefab.insertNode", "row", m{"node": m{"type": "mui.stack", "key": "root", "props": m{"direction": "row"}}}),
		s("prefab.insertNode", "row", m{"parentPath": "root", "node": m{"type": "mui.text", "key": "title", "bind": m{"text": "props.title"}}}),
		s("prefab.insertNode", "row", m{"parentPath": "root", "node": m{"type": "mui.chip", "key": "a", "props": m{"label": "a"}}}),
		s("prefab.insertNode", "row", m{"parentPath": "root", "node": m{"type": "mui.chip", "key": "b", "props": m{"label": "b"}}}),
		s("prefab.insertNode", "row", m{"parentPath": "root", "index": 0, "node": m{"type": "mui.chip", "key": "c", "props": m{"label": "c"}}}),
		s("prefab.updateNode", "row", m{"path": "root/a", "props": m{"label": "A", "color": "primary"}}),
		s("prefab.updateNode", "row", m{"path": "root/a", "props": m{"color": nil}, "key": "first"}),
		s("prefab.updateNode", "row", m{"path": "root/title", "bind": m{"text": nil}, "props": m{"text": "Hi"}}),
		s("prefab.moveNode", "row", m{"path": "root/b", "parentPath": "root", "index": 0}),
		s("prefab.wrapNodes", "row", m{"paths": []string{"root/first", "root/title"}, "wrapper": m{"type": "mui.stack", "key": "texts"}}),
		s("prefab.unwrapNode", "row", m{"path": "root/texts"}),
		s("prefab.wrapNodes", "row", m{"paths": []string{"root/c", "root/first"}, "wrapper": m{"type": "mui.box"}}),
		s("prefab.wrapNodes", "row", m{"paths": []string{"root"}, "wrapper": m{"type": "mui.box", "key": "outer"}}),
		s("prefab.unwrapNode", "row", m{"path": "outer"}),
		s("prefab.removeNode", "row", m{"path": "root/b"}),
		s("prefab.attachGraph", "row", m{"name": "onClick", "graph": "tasks-onClick"}),
		s("prefab.updateNode", "row", m{"path": "root/box/c", "on": m{"click": m{"handler": "onClick"}}}),
		s("prefab.updateNode", "row", m{"path": "root/box/c", "on": m{"click": nil}}),
		s("prefab.detachGraph", "row", m{"name": "onClick"}),
		s("prefab.setPreview", "row", m{"props": m{"title": "Water the plants"}}),
		s("prefab.setVariants", "row", m{"variants": m{"loud": m{"props": m{"title": "LOUD"}}}}),
		s("prefab.setInfo", "row", m{"description": "One row", "name": "A row", "tags": []string{"row", "list"}}),
		s("prefab.import", "row", m{"prefab": m{"name": "Row", "owner": own, "template": m{"type": "mui.text", "key": "root"}}}),
		s("prefab.import", "card", m{"prefab": m{"name": "Card", "template": m{"type": "mui.text", "key": "root"}}}),
		s("prefab.publish", "row", m{"rev": 0}),
		s("prefab.delete", "card", m{}),
		s("prefab.restore", "card", m{}),
		// Apps
		s("app.setInfo", "tasks", m{"description": "Tasks for people", "frame": ""}),
		s("app.setNav", "tasks", m{"nav": []m{{"title": "All tasks", "route": "/all"}}}),
		s("app.setSettingsSchema", "tasks", m{"schema": m{"type": "object", "properties": m{"days": m{"type": "integer"}}}, "defaults": m{"days": 7}}),
		s("app.addPage", "tasks", m{"route": "/row", "prefab": "row", "index": 0}),
		s("app.updatePage", "tasks", m{"route": "/all", "title": "Everything", "rename": "/everything"}),
		s("app.setInfo", "tasks", m{"home": "/row"}),
		s("app.removePage", "tasks", m{"route": "/mine"}),
		s("app.addEditorKind", "tasks", m{"kind": "task", "page": "/task/:id", "title": "{record.title}", "subject": m{"type": "task", "label": "{record.title}"}}),
		s("app.updateEditorKind", "tasks", m{"kind": "task", "icon": "checklist", "subject": m{"label": "{record.name}"}}),
		s("app.removeEditorKind", "tasks", m{"kind": "task"}),
		s("app.updateCommand", "tasks", m{"type": "task.reassign", "action": "entity:task.delete"}),
		s("app.removeCommand", "tasks", m{"type": "task.reassign"}),
		s("app.addCommand", "tasks", m{"type": "task.reassign", "graph": "cmd-task-reassign", "action": "entity:task.update"}),
		s("app.updateQuery", "tasks", m{"name": "myTasks", "description": "Mine"}),
		s("app.removeQuery", "tasks", m{"name": "myTasks"}),
		s("app.addQuery", "tasks", m{"name": "myTasks", "graph": "qry-myTasks"}),
		s("app.addRole", "tasks", m{"id": "member", "actions": []string{"entity:task.read"}, "rowRules": m{"task": "record.assignee == principal.user"}}),
		s("app.updateRole", "tasks", m{"id": "member", "title": "Member", "rowRules": m{"task": nil}}),
		s("app.removeRole", "tasks", m{"id": "manager"}),
		s("app.addTrigger", "tasks", m{"id": "taskDone", "title": "Done"}),
		s("app.updateTrigger", "tasks", m{"id": "taskDone", "config": m{"field": "done"}}),
		s("app.removeTrigger", "tasks", m{"id": "taskDone"}),
		s("app.addEntity", "tasks", m{"entity": "note"}),
		s("app.setSeed", "tasks", m{"seed": m{"task": []m{{"id": "t1", "values": m{"title": "First"}}}}}),
		s("app.removeEntity", "tasks", m{"entity": "note"}),
		s("app.setManifest", "tasks", m{"manifest": m{"name": "Tasks", "pages": []m{{"route": "/row", "prefab": "row"}}, "entities": []string{"task"}}}),
		s("app.create", "", m{"name": "Notes"}),
		s("app.delete", "notes", m{}),
		s("app.restore", "notes", m{}),
		// Page overlays
		s("ui.insertNode", "demo-main", m{"parent": "root", "node": m{"type": "mui.text", "key": "hi", "props": m{"text": "Hi"}}}),
		s("ui.removeNode", "demo-main", m{"key": "hi"}),
	}
}

// TestEveryOpUndoes runs each definition command against the states the
// steps before it left, then its inverse, which must restore the state
// exactly, then the command again, which must give the same state as the
// first time (forge 06a's property test).
func TestEveryOpUndoes(t *testing.T) {
	f := setup(t)
	f.parts(ada, "")
	f.manifest(ada, "")
	covered := map[string]bool{}
	var seq uint64 // the last commit, which reads wait for
	for i, st := range steps() {
		covered[st.command] = true
		if a, ok := st.args.(m); ok {
			if _, ok := a["rev"]; ok {
				a["rev"] = f.rev(st.command, st.target, 0)
			}
		}
		target := st.target
		before := "null" // a part made with an id from its name is new
		if target != "" {
			before = f.state(st.command, target, 0)
		}
		res := f.ok(st.command, target, st.args)
		seq = max(seq, res.Seq)
		target = res.Target
		after := f.state(st.command, target, res.Seq)
		entries := f.entries(ada, "")
		e := entries[len(entries)-1]
		if e.Seq != res.Seq || e.Command != st.command {
			t.Fatalf("step %d: the log's last op is %s at %d, not %s at %d", i, e.Command, e.Seq, st.command, res.Seq)
		}
		if e.Irreversible != "" {
			if !irreversible(st.command) {
				t.Errorf("step %d, %s: no inverse (%s)", i, st.command, e.Irreversible)
			}
			continue
		}
		if len(e.Inverse) == 0 {
			t.Fatalf("step %d, %s: an empty inverse", i, st.command)
		}
		for _, op := range e.Inverse {
			seq = max(seq, f.ok(op.Command, op.Target, op.Args).Seq)
		}
		if got := f.state(st.command, target, seq); got != before {
			t.Errorf("step %d, %s %s: undoing gave\n  %s\nnot\n  %s\n(inverse %s)", i, st.command, target, got, before, mustJSON(e.Inverse))
		}
		// Redoing is undoing the undo: the undo's own inverses, last first
		undo := f.entries(ada, "")[len(entries):]
		var redo []live.Op
		for _, u := range slices.Backward(undo) {
			if u.Irreversible != "" {
				redo = nil
				break
			}
			redo = append(redo, u.Inverse...)
		}
		for _, op := range redo {
			seq = max(seq, f.ok(op.Command, op.Target, op.Args).Seq)
		}
		if got := f.state(st.command, target, seq); redo != nil && got != after {
			t.Errorf("step %d, %s %s: redoing gave\n  %s\nnot\n  %s\n(redo %s)", i, st.command, target, got, after, mustJSON(redo))
		}
		if redo == nil {
			// Carry on from the state the step left
			seq = max(seq, f.ok(st.command, target, st.args).Seq)
		}
	}
	for _, c := range live.Commands.All(f.reg) {
		if c.Logged() && !covered[c.ID] && !irreversible(c.ID) {
			t.Errorf("no step runs %s", c.ID)
		}
	}
}

// irreversible are the commands no op undoes: publishing and installs.
// Making and deleting parts are undone by their restorable deletes (forge
// 06b).
func irreversible(command string) bool {
	return slices.Contains([]string{"bp.publish", "prefab.publish", "entity.publish", "app.publish",
		"app.install", "app.upgrade", "app.uninstall"}, command)
}

func mustJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

// sink collects an ops.list subscription's result.
type sink struct {
	mu    sync.Mutex
	log   any
	delta []live.Delta
	got   chan struct{}
}

func (s *sink) Snapshot(data any, _ live.Position) {
	raw, _ := json.Marshal(data)
	var v any
	_ = json.Unmarshal(raw, &v)
	s.mu.Lock()
	s.log = v
	s.mu.Unlock()
	s.got <- struct{}{}
}
func (s *sink) Change(d live.Delta, _ live.Position) {
	s.mu.Lock()
	s.log = live.Apply(s.log, d)
	s.delta = append(s.delta, d)
	s.mu.Unlock()
	s.got <- struct{}{}
}
func (s *sink) Failed(resp features.ServiceResponse) { panic(resp.Message) }

func (s *sink) current() ops.Log {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, _ := json.Marshal(s.log)
	var l ops.Log
	_ = json.Unmarshal(raw, &l)
	return l
}

// until waits for a result that passes ok.
func (s *sink) until(t *testing.T, what string, ok func(ops.Log) bool) ops.Log {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if l := s.current(); ok(l) {
			return l
		}
		select {
		case <-s.got:
		case <-deadline:
			t.Fatalf("no result with %s arrived: %s", what, mustJSON(s.current()))
		}
	}
}

func (f *fixture) open(args features.ServiceArgs, params ops.Params) *sink {
	f.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	f.t.Cleanup(cancel)
	snk := &sink{got: make(chan struct{}, 1000)}
	args.Ctx = ctx
	raw, _ := json.Marshal(params)
	if fail := f.subs.Open(subscribe.OpenArgs{ServiceArgs: args, Query: "ops.list", Params: raw, Sink: snk}); fail != nil {
		f.t.Fatal(fail.Message)
	}
	return snk
}

func requests(l ops.Log) []string {
	var out []string
	for _, r := range l.Requests {
		out = append(out, r.ID)
	}
	return out
}

// TestTheLogByAppRequestAndPrincipal builds the Tasks app in two requests
// from two principals: ops.list by app, request and principal gives each
// its ops, and a subscriber sees a third request arrive.
func TestTheLogByAppRequestAndPrincipal(t *testing.T) {
	f := setup(t)
	bob := as(2, 1)
	f.parts(ada, "parts")
	f.manifest(bob, "manifest")
	f.send(ada, "crm", "", "prefab.create", "crmCard", m{"name": "CRM card", "owner": "app:crm"})
	f.entries(ada, "") // the log has folded them all, so later changes are only the third request's

	all := f.open(ada, ops.Params{App: "tasks"})
	l := all.until(t, "both requests", func(l ops.Log) bool {
		return len(l.Requests) == 2 && len(l.Requests[0].Ops)+len(l.Requests[1].Ops) == 18
	})
	if got := requests(l); !slices.Equal(got, []string{"manifest", "parts"}) {
		t.Fatalf("the requests are %v, newest first", got)
	}
	parts, manifest := l.Requests[1], l.Requests[0]
	if parts.Principal != 1 || manifest.Principal != 2 || len(parts.Ops) != 8 || len(manifest.Ops) != 10 {
		t.Errorf("parts by %d with %d ops, manifest by %d with %d ops", parts.Principal, len(parts.Ops), manifest.Principal, len(manifest.Ops))
	}
	// The library prefab is in the app's log, as the request was made for it
	if i := slices.IndexFunc(parts.Ops, func(e ops.Entry) bool { return e.Target == "pageHeader" }); i < 0 || parts.Ops[i].App != "tasks" {
		t.Errorf("pageHeader is not logged under tasks: %s", mustJSON(parts.Ops))
	}
	for _, e := range manifest.Ops {
		if e.App != "tasks" || e.Request != "manifest" || e.Seq <= e.Decided && e.Decided != 0 {
			t.Errorf("an op of the manifest: %s", mustJSON(e))
		}
	}
	if first := manifest.Ops[0]; first.Command != "app.create" || first.Target != "tasks" || mustJSON(first.Inverse) != `[{"command":"app.delete","target":"tasks","args":{}}]` {
		t.Errorf("the first op is %s", mustJSON(first))
	}
	if add := manifest.Ops[1]; add.Command != "app.addEntity" || mustJSON(add.Inverse) != `[{"command":"app.removeEntity","target":"tasks","args":{"entity":"task"}}]` {
		t.Errorf("app.addEntity's inverse is %s", mustJSON(add.Inverse))
	}

	byRequest := f.open(ada, ops.Params{Request: "parts"})
	if got := requests(byRequest.until(t, "the parts", func(l ops.Log) bool { return len(l.Requests) == 1 })); !slices.Equal(got, []string{"parts"}) {
		t.Errorf("by request: %v", got)
	}
	byBob := f.open(ada, ops.Params{Principal: 2})
	if got := byBob.until(t, "Bob's", func(l ops.Log) bool { return len(l.Requests) == 1 && len(l.Requests[0].Ops) == 10 }); got.Requests[0].ID != "manifest" || len(got.Requests[0].Ops) != 10 {
		t.Errorf("by principal: %s", mustJSON(got))
	}
	crm := f.open(ada, ops.Params{App: "crm"})
	if got := crm.until(t, "crm's", func(l ops.Log) bool { return len(l.Requests) == 1 }); got.Requests[0].Ops[0].Target != "crmCard" {
		t.Errorf("crm's log: %s", mustJSON(got))
	}

	// A third request, by Ada, as a chat would send it: the subscriber
	// sees it arrive first, and the second request's ops stay as they were
	f.bus.Execute(bus.Request{ServiceArgs: ada, ID: "third-1", Type: "app.addTrigger", Target: "tasks", RequestID: "round-7", Actor: "chat", Args: json.RawMessage(`{"id":"taskDone"}`)})
	f.send(ada, "round-7", "", "app.updateTrigger", "tasks", m{"id": "taskDone", "title": "A task is done"})
	l = all.until(t, "the third request", func(l ops.Log) bool { return len(l.Requests) == 3 && len(l.Requests[0].Ops) == 2 })
	if r := l.Requests[0]; r.ID != "round-7" || r.Actor != "chat" || r.Principal != 1 {
		t.Errorf("the third request: %s", mustJSON(r))
	}
	if got := requests(l); !slices.Equal(got, []string{"round-7", "manifest", "parts"}) {
		t.Errorf("after the third request: %v", got)
	}
	all.mu.Lock()
	ops0 := all.delta
	all.mu.Unlock()
	if len(ops0) < 2 || ops0[0].Op != "insert" || ops0[len(ops0)-1].Op != "move" {
		t.Errorf("the changes: %s", mustJSON(ops0))
	}
	// A command sent without a request is its own
	res := f.ok("app.updateTrigger", "tasks", m{"id": "taskDone", "description": "Fires when a task is done"})
	if e := f.entries(ada, "tasks"); e[len(e)-1].Request == "" || e[len(e)-1].Request != e[len(e)-1].CommandID || res.RequestID != e[len(e)-1].Request {
		t.Errorf("a command without a request is logged under %q", e[len(e)-1].Request)
	}
}

// TestTheLogByScope reads the log as an editor's Activity tab does (forge
// 06c): one prefab's ops, the manifest's, each request and op summarized.
func TestTheLogByScope(t *testing.T) {
	f := setup(t)
	f.parts(ada, "parts")
	f.manifest(as(2, 1), "manifest")
	f.entries(ada, "")

	detail := f.open(ada, ops.Params{Scope: "prefab:taskDetail"}).until(t, "taskDetail's", func(l ops.Log) bool { return len(l.Requests) == 1 && len(l.Requests[0].Ops) == 2 })
	r := detail.Requests[0]
	if r.ID != "parts" || r.Ops[0].Command != "prefab.create" || r.Ops[1].Command != "prefab.setInterface" {
		t.Errorf("taskDetail's log: %s", mustJSON(detail))
	}
	if !strings.Contains(r.Summary, "taskDetail") || !strings.HasSuffix(r.Summary, "and 1 more change") || !strings.Contains(r.Ops[1].Summary, "taskDetail") {
		t.Errorf("the summaries: %q, %q", r.Summary, r.Ops[1].Summary)
	}
	manifest := f.open(ada, ops.Params{Scope: "app:tasks/manifest"}).until(t, "the manifest's", func(l ops.Log) bool { return len(l.Requests) == 1 && len(l.Requests[0].Ops) == 10 })
	if manifest.Requests[0].ID != "manifest" || !strings.Contains(manifest.Requests[0].Ops[2].Summary, "/mine") {
		t.Errorf("the manifest's log: %s", mustJSON(manifest))
	}
	whole := f.open(ada, ops.Params{Scope: "app:tasks"}).until(t, "the app's", func(l ops.Log) bool { return len(l.Requests) == 2 })
	if got := requests(whole); !slices.Equal(got, []string{"manifest", "parts"}) {
		t.Errorf("the app's requests: %v", got)
	}
}

// TestReplayRebuildsAnApp replays the Tasks app's log into an empty
// tenant: its parts and manifest come out the same, published.
func TestReplayRebuildsAnApp(t *testing.T) {
	f := setup(t)
	f.parts(ada, "parts")
	f.manifest(ada, "manifest")
	for _, x := range []struct{ command, target string }{{"entity.publish", "task"}, {"prefab.publish", "myTasks"}, {"prefab.publish", "allTasks"},
		{"prefab.publish", "taskDetail"}, {"bp.publish", "cmd-task-reassign"}, {"bp.publish", "qry-myTasks"}, {"app.publish", "tasks"}} {
		if x.command == "app.publish" {
			// The app checks its parts' published versions
			for _, pr := range []string{drafts.PublishedProjector, prefabs.PublishedProjector, entities.PublishedProjector} {
				if err := f.p.Settle(context.Background(), 1, pr); err != nil {
					t.Fatal(err)
				}
			}
		}
		f.send(ada, "publish", "", x.command, x.target, m{"rev": f.rev(x.command, x.target, 0)})
	}
	n, err := ops.Replay(ada, f.p, "tasks", as(1, 3), f.bus)
	if err != nil {
		t.Fatal(err)
	}
	if want := len(f.entries(ada, "tasks")); n != want {
		t.Errorf("replayed %d ops of %d", n, want)
	}
	read := func(tenant uint64, projector, key string) string {
		v, _, _ := f.p.Get(context.Background(), tenant, projector, key)
		var d map[string]any
		_ = json.Unmarshal(v.Value, &d)
		for _, k := range []string{"updatedAt", "createdBy", "updatedBy", "versions"} {
			delete(d, k)
		}
		return mustJSON(d)
	}
	for _, k := range []struct{ projector, key string }{{manifests.DraftProjector, "app.tasks"}, {entities.DraftProjector, "entity.task"},
		{prefabs.DraftProjector, "prefab.myTasks"}, {prefabs.DraftProjector, "prefab.pageHeader"}, {drafts.DraftProjector, "graph.cmd-task-reassign"},
		{drafts.DraftProjector, "graph.qry-myTasks"}} {
		if a, b := read(1, k.projector, k.key), read(3, k.projector, k.key); a != b || a == "null" {
			t.Errorf("%s differs after replaying:\n  %s\n  %s", k.key, a, b)
		}
	}
	if got := f.entries(as(1, 3), "tasks"); len(got) != n || !strings.HasPrefix(got[0].Request, "replay-") {
		t.Errorf("the replayed log has %d ops", len(got))
	}
}
