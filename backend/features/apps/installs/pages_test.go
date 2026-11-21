package installs_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/ui"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
)

// publishPrefab publishes a prefab at its draft's rev.
func (f *fixture) publishPrefab(id string, seq uint64) {
	f.t.Helper()
	var d struct{ Rev int }
	_ = json.Unmarshal(f.wait(prefabs.DraftProjector, "prefab."+id, seq).Value, &d)
	res := f.ok("prefab.publish", id, map[string]any{"rev": d.Rev, "allowBreaking": true})
	f.wait(prefabs.PublishedProjector, "prefab."+id, res.Seq)
}

// publishRest publishes the Tasks app's entity type and graphs.
func (f *fixture) publishRest() {
	f.t.Helper()
	res := f.ok("entity.publish", "task", map[string]any{"rev": 1})
	f.wait(entities.PublishedProjector, "entity.task", res.Seq)
	f.publishGraph("cmd-tasks-task-reassign", reassign(""))
	f.publishGraph("qry-tasks-myTasks", myTasks(1))
}

// openGraph is My tasks' onOpen handler: it navigates to a task.
func openGraph(id string) map[string]any {
	return map[string]any{"name": "Open a task", "kind": uinodes.KindHandler, "owner": "app:tasks",
		"nodes": []any{node("event", uinodes.OnEvent, nil), node("go", "ui.navigate", map[string]any{"route": "/task/" + id})},
		"edges": edges("event.then -> go.then")}
}

// page is an open session of an app page and the frames it was sent.
type page struct {
	t      *testing.T
	s      *ui.Session
	frames chan any
	tree   *ui.Node
	title  string
	sub    *ui.Subject
	errs   []ui.ErrorFrame
}

func (f *fixture) open(id string, params any) *page {
	f.t.Helper()
	src := ui.AppSources.All(f.reg)[0]
	pg, mine, err := src.Page(args(), id)
	if !mine || err != nil {
		f.t.Fatalf("%s: mine %v, %v", id, mine, err)
	}
	raw, _ := json.Marshal(params)
	p := &page{t: f.t, frames: make(chan any, 256)}
	ctx, cancel := context.WithCancel(context.Background())
	f.t.Cleanup(cancel)
	a := args()
	a.Ctx = ctx
	runner := uinodes.Runner{Deps: func() uinodes.Deps { return uinodes.Deps{Engine: f.eng} }}
	s, fail := ui.Open(a, "s-"+strings.ReplaceAll(id, "/", "-"), pg, raw, nil, ui.Options{Catalog: ui.Catalog(f.reg), Subs: f.subs, Graphs: runner,
		Send: func(fr any) { p.frames <- fr }})
	if fail != nil {
		f.t.Fatal(fail.Message)
	}
	f.t.Cleanup(func() { s.Close(true) })
	p.s = s
	return p
}

// until reads frames until the page's tree, title or errors satisfy ok.
func (p *page) until(what string, ok func(p *page) bool) {
	p.t.Helper()
	deadline := time.After(5 * time.Second)
	for !ok(p) {
		select {
		case fr := <-p.frames:
			switch fr := fr.(type) {
			case ui.TreeFrame:
				var tree *ui.Node
				raw, _ := json.Marshal(fr.Tree)
				_ = json.Unmarshal(raw, &tree)
				p.tree, p.title, p.sub = tree, fr.Title, fr.Subject
			case ui.PatchFrame:
				// A copy: the session's tree shares the patches' nodes
				var patches []ui.Patch
				raw, _ := json.Marshal(fr.Patches)
				_ = json.Unmarshal(raw, &patches)
				for _, pt := range patches {
					p.tree = ui.ApplyPatch(p.tree, pt)
				}
				if fr.Title != "" || fr.Subject != nil {
					p.title, p.sub = fr.Title, fr.Subject
				}
			case ui.ErrorFrame:
				p.errs = append(p.errs, fr)
			}
		case <-deadline:
			raw, _ := json.Marshal(p.tree)
			p.t.Fatalf("never %s: title %q, errors %+v, tree %s", what, p.title, p.errs, raw)
		}
	}
}

func (p *page) shows(text string) bool {
	raw, _ := json.Marshal(p.tree)
	return strings.Contains(string(raw), text)
}

// event sends the event of the node keyed key, as the browser would.
func (p *page) event(key, event string, payload map[string]any) {
	p.t.Helper()
	var path []string
	var handler string
	p.tree.Walk(func(at []string, n *ui.Node) {
		if n.Key == key {
			path, handler = at, n.On[event].Handler
		}
	})
	if handler == "" {
		p.t.Fatalf("no %s on %s", event, key)
	}
	p.s.Event(args(), handler, path, payload)
}

func TestAppRuntime(t *testing.T) {
	f := setup(t)
	f.tasks(false)
	f.publishGraph("myTasks-onOpen", openGraph("t1"))
	// My tasks shows its version and opens a task; Task detail shows its id
	f.ok("prefab.insertNode", "myTasks", map[string]any{"node": map[string]any{"type": "mui.stack", "key": "root"}})
	f.ok("prefab.insertNode", "myTasks", map[string]any{"parentPath": "root", "node": map[string]any{"type": "mui.heading", "key": "h", "props": map[string]any{"text": "Mine v1"}}})
	f.ok("prefab.attachGraph", "myTasks", map[string]any{"name": "onOpen", "graph": "myTasks-onOpen"})
	seq := f.ok("prefab.insertNode", "myTasks", map[string]any{"parentPath": "root", "node": map[string]any{"type": "mui.button", "key": "open",
		"props": map[string]any{"label": "Open"}, "on": map[string]any{"click": map[string]any{"handler": "onOpen"}}}}).Seq
	f.publishPrefab("myTasks", seq)
	seq = f.ok("prefab.insertNode", "taskDetail", map[string]any{"node": map[string]any{"type": "mui.text", "key": "id", "bind": map[string]any{"text": `="Task " + props.id`}}}).Seq
	f.publishPrefab("taskDetail", seq)
	// The default frame, as Forge's starter App frame declares it
	f.ok("prefab.create", "appFrame", map[string]any{"name": "App frame"})
	f.ok("prefab.setInterface", "appFrame", map[string]any{
		"props": map[string]any{"title": map[string]any{"type": "string"}, "nav": map[string]any{"type": "json"}, "selected": map[string]any{"type": "string"},
			"subject": map[string]any{"type": "string"}, "canBack": map[string]any{"type": "bool"}},
		"slots": map[string]any{"page": map[string]any{}}})
	f.ok("prefab.insertNode", "appFrame", map[string]any{"node": map[string]any{"type": "mui.stack", "key": "root"}})
	f.ok("prefab.insertNode", "appFrame", map[string]any{"parentPath": "root", "node": map[string]any{"type": "mui.list", "key": "nav",
		"props": map[string]any{"items": []any{}}, "bind": map[string]any{"items": "props.nav", "selected": "props.selected"}}})
	f.ok("prefab.insertNode", "appFrame", map[string]any{"parentPath": "root", "node": map[string]any{"type": "mui.heading", "key": "title",
		"props": map[string]any{"text": ""}, "bind": map[string]any{"text": `=props.title + (props.canBack ? " ‹" : "") + " @" + props.subject`}}})
	seq = f.ok("prefab.insertNode", "appFrame", map[string]any{"parentPath": "root", "node": map[string]any{"type": "slot", "key": "page", "props": map[string]any{"name": "page"}}}).Seq
	f.publishPrefab("appFrame", seq)
	f.publishRest()
	f.ok("app.updateEditorKind", "tasks", map[string]any{"kind": "task", "subject": map[string]any{"type": "task", "label": "Task {record.title}"}})
	seq = f.ok("app.setNav", "tasks", map[string]any{"nav": []any{map[string]any{"title": "My tasks", "route": "/mine", "icon": "person"}}}).Seq
	f.publish(seq)
	f.lifecycle("app.install", "tasks", map[string]any{})
	f.ok("record.create", "t1", map[string]any{"entity": "task", "values": map[string]any{"title": "Write docs"}})

	// The Apps section's app and kinds, with who may use it
	src := ui.AppSources.All(f.reg)[0]
	eventually(t, "the app listed", func() bool { a, _ := src.Apps(args()); return len(a) > 0 })
	list, kinds := src.Apps(args())
	if len(list) != 1 || list[0].ID != "tasks" || list[0].Title != "Tasks" || len(list[0].Reach) != 1 || list[0].Reach[0].Resource != "app:tasks/entity:task" {
		t.Fatalf("apps = %+v", list)
	}
	if !slices.ContainsFunc(kinds, func(k ui.EditorKind) bool { return k.ID == "app:tasks:main" && k.Page == "tasks/main" }) ||
		!slices.ContainsFunc(kinds, func(k ui.EditorKind) bool { return k.ID == "app:tasks:task" && k.Page == "tasks/task" }) {
		t.Fatalf("kinds = %+v", kinds)
	}
	if _, mine, _ := src.Page(args(), "hello/main"); mine {
		t.Error("a page of an app never installed here is the source's")
	}

	// Published again after the install: the app keeps what it pinned
	seq = f.ok("prefab.updateNode", "myTasks", map[string]any{"path": "root/h", "props": map[string]any{"text": "Mine v2"}}).Seq
	f.publishPrefab("myTasks", seq)
	f.publishGraph("myTasks-onOpen", openGraph("t2"))

	// The main tab: the frame around the home page, at the pinned version
	main := f.open("tasks/main", map[string]any{})
	main.until("showing My tasks", func(p *page) bool { return p.shows("Mine v1") && p.shows("My tasks @app_tasks.mine") })
	if main.shows("Mine v2") || main.title != "Tasks" || main.sub == nil || main.sub.Object != "app_tasks.mine" || main.sub.Type != "appPage" {
		t.Fatalf("main: title %q subject %+v", main.title, main.sub)
	}
	// The page's graph, as the version copied it, navigates to t1 (not t2)
	main.event("open", "click", nil)
	main.until("showing task t1", func(p *page) bool { return p.shows("Task t1") })
	if main.sub.Object != "record_task.t1" || main.sub.ID != "task.t1" || !main.shows(" ‹ @record_task.t1") {
		t.Errorf("on /task/t1: subject %+v", main.sub)
	}
	// Back and forward, the tab's own history
	main.s.Event(args(), "app.back", nil, nil)
	main.until("back on My tasks", func(p *page) bool { return p.shows("Mine v1") && !p.shows("Task t1") })
	main.s.Event(args(), "app.forward", nil, nil)
	main.until("forward on t1", func(p *page) bool { return p.shows("Task t1") })
	// A frame's navigate; a route the app lacks changes nothing
	main.s.Event(args(), "app.navigate", nil, map[string]any{"route": "/task/t9"})
	main.until("on t9", func(p *page) bool { return p.shows("Task t9") })
	main.s.Event(args(), "app.navigate", nil, map[string]any{"route": "/nowhere"})
	main.s.Event(args(), "app.navigate", nil, map[string]any{"route": "/mine"})
	main.until("on My tasks", func(p *page) bool { return p.shows("Mine v1") })
	// A deep link opens at its route
	deep := f.open("tasks/main", map[string]any{"route": "/task/t1"})
	deep.until("deep link on t1", func(p *page) bool { return p.shows("Task t1") })

	// An editor kind's tab: the route's params as props, the title the
	// manifest's template over its record, following it
	tab := f.open("tasks/task", map[string]any{"id": "t1"})
	tab.until("titled by its record", func(p *page) bool { return p.shows("Task t1") && p.title == "Write docs" })
	if tab.sub == nil || tab.sub.Label != "Task Write docs" || tab.sub.Object != "record_task.t1" {
		t.Errorf("tab subject %+v", tab.sub)
	}
	f.ok("record.update", "t1", map[string]any{"entity": "task", "set": map[string]any{"title": "Write the docs"}})
	tab.until("retitled", func(p *page) bool { return p.title == "Write the docs" && p.sub.Label == "Task Write the docs" })

	// An upgrade: open sessions reopen at their next event, and a kind it
	// removed is gone
	f.ok("app.removeEditorKind", "tasks", map[string]any{"kind": "task"})
	seq = f.ok("app.setInfo", "tasks", map[string]any{"description": "v2"}).Seq
	f.publish(seq)
	f.lifecycle("app.upgrade", "tasks", map[string]any{})
	// The runtime reads installs from a watch, a moment behind the projection
	eventually(t, "the removed kind gone from the list", func() bool {
		_, kinds := src.Apps(args())
		return !slices.ContainsFunc(kinds, func(k ui.EditorKind) bool { return k.ID == "app:tasks:task" })
	})
	main.s.Event(args(), "app.back", nil, nil)
	main.until("told to reopen", func(p *page) bool {
		return slices.ContainsFunc(p.errs, func(e ui.ErrorFrame) bool { return e.ErrorCode == "page_changed" })
	})
	if _, _, err := src.Page(args(), "tasks/task"); err == nil || !strings.Contains(err.Error(), "no editor kind task") {
		t.Errorf("a removed kind: %v", err)
	}
	// Uninstalled: its pages are gone, not someone else's
	f.lifecycle("app.uninstall", "tasks", map[string]any{})
	eventually(t, "the app gone from the list", func() bool { a, _ := src.Apps(args()); return len(a) == 0 })
	if _, mine, err := src.Page(args(), "tasks/main"); !mine || err == nil {
		t.Errorf("an uninstalled app's page: mine %v, %v", mine, err)
	}
	_ = installs.Projector
}

func TestAppRuntimeWithoutAFrame(t *testing.T) {
	f := setup(t)
	f.tasks(false)
	seq := f.ok("prefab.insertNode", "myTasks", map[string]any{"node": map[string]any{"type": "mui.heading", "key": "h", "props": map[string]any{"text": "Mine"}}}).Seq
	f.publishPrefab("myTasks", seq)
	f.publishPrefab("taskDetail", f.ok("prefab.setInfo", "taskDetail", map[string]any{"description": "A task"}).Seq)
	f.publishRest()
	f.publish(f.ok("app.setInfo", "tasks", map[string]any{"description": "framed by nothing"}).Seq)
	f.lifecycle("app.install", "tasks", map[string]any{})
	src := ui.AppSources.All(f.reg)[0]
	eventually(t, "the app listed", func() bool { a, _ := src.Apps(args()); return len(a) > 0 })
	// No App frame in the tenant: the page alone, under the tab's header
	main := f.open("tasks/main", nil)
	main.until("showing My tasks", func(p *page) bool { return p.shows("Mine") })
	if main.tree.Key != "root" || main.tree.Type != "mui.heading" {
		t.Errorf("unframed root: %s %s", main.tree.Type, main.tree.Key)
	}
}
