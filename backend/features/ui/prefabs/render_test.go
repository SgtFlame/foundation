package prefabs_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
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

// The template evaluator (forge 04b) over a real stack: records, graphs
// published by bp.* commands, prefabs published by prefab.* commands and
// a UI session opened on a prefab page.

type render struct {
	t      *testing.T
	reg    *extensions.Registry
	bus    *bus.Bus
	subs   *subscribe.Subscriptions
	p      *project.Projections
	n      int
	seq    map[string]uint64
	notes  chan string // what the test.note nodes saw, in order
	early  []string    // notes seen while waiting for another
	runner uinodes.Runner
}

func newRender(t *testing.T) *render {
	reg := extensions.NewRegistry()
	if err := uicomponents.Contribute(reg); err != nil {
		t.Fatal(err)
	}
	st := &render{t: t, reg: reg, seq: map[string]uint64{}, notes: make(chan string, 256)}
	cognitionnodes.Contribute(reg)
	// test.note records what a graph saw, so lifecycle runs can be watched
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "test.note", Source: "test"}, Title: "Note", Category: "Test", Effects: true,
		Kinds: []string{uinodes.KindHandler, uinodes.KindPrefab}, Description: "Notes a label and a value for the test", Example: "Note mount",
		Config: json.RawMessage(`{"type":"object","properties":{"label":{"type":"string"}}}`),
		Pins: cognition.Pins{In: []cognition.Pin{cognition.ExecPin(cognition.Then, ""), {ID: "value", Name: "Value", Type: cognition.Any, Optional: true, Description: "What to note"}},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, "")}},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Label string }
			_ = x.Config(&c)
			st.notes <- fmt.Sprintf("%s:%v", c.Label, in["value"])
			return nil, cognition.Then, nil
		},
	})
	var (
		draftDeps  drafts.Deps
		liveDeps   livenodes.Deps
		uiDeps     uinodes.Deps
		entityDeps entities.Deps
		recordDeps data.Deps
		prefabDeps prefabs.Deps
	)
	drafts.Contribute(reg, func() drafts.Deps { return draftDeps })
	livenodes.Contribute(reg, func() livenodes.Deps { return liveDeps })
	uinodes.Contribute(reg)
	entities.Contribute(reg, func() entities.Deps { return entityDeps })
	data.Contribute(reg, func() data.Deps { return recordDeps })
	recordsnodes.Contribute(reg, func() data.Deps { return recordDeps })
	prefabs.Contribute(reg, func() prefabs.Deps { return prefabDeps })
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, slices.Concat(cognition.Models(), records.Models(), ui.Models())...); err != nil {
		t.Fatal(err)
	}
	srv := natsd.StartTest(t)
	p := project.New(srv.JS(), reg)
	t.Cleanup(p.Close)
	b := bus.New(p)
	subs := subscribe.New(p, reg)
	eng := engine.New(gdb, reg, nil)
	eng.SetSource(drafts.Source{Store: p})
	draftDeps = drafts.Deps{Engine: eng, Projections: p}
	liveDeps = livenodes.Deps{Engine: eng, Bus: b, Subscriptions: subs, Store: p}
	uiDeps = uinodes.Deps{Engine: eng}
	recordDeps = data.Deps{Projections: p, Bus: b}
	entityDeps = entities.Deps{Projections: p, DB: gdb, OnPublish: func(args features.ServiceArgs) error { return data.Follow(args, recordDeps) }}
	prefabDeps = prefabs.Deps{Projections: p, DB: gdb}
	b.Resolve = livenodes.CommandFor(func() livenodes.Deps { return liveDeps })
	subs.Resolve = livenodes.QueryFor(func() livenodes.Deps { return liveDeps })
	st.bus, st.subs, st.p = b, subs, p
	st.runner = uinodes.Runner{Deps: func() uinodes.Deps { return uiDeps }}
	return st
}

func user() features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, UserID: 1, TenantID: 1}}
}

// do sends a command as user 1 and fails on a rejection.
func (st *render) do(typ, target string, a any) bus.Result {
	st.t.Helper()
	res := st.try(typ, target, a)
	if res.Common.Code >= 300 {
		st.t.Fatalf("%s %s: %s %+v", typ, target, res.Common.Message, res.Problems)
	}
	return res
}

func (st *render) try(typ, target string, a any) bus.Result {
	st.n++
	raw, _ := json.Marshal(a)
	res := st.bus.Execute(bus.Request{ServiceArgs: user(), ID: fmt.Sprintf("r%d", st.n), Type: typ, Target: target, Args: raw})
	if res.Common.Code < 300 {
		st.seq[res.Target] = max(st.seq[res.Target], res.Seq)
	}
	return res
}

// rev reads a draft's rev once its last command is projected.
func (st *render) rev(projector, key, target string) int {
	st.t.Helper()
	v, err := st.p.WaitFor(context.Background(), 1, projector, key, st.seq[target], 2*time.Second)
	if err != nil {
		st.t.Fatal(err)
	}
	var d struct{ Rev int }
	_ = json.Unmarshal(v.Value, &d)
	return d.Rev
}

// graph imports and publishes a graph: nodes as [id, type, config], edges
// as "node.pin>node.pin".
func (st *render) graph(id, kind string, nodes [][3]any, edges ...string) {
	st.t.Helper()
	var ns []map[string]any
	for _, n := range nodes {
		ns = append(ns, map[string]any{"id": n[0], "type": n[1], "config": n[2], "position": map[string]int{"x": 0, "y": 0}})
	}
	var es []map[string]any
	for _, e := range edges {
		from, to, _ := strings.Cut(e, ">")
		fn, fp, _ := strings.Cut(from, ".")
		tn, tp, _ := strings.Cut(to, ".")
		es = append(es, map[string]any{"from": map[string]string{"node": fn, "pin": fp}, "to": map[string]string{"node": tn, "pin": tp}})
	}
	st.do("bp.import", id, map[string]any{"graph": map[string]any{"name": id, "kind": kind, "nodes": ns, "edges": es}})
	st.do("bp.publish", id, map[string]int{"rev": st.rev(drafts.DraftProjector, "graph."+id, id)})
	if _, err := st.p.WaitFor(context.Background(), 1, drafts.PublishedProjector, "graph."+id, st.seq[id], 2*time.Second); err != nil {
		st.t.Fatal(err)
	}
}

// publish publishes a prefab's draft and waits until it is the last
// version, in the projection and in the tenant database.
func (st *render) publish(id string) {
	st.t.Helper()
	st.do("prefab.publish", id, map[string]int{"rev": st.rev(prefabs.DraftProjector, "prefab."+id, id)})
	if _, err := st.p.WaitFor(context.Background(), 1, prefabs.PublishedProjector, "prefab."+id, st.seq[id], 2*time.Second); err != nil {
		st.t.Fatal(err)
	}
}

func (st *render) insert(prefab, parent string, node map[string]any) {
	st.t.Helper()
	a := map[string]any{"node": node}
	if parent != "" {
		a["parentPath"] = parent
	}
	st.do("prefab.insertNode", prefab, a)
}

// task creates a task record, returning its id.
func (st *render) task(title string) string {
	st.t.Helper()
	res := st.do("record.create", "", map[string]any{"entity": "task", "values": map[string]any{"title": title, "done": false}})
	return res.Target
}

// session is a UI session's frames.
type session struct {
	t      *testing.T
	s      *ui.Session
	frames chan any
}

func (st *render) open(page ui.Page) *session {
	st.t.Helper()
	return st.openWith(page, nil)
}

func (st *render) openWith(page ui.Page, params json.RawMessage) *session {
	st.t.Helper()
	ss := &session{t: st.t, frames: make(chan any, 512)}
	ctx, cancel := context.WithCancel(context.Background())
	st.t.Cleanup(cancel)
	args := user()
	args.Ctx = ctx
	s, fail := ui.Open(args, "s1", page, params, nil, ui.Options{Catalog: ui.Catalog(st.reg), Commander: commander{st.bus}, Graphs: st.runner, Subs: st.subs,
		Send: func(f any) { ss.frames <- f }})
	if fail != nil {
		st.t.Fatal(fail.Message)
	}
	ss.s = s
	st.t.Cleanup(func() { s.Close(true) })
	return ss
}

type commander struct{ b *bus.Bus }

func (c commander) Execute(args features.ServiceArgs, id, typ, target, request string, raw json.RawMessage) ([]live.Problem, *features.ServiceResponse) {
	res := c.b.Execute(bus.Request{ServiceArgs: args, ID: id, Type: typ, Target: target, RequestID: request, Args: raw})
	if res.Common.Code >= 300 {
		return res.Problems, &res.Common
	}
	return nil, nil
}

// tree is the session's first tree.
func (ss *session) tree() *ui.Node {
	ss.t.Helper()
	for {
		select {
		case f := <-ss.frames:
			if t, ok := f.(ui.TreeFrame); ok {
				return t.Tree
			}
		case <-time.After(5 * time.Second):
			ss.t.Fatal("no tree")
		}
	}
}

// patches waits for a patch frame whose JSON holds all of want, and gives
// its patches.
func (ss *session) patches(want ...string) []ui.Patch {
	ss.t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case f := <-ss.frames:
			if e, ok := f.(ui.ErrorFrame); ok {
				ss.t.Logf("error frame: %+v", e)
			}
			pf, ok := f.(ui.PatchFrame)
			if !ok {
				continue
			}
			raw, _ := json.Marshal(pf)
			if !slices.ContainsFunc(want, func(w string) bool { return !strings.Contains(string(raw), w) }) {
				return pf.Patches
			}
		case <-timeout:
			ss.t.Fatalf("no patch with %v", want)
			return nil
		}
	}
}

// note waits for a note, in any order with the others.
func (st *render) note(want string) {
	st.t.Helper()
	if i := slices.Index(st.early, want); i >= 0 {
		st.early = slices.Delete(st.early, i, i+1)
		return
	}
	timeout := time.After(5 * time.Second)
	for {
		select {
		case n := <-st.notes:
			if n == want {
				return
			}
			st.early = append(st.early, n)
		case <-timeout:
			st.t.Fatalf("no note %q; saw %v", want, st.early)
		}
	}
}

func text(n *ui.Node) string { raw, _ := json.Marshal(n); return string(raw) }

// build makes Task, its records, the graphs and the prefabs: Badge (a
// chip), Task row (a checkbox, a title, a when, a Badge, a slot; raises
// toggled; logic on mount, props change and unmount) and Task list (a
// repeat of Task row over records.find, a pinned Badge, handling toggled).
func (st *render) build() (ids []string) {
	st.do("entity.create", "task", map[string]any{"name": "Task", "fields": []map[string]any{
		{"name": "title", "type": "text"}, {"name": "done", "type": "bool"}}})
	st.do("entity.publish", "task", map[string]int{"rev": st.rev(entities.DraftProjector, "entity.task", "task")})
	if _, err := st.p.WaitFor(context.Background(), 1, entities.PublishedProjector, "entity.task", st.seq["task"], 2*time.Second); err != nil {
		st.t.Fatal(err)
	}
	ids = append(ids, st.task("Alpha"), st.task("Beta"))

	cfg := func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	// Task row's toggle raises toggled with the task's id and its new done
	st.graph("taskRow-onToggle", "ui.handler", [][3]any{
		{"event", "ui.onEvent", cfg("outputs", []any{})},
		{"id", "ui.getProp", cfg("path", "task._id")},
		{"done", "ui.getProp", cfg("path", "task.done")},
		{"flip", "data.expression", cfg("expr", "!(done ?? false)", "type", "bool")},
		{"raise", "ui.raiseEvent", cfg("event", "toggled", "inputs", []any{cfg("id", "id", "type", "string"), cfg("id", "done", "type", "bool")})},
	}, "event.then>raise.then", "id.value>raise.id", "done.value>flip.done", "flip.value>raise.done")
	st.graph("taskRow-onEdit", "ui.handler", [][3]any{
		{"event", "ui.onEvent", cfg("outputs", []any{})},
		{"yes", "data.expression", cfg("expr", "true", "type", "bool")},
		{"set", "ui.setState", cfg("path", "editing")},
	}, "event.then>set.then", "yes.value>set.value")
	st.graph("taskRow-logic", "prefab.logic", [][3]any{
		{"mount", "ui.onMount", cfg()},
		{"props", "ui.onPropsChanged", cfg()},
		{"unmount", "ui.onUnmount", cfg()},
		{"title", "ui.getProp", cfg("path", "task.title")},
		{"n1", "test.note", cfg("label", "mount")},
		{"n2", "test.note", cfg("label", "props")},
		{"n3", "test.note", cfg("label", "unmount")},
		{"params", "data.expression", cfg("expr", `{entity: "task"}`, "type", "json")},
		{"sub", "ui.subscribe", cfg("path", "others")},
	}, "mount.then>n1.then", "n1.then>sub.then", "params.value>sub.params", "props.then>n2.then", "unmount.then>n3.then",
		"title.value>n1.value", "title.value>n2.value", "title.value>n3.value")
	// Task list's handler for toggled sends record.update
	st.graph("taskList-onToggled", "ui.handler", [][3]any{
		{"event", "ui.onEvent", cfg("outputs", []any{cfg("id", "id", "type", "string"), cfg("id", "done", "type", "bool")})},
		{"note", "test.note", cfg("label", "toggled")},
		{"args", "data.expression", cfg("expr", `{entity: "task", set: {done: done}}`, "type", "json")},
		{"send", "live.sendCommand", cfg("command", "record.update")},
	}, "event.then>note.then", "event.id>note.value", "note.then>send.then", "event.id>send.target", "event.done>args.done", "args.value>send.args")
	st.graph("taskList-logic", "prefab.logic", [][3]any{
		{"changed", "ui.onQueryChanged", cfg()},
		{"note", "test.note", cfg("label", "query")},
	}, "changed.then>note.then")

	// Badge, published twice later
	st.do("prefab.create", "badge", map[string]any{"name": "Badge"})
	st.do("prefab.setInterface", "badge", map[string]any{"props": map[string]any{"label": map[string]any{"type": "string", "required": true}}})
	st.insert("badge", "", map[string]any{"type": "mui.chip", "key": "chip", "props": map[string]any{"label": "?", "color": "default"}, "bind": map[string]any{"label": "props.label"}})
	st.publish("badge")

	st.do("prefab.create", "taskRow", map[string]any{"name": "Task row"})
	st.do("prefab.setInterface", "taskRow", map[string]any{
		"props":  map[string]any{"task": map[string]any{"type": "record", "entity": "task", "required": true}},
		"state":  map[string]any{"editing": map[string]any{"type": "bool", "default": false}, "others": map[string]any{"type": "json"}},
		"events": map[string]any{"toggled": map[string]any{"payload": map[string]any{"id": "string", "done": "bool"}}},
		"slots":  map[string]any{"actions": map[string]any{}},
	})
	for name, g := range map[string]string{"onToggle": "taskRow-onToggle", "onEdit": "taskRow-onEdit", "logic": "taskRow-logic"} {
		st.do("prefab.attachGraph", "taskRow", map[string]any{"name": name, "graph": g})
	}
	st.insert("taskRow", "", map[string]any{"type": "mui.stack", "key": "root", "props": map[string]any{"direction": "row", "sx": map[string]any{"gap": 1, "color": "text.secondary"}}})
	st.insert("taskRow", "root", map[string]any{"type": "mui.checkbox", "key": "check", "bind": map[string]any{"checked": "props.task.done"}, "on": map[string]any{"change": map[string]any{"handler": "onToggle"}}})
	st.insert("taskRow", "root", map[string]any{"type": "mui.text", "key": "title", "bind": map[string]any{"text": "props.task.title"}})
	st.insert("taskRow", "root", map[string]any{"type": "mui.button", "key": "edit", "props": map[string]any{"label": "Edit"}, "on": map[string]any{"click": map[string]any{"handler": "onEdit"}}})
	st.insert("taskRow", "root", map[string]any{"type": "when", "key": "editing", "props": map[string]any{"test": "state.editing"},
		"slots": map[string]any{"then": []any{map[string]any{"type": "mui.text", "key": "editor", "props": map[string]any{"text": "Editing"}}}}})
	st.insert("taskRow", "root", map[string]any{"type": "prefab.badge", "key": "badge", "bind": map[string]any{"label": `=props.task.done ? "done" : "open"`}})
	st.insert("taskRow", "root", map[string]any{"type": "mui.text", "key": "count", "bind": map[string]any{"text": `=string(len(state.others?.records ?? []))`}})
	st.insert("taskRow", "root", map[string]any{"type": "slot", "key": "actions", "props": map[string]any{"name": "actions"}})
	st.publish("taskRow")

	st.do("prefab.create", "taskList", map[string]any{"name": "Task list"})
	st.do("prefab.setInterface", "taskList", map[string]any{
		"props":   map[string]any{"heading": map[string]any{"type": "string", "default": "Tasks"}},
		"queries": map[string]any{"tasks": map[string]any{"query": "records.find", "params": map[string]any{"entity": "task", "sort": []any{map[string]any{"field": "title"}}}}},
	})
	st.do("prefab.attachGraph", "taskList", map[string]any{"name": "onToggled", "graph": "taskList-onToggled"})
	st.do("prefab.attachGraph", "taskList", map[string]any{"name": "logic", "graph": "taskList-logic"})
	st.insert("taskList", "", map[string]any{"type": "mui.stack", "key": "root"})
	st.insert("taskList", "root", map[string]any{"type": "mui.heading", "key": "heading", "bind": map[string]any{"text": "props.heading"}})
	st.insert("taskList", "root", map[string]any{"type": "prefab.badge@1", "key": "pinned", "props": map[string]any{"label": "v1"}})
	st.insert("taskList", "root", map[string]any{"type": "repeat", "key": "rows", "props": map[string]any{"each": "query:tasks", "as": "task", "key": "task._id"}})
	st.insert("taskList", "root/rows", map[string]any{"type": "prefab.taskRow", "key": "row", "bind": map[string]any{"task": "task"},
		"on": map[string]any{"toggled": map[string]any{"handler": "onToggled"}}, "slots": map[string]any{"actions": []any{
			map[string]any{"type": "mui.text", "key": "by", "bind": map[string]any{"text": "props.heading"}}}}})
	st.publish("taskList")
	return ids
}

func page() ui.Page {
	p := ui.PrefabPage("taskList", "", map[string]any{"heading": "Our tasks"})
	p.ID, p.App, p.Title = "test/tasks", "test", "Tasks"
	return p
}

func TestAPrefabPageRendersPrimitives(t *testing.T) {
	st := newRender(t)
	ids := st.build()
	ss := st.open(page())
	tree := ss.tree()
	all := text(tree)
	if strings.Contains(all, "ui.error") || strings.Contains(all, "prefab.") || strings.Contains(all, `"repeat"`) || strings.Contains(all, `"when"`) || strings.Contains(all, `"slot"`) {
		t.Fatalf("the tree should hold only primitives: %s", all)
	}
	// The repeat gave a row per task, in order, keyed by the row key
	var keys []string
	for _, c := range tree.Children {
		keys = append(keys, c.Key)
	}
	want := []string{"heading", "pinned", "rows_" + ids[0], "rows_" + ids[1]}
	if !slices.Equal(keys, want) {
		t.Fatalf("the list's children are %v, not %v", keys, want)
	}
	row := tree.Child("rows_" + ids[0])
	if row.Type != "mui.stack" || row.Child("title").Props["text"] != "Alpha" || row.Child("check").Props["checked"] != false {
		t.Errorf("the first row: %s", text(row))
	}
	// The nested Badge read its prop from an expression, the when shows
	// nothing yet, and the slot holds the list's content in its scope
	if row.Child("badge").Type != "mui.chip" || row.Child("badge").Props["label"] != "open" || row.Child("editor") != nil {
		t.Errorf("the row's badge and when: %s", text(row))
	}
	if by := row.Child("actions").Child("by"); by == nil || by.Props["text"] != "Our tasks" {
		t.Errorf("slot content reads the list's props: %s", text(row.Child("actions")))
	}
	if h := tree.Child("heading"); h.Props["text"] != "Our tasks" {
		t.Errorf("the page's props reach the root prefab: %s", text(h))
	}
	// Handlers are the instance's graphs, by its key path
	if h := row.Child("check").On["change"].Handler; h != ui.InstanceHandler("root/rows_"+ids[0], "onToggle") {
		t.Errorf("the checkbox's handler is %q", h)
	}
}

func TestOnlyTheInstanceThatChangedIsPatched(t *testing.T) {
	st := newRender(t)
	ids := st.build()
	ss := st.open(page())
	ss.tree()
	st.note("mount:Alpha")
	st.note("mount:Beta")
	under := func(patches []ui.Patch, prefix string) {
		t.Helper()
		if len(patches) == 0 {
			t.Fatal("no patches")
		}
		for _, p := range patches {
			if len(p.Path) == 0 || p.Path[0] != prefix {
				raw, _ := json.Marshal(patches)
				t.Fatalf("a patch outside %s: %s", prefix, raw)
			}
		}
	}
	// Another user renames Beta: only Beta's row is patched
	st.do("record.update", ids[1], map[string]any{"entity": "task", "set": map[string]any{"title": "Bravo"}})
	under(ss.patches(`"Bravo"`), "rows_"+ids[1])
	st.note("props:Bravo")
	// A state change renders only its instance
	ss.s.Event(user(), ui.InstanceHandler("root/rows_"+ids[0], "onEdit"), []string{"rows_" + ids[0], "edit"}, nil)
	under(ss.patches(`"Editing"`), "rows_"+ids[0])
}

func TestARaisedEventRunsItsParentsHandler(t *testing.T) {
	st := newRender(t)
	ids := st.build()
	ss := st.open(page())
	ss.tree()
	ss.s.Event(user(), ui.InstanceHandler("root/rows_"+ids[0], "onToggle"), []string{"rows_" + ids[0], "check"}, map[string]any{"value": true})
	st.note("toggled:" + ids[0])
	// The list's handler sent record.update: the row shows it done
	ss.patches(`"checked":true`, `"label":"done"`)
}

func TestRepublishingANestedPrefabRendersItsInstances(t *testing.T) {
	st := newRender(t)
	ids := st.build()
	ss := st.open(page())
	ss.tree()
	st.do("prefab.updateNode", "badge", map[string]any{"path": "chip", "props": map[string]any{"color": "primary"}})
	st.publish("badge")
	patches := ss.patches(`"color":"primary"`)
	var paths []string
	for _, p := range patches {
		paths = append(paths, strings.Join(p.Path, "/"))
	}
	for _, id := range ids {
		if !slices.Contains(paths, "rows_"+id+"/badge") {
			t.Errorf("row %s's badge was not drawn again: %v", id, paths)
		}
	}
	if slices.ContainsFunc(paths, func(p string) bool { return strings.HasPrefix(p, "pinned") }) {
		t.Errorf("the pinned badge changed: %v", paths)
	}
}

func TestLifecycleRunsAndSubscriptionsEndWithTheInstance(t *testing.T) {
	st := newRender(t)
	ids := st.build()
	ss := st.open(page())
	ss.tree()
	st.note("mount:Alpha")
	st.note("mount:Beta")
	// Subscribe to records kept every task in the row's state
	ss.patches(`"text":"2"`)
	sub := "root/rows_" + ids[0] + "#sub:others"
	if !slices.Contains(ss.s.Queries(), sub) {
		t.Fatalf("the mount run's subscription is not held: %v", ss.s.Queries())
	}
	// A record change changes the list's query (On query changed) and the
	// row's props (On props changed)
	st.do("record.update", ids[0], map[string]any{"entity": "task", "set": map[string]any{"title": "Able"}})
	st.note("props:Able")
	st.note("query:<nil>")
	// Deleting Alpha unmounts its row: its run sees its last props, and its
	// subscription and query names go
	st.do("record.delete", ids[0], map[string]any{"entity": "task"})
	st.note("unmount:Able")
	deadline := time.Now().Add(2 * time.Second)
	for slices.ContainsFunc(ss.s.Queries(), func(q string) bool { return strings.HasPrefix(q, "root/rows_"+ids[0]) }) {
		if time.Now().After(deadline) {
			t.Fatalf("the unmounted row's subscription is still held: %v", ss.s.Queries())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestStylingAndPreview(t *testing.T) {
	st := newRender(t)
	st.build()
	// An sx with a raw colour is refused, with the tokens as the fix
	res := st.try("prefab.updateNode", "taskRow", map[string]any{"path": "root", "props": map[string]any{"sx": map[string]any{"color": "#ff0000", "width": 300}}})
	if res.Common.Code != 422 || !slices.ContainsFunc(res.Problems, func(p live.Problem) bool {
		return p.Path == "prefab.template.root.props.sx" && strings.Contains(p.Message, "palette role") && strings.Contains(p.Fix, "text.secondary")
	}) || !slices.ContainsFunc(res.Problems, func(p live.Problem) bool { return strings.Contains(p.Message, "sx.width is not a theme token") }) {
		t.Errorf("a raw sx was not refused: %d %+v", res.Common.Code, res.Problems)
	}
	if problems := ui.Validate(ui.Catalog(st.reg), ui.N("mui.box", "b").P("sx", map[string]any{"bgcolor": "red"})); len(problems) != 1 || problems[0].Fix == "" {
		t.Errorf("ui.Validate should refuse a raw sx: %+v", problems)
	}

	// prefab.preview: a card holding a prefab only drafted, and a Badge
	st.do("prefab.create", "draftOnly", map[string]any{"name": "Draft only"})
	st.insert("draftOnly", "", map[string]any{"type": "mui.text", "key": "t", "props": map[string]any{"text": "x"}})
	st.do("prefab.create", "card", map[string]any{"name": "Card"})
	st.do("prefab.setInterface", "card", map[string]any{"props": map[string]any{"name": map[string]any{"type": "string"}}})
	st.do("prefab.setPreview", "card", map[string]any{"props": map[string]any{"name": "Ada"}})
	st.insert("card", "", map[string]any{"type": "mui.card", "key": "root"})
	st.insert("card", "root", map[string]any{"type": "prefab.badge", "key": "who", "bind": map[string]any{"label": "props.name"}})
	st.insert("card", "root", map[string]any{"type": "prefab.draftOnly", "key": "inner"})
	st.rev(prefabs.DraftProjector, "prefab.card", "card")
	tree, problems, fail := ui.Preview(user(), ui.Options{Catalog: ui.Catalog(st.reg), Subs: st.subs}, "card", "", nil, true)
	if fail != nil {
		t.Fatal(fail.Message)
	}
	if who := tree.Child("who"); who == nil || who.Props["label"] != "Ada" {
		t.Errorf("the preview data should reach the badge: %s", text(tree))
	}
	if len(problems) != 1 || strings.Join(problems[0].Path, "/") != "root/inner" || !strings.Contains(problems[0].Message, "no published prefab") {
		t.Errorf("the preview's problems: %+v", problems)
	}
	// A required prop bound to nothing at run time is a problem in place
	tree, problems, _ = ui.Preview(user(), ui.Options{Catalog: ui.Catalog(st.reg), Subs: st.subs}, "card", "", map[string]any{}, true)
	if len(problems) != 2 || strings.Join(problems[0].Path, "/") != "root/who" || !strings.Contains(problems[0].Message, "needs the prop label") {
		t.Errorf("without a name the badge has a problem: %+v %s", problems, text(tree))
	}
}

func TestTheDesignPageDrawsTheDraftDry(t *testing.T) {
	st := newRender(t)
	ids := st.build()
	st.do("prefab.setPreview", "taskList", map[string]any{"props": map[string]any{"heading": "Preview"}})
	st.rev(prefabs.DraftProjector, "prefab.taskList", "taskList")
	ss := st.openWith(ui.DesignPage(), json.RawMessage(`{"prefab":"taskList"}`))
	tree := ss.tree()
	// Every node drawn from the draft's template carries its key path; a
	// nested prefab's nodes are its instance's, and slot content its own
	row := tree.Child("rows_" + ids[0])
	for _, c := range []struct {
		n    *ui.Node
		want string
	}{{tree, "root"}, {tree.Child("heading"), "root/heading"}, {tree.Child("pinned"), "root/pinned"}, {row, "root/rows/row"},
		{row.Child("title"), ""}, {row.Child("actions").Child("by"), "root/rows/row/by"}} {
		if c.n == nil || c.n.Src != c.want {
			t.Errorf("want src %q: %s", c.want, text(c.n))
		}
	}
	// The shared preview data are its props, and a change to them lands
	if h := tree.Child("heading"); h.Props["text"] != "Preview" {
		t.Errorf("the preview data should be the props: %s", text(h))
	}
	st.do("prefab.setPreview", "taskList", map[string]any{"props": map[string]any{"heading": "Again"}})
	ss.patches(`"Again"`)
	// A handler runs dry: the command it sends comes back as an action
	ss.s.Event(user(), ui.InstanceHandler("root/rows_"+ids[0], "onToggle"), []string{"rows_" + ids[0], "check"}, map[string]any{"value": true})
	st.note("toggled:" + ids[0])
	ss.patches(`"type":"dryRun"`, `"name":"record.update"`, ids[0])
	raw, _ := json.Marshal(map[string]string{"entity": "task", "id": ids[0]})
	data, _, fail := st.subs.Once(user(), "records.get", raw)
	if fail != nil {
		t.Fatal(fail.Message)
	}
	if got, _ := json.Marshal(data); strings.Contains(string(got), `"done":true`) {
		t.Errorf("a dry run committed: %s", got)
	}
}

// A prefab whose root is another prefab, inserted while the page is open,
// is drawn once that prefab's definition arrives
func TestARootInstanceAddedLaterIsDrawn(t *testing.T) {
	st := newRender(t)
	st.build()
	st.do("prefab.create", "wrapper", map[string]any{"name": "Wrapper"})
	st.rev(prefabs.DraftProjector, "prefab.wrapper", "wrapper")
	ss := st.openWith(ui.DesignPage(), json.RawMessage(`{"prefab":"wrapper"}`))
	ss.tree()
	st.insert("wrapper", "", map[string]any{"type": "prefab.badge", "key": "badge", "props": map[string]any{"label": "later"}})
	ss.patches(`"type":"mui.chip"`, `"later"`)
}
