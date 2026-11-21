package prefabs_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	livetools "github.com/SgtFlame/foundation/backend/features/live/tools"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/SgtFlame/foundation/backend/features/ui"
	uicomponents "github.com/SgtFlame/foundation/backend/features/ui/components"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
	uitools "github.com/SgtFlame/foundation/backend/features/ui/tools"
	"gorm.io/gorm"
)

type fixture struct {
	t    *testing.T
	reg  *extensions.Registry
	bus  *bus.Bus
	subs *subscribe.Subscriptions
	p    *project.Projections
	db   *gorm.DB
	mu   sync.Mutex
	n    int
	seq  map[string]uint64 // each prefab's last commit
}

func setup(t *testing.T) *fixture {
	reg := extensions.NewRegistry()
	cognitionnodes.Contribute(reg)
	uinodes.Contribute(reg)
	if err := uicomponents.Contribute(reg); err != nil {
		t.Fatal(err)
	}
	var (
		draftDeps  drafts.Deps
		entityDeps entities.Deps
		deps       prefabs.Deps
		subs       *subscribe.Subscriptions
		toolDeps   livetools.Deps
	)
	drafts.Contribute(reg, func() drafts.Deps { return draftDeps })
	entities.Contribute(reg, func() entities.Deps { return entityDeps })
	prefabs.Contribute(reg, func() prefabs.Deps { return deps })
	uitools.ContributePrefabs(reg, func() *subscribe.Subscriptions { return subs })
	livetools.Contribute(reg, func() livetools.Deps { return toolDeps })
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
	eng := engine.New(gdb, reg, nil)
	eng.SetSource(drafts.Source{Store: p})
	draftDeps = drafts.Deps{Engine: eng, Projections: p}
	entityDeps = entities.Deps{Projections: p, DB: gdb}
	deps = prefabs.Deps{Projections: p, DB: gdb}
	subs = subscribe.New(p, reg)
	f := &fixture{t: t, reg: reg, bus: bus.New(p), subs: subs, p: p, db: gdb, seq: map[string]uint64{}}
	toolDeps = livetools.Deps{Bus: f.bus, Subscriptions: subs}
	return f
}

func args() features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
}

func (f *fixture) do(typ, target string, a any) bus.Result {
	f.mu.Lock()
	f.n++
	id := fmt.Sprintf("c%d", f.n)
	f.mu.Unlock()
	raw, _ := json.Marshal(a)
	res := f.bus.Execute(bus.Request{ServiceArgs: args(), ID: id, Type: typ, Target: target, Args: raw})
	if res.Common.Code < 300 {
		f.mu.Lock()
		f.seq[res.Target] = max(f.seq[res.Target], res.Seq)
		f.mu.Unlock()
	}
	return res
}

func (f *fixture) ok(typ, target string, a any) bus.Result {
	f.t.Helper()
	res := f.do(typ, target, a)
	if res.Common.Code >= 300 {
		f.t.Fatalf("%s: %s %+v", typ, res.Common.Message, res.Problems)
	}
	return res
}

// rejected sends a command that must be refused with a problem at path
// whose message holds want.
func (f *fixture) rejected(typ, target string, a any, path, want string) {
	f.t.Helper()
	res := f.do(typ, target, a)
	if res.Common.Code != 422 || !slices.ContainsFunc(res.Problems, func(p live.Problem) bool {
		return p.Path == path && strings.Contains(p.Message, want)
	}) {
		f.t.Errorf("%s was not refused at %s with %q: %d %+v", typ, path, want, res.Common.Code, res.Problems)
	}
}

// describe reads a prefab's draft once its last commit is projected.
func (f *fixture) describe(id string) prefabs.View {
	f.t.Helper()
	if _, err := f.p.WaitFor(context.Background(), features.LocalTenantID, prefabs.DraftProjector, "prefab."+id, f.seq[id], 2*time.Second); err != nil {
		f.t.Fatal(err)
	}
	data, _, fail := f.subs.Once(args(), "prefab.describe", json.RawMessage(`{"id":"`+id+`"}`))
	if fail != nil {
		f.t.Fatal(fail.Message)
	}
	raw, _ := json.Marshal(data)
	var v prefabs.View
	_ = json.Unmarshal(raw, &v)
	return v
}

// base makes the Task entity type and the graphs a Task row uses.
func (f *fixture) base() {
	f.ok("entity.create", "task", map[string]any{"name": "Task", "fields": []map[string]any{
		{"name": "title", "type": "text"}, {"name": "done", "type": "bool"}, {"name": "due", "type": "date"}}})
	f.ok("bp.create", "taskRow-onToggle", map[string]any{"kind": "ui.handler", "name": "Toggle a task"})
	f.ok("bp.create", "taskRow-logic", map[string]any{"kind": "prefab.logic", "name": "Task row logic"})
	f.ok("bp.create", "some-function", map[string]any{"kind": "function", "name": "Not a handler"})
}

// taskRow builds Task row by commands alone.
func (f *fixture) taskRow() {
	f.t.Helper()
	res := f.ok("prefab.create", "", map[string]any{"name": "Task row", "description": "One task with a checkbox", "owner": "app:hello"})
	if res.Target != "taskRow" {
		f.t.Fatalf("the id made from the name is %q", res.Target)
	}
	f.ok("prefab.setInterface", "taskRow", map[string]any{
		"props":  map[string]any{"task": map[string]any{"type": "record", "entity": "task", "required": true}},
		"state":  map[string]any{"editing": map[string]any{"type": "bool", "default": false}},
		"events": map[string]any{"toggled": map[string]any{"payload": map[string]any{"id": "string", "done": "bool"}}},
		"slots":  map[string]any{"actions": map[string]any{"description": "Buttons at the row's end"}},
	})
	f.ok("prefab.attachGraph", "taskRow", map[string]any{"name": "onToggle", "graph": "taskRow-onToggle"})
	f.ok("prefab.attachGraph", "taskRow", map[string]any{"name": "logic", "graph": "taskRow-logic"})
	f.ok("prefab.insertNode", "taskRow", map[string]any{"node": map[string]any{"type": "mui.stack", "key": "root", "props": map[string]any{"direction": "row"}}})
	f.ok("prefab.insertNode", "taskRow", map[string]any{"parentPath": "root", "node": map[string]any{"type": "mui.checkbox",
		"bind": map[string]any{"checked": "props.task.done"}, "on": map[string]any{"change": map[string]any{"handler": "onToggle"}}}})
	f.ok("prefab.insertNode", "taskRow", map[string]any{"parentPath": "root", "node": map[string]any{"type": "mui.text", "key": "title", "bind": map[string]any{"text": "props.task.title"}}})
	f.ok("prefab.insertNode", "taskRow", map[string]any{"parentPath": "root", "node": map[string]any{"type": "mui.chip", "key": "due", "props": map[string]any{"label": "due"}, "bind": map[string]any{"label": "props.task.due"}}})
	f.ok("prefab.insertNode", "taskRow", map[string]any{"parentPath": "root", "node": map[string]any{"type": "slot", "key": "actions", "props": map[string]any{"name": "actions"}}})
}

// sink collects a subscription's views.
type sink struct {
	mu    sync.Mutex
	views []prefabs.View
	got   chan struct{}
}

func (s *sink) add(raw []byte) {
	var v prefabs.View
	_ = json.Unmarshal(raw, &v)
	s.mu.Lock()
	s.views = append(s.views, v)
	s.mu.Unlock()
	s.got <- struct{}{}
}
func (s *sink) Snapshot(data any, _ live.Position) { raw, _ := json.Marshal(data); s.add(raw) }
func (s *sink) Change(d live.Delta, _ live.Position) {
	if d.Op == "replace" {
		s.add(d.Value)
	}
}
func (s *sink) Failed(resp features.ServiceResponse) { panic(resp.Message) }

func (s *sink) next(t *testing.T) prefabs.View {
	t.Helper()
	select {
	case <-s.got:
	case <-time.After(3 * time.Second):
		t.Fatal("no change arrived")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.views[len(s.views)-1]
}

func TestTaskRowIsBuiltByCommands(t *testing.T) {
	f := setup(t)
	f.base()
	f.ok("prefab.create", "", map[string]any{"name": "Watched"})
	f.describe("watched")
	s := &sink{got: make(chan struct{}, 64)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if fail := f.subs.Open(subscribe.OpenArgs{ServiceArgs: features.ServiceArgs{Ctx: ctx, Principal: features.LocalPrincipal()}, Query: "prefab.describe", Params: json.RawMessage(`{"id":"watched"}`), Sink: s}); fail != nil {
		t.Fatal(fail.Message)
	}
	if v := s.next(t); v.Rev != 1 {
		t.Fatalf("snapshot at rev %d", v.Rev)
	}
	// A second subscriber sees each change as it lands
	f.ok("prefab.insertNode", "watched", map[string]any{"node": map[string]any{"type": "mui.stack"}})
	if v := s.next(t); v.Rev != 2 || v.Prefab.Template == nil || v.Prefab.Template.Key != "stack" {
		t.Fatalf("after the insert: %+v", v)
	}
	f.ok("prefab.insertNode", "watched", map[string]any{"parentPath": "stack", "node": map[string]any{"type": "mui.checkbox"}})
	f.ok("prefab.insertNode", "watched", map[string]any{"parentPath": "stack", "node": map[string]any{"type": "mui.checkbox"}})
	s.next(t)
	if v := s.next(t); strings.Join(prefabs.Paths(v.Prefab.Template), " ") != "stack stack/checkbox stack/checkbox_2" {
		t.Errorf("keys from the type: %v", prefabs.Paths(v.Prefab.Template))
	}

	f.taskRow()
	v := f.describe("taskRow")
	if got := strings.Join(prefabs.Paths(v.Prefab.Template), " "); got != "root root/checkbox root/title root/due root/actions" {
		t.Errorf("Task row's nodes: %s", got)
	}
	if v.Prefab.Owner != "app:hello" || v.Prefab.Props["task"].Entity != "task" || v.Prefab.Graphs["onToggle"] != "taskRow-onToggle" {
		t.Errorf("Task row: %+v", v.Prefab)
	}

	// Moving, wrapping and unwrapping by key paths
	f.ok("prefab.wrapNodes", "taskRow", map[string]any{"paths": []string{"root/due", "root/title"}, "wrapper": map[string]any{"type": "mui.stack", "key": "texts", "props": map[string]any{"direction": "column"}}})
	if got := strings.Join(prefabs.Paths(f.describe("taskRow").Prefab.Template), " "); got != "root root/checkbox root/texts root/texts/title root/texts/due root/actions" {
		t.Errorf("after wrapping: %s", got)
	}
	f.ok("prefab.moveNode", "taskRow", map[string]any{"path": "root/actions", "parentPath": "root", "index": 0})
	f.ok("prefab.unwrapNode", "taskRow", map[string]any{"path": "root/texts"})
	if got := strings.Join(prefabs.Paths(f.describe("taskRow").Prefab.Template), " "); got != "root root/actions root/checkbox root/title root/due" {
		t.Errorf("after moving and unwrapping: %s", got)
	}
	f.ok("prefab.updateNode", "taskRow", map[string]any{"path": "root/due", "props": map[string]any{"color": "warning", "label": nil}})
	n := f.describe("taskRow").Prefab.Template.Child("due")
	if n.Props["color"] != "warning" || n.Props["label"] != nil || n.Bind["label"] != "props.task.due" {
		t.Errorf("the merged update: %+v", n)
	}
	f.ok("prefab.setPreview", "taskRow", map[string]any{"props": map[string]any{"task": map[string]any{"_id": "1", "title": "Water the plants"}}})
	f.ok("prefab.removeNode", "taskRow", map[string]any{"path": "root/actions"})
	if v := f.describe("taskRow"); v.Prefab.Template.Child("actions") != nil || v.Prefab.Preview.Props["task"] == nil {
		t.Errorf("after removing: %+v", v.Prefab)
	}
}

func TestTwoDesignersEditOneDraft(t *testing.T) {
	f := setup(t)
	f.base()
	f.taskRow()
	// One designer moves a node while another changes a different node's props
	for round := range 5 {
		var wg sync.WaitGroup
		results := make([]bus.Result, 2)
		wg.Go(func() {
			idx := round % 2
			results[0] = f.do("prefab.moveNode", "taskRow", map[string]any{"path": "root/actions", "parentPath": "root", "index": idx})
		})
		wg.Go(func() {
			results[1] = f.do("prefab.updateNode", "taskRow", map[string]any{"path": "root/due", "props": map[string]any{"variant": []string{"filled", "outlined"}[round%2]}})
		})
		wg.Wait()
		for _, r := range results {
			if r.Common.Code >= 300 {
				t.Fatalf("round %d: %s %+v", round, r.Common.Message, r.Problems)
			}
		}
		v := f.describe("taskRow")
		if i := slices.IndexFunc(v.Prefab.Template.Children, func(n *ui.Node) bool { return n.Key == "actions" }); i != round%2 {
			t.Errorf("round %d: the move did not land: actions at %d", round, i)
		}
		if got := v.Prefab.Template.Child("due").Props["variant"]; got != []string{"filled", "outlined"}[round%2] {
			t.Errorf("round %d: the update did not land: %v", round, got)
		}
	}
	// Two updates of different props of one node both land
	var wg sync.WaitGroup
	wg.Go(func() {
		f.ok("prefab.updateNode", "taskRow", map[string]any{"path": "root/due", "props": map[string]any{"color": "error"}})
	})
	wg.Go(func() {
		f.ok("prefab.updateNode", "taskRow", map[string]any{"path": "root/due", "props": map[string]any{"variant": "outlined"}})
	})
	wg.Wait()
	if p := f.describe("taskRow").Prefab.Template.Child("due").Props; p["color"] != "error" || p["variant"] != "outlined" {
		t.Errorf("one of two updates of one node was lost: %v", p)
	}
}

func TestBadChangesAreRefusedWithPaths(t *testing.T) {
	f := setup(t)
	f.base()
	f.taskRow()
	insert := func(node map[string]any) map[string]any { return map[string]any{"parentPath": "root", "node": node} }
	f.rejected("prefab.insertNode", "taskRow", insert(map[string]any{"type": "mui.blink"}), "prefab.template.root/blink.type", "no component")
	f.rejected("prefab.updateNode", "taskRow", map[string]any{"path": "root/title", "props": map[string]any{"variant": "huge"}}, "prefab.template.root/title.props.variant", "mui.text props")
	f.rejected("prefab.updateNode", "taskRow", map[string]any{"path": "root/title", "props": map[string]any{"size": 3}}, "prefab.template.root/title.props.size", "has no prop")
	f.rejected("prefab.insertNode", "taskRow", insert(map[string]any{"type": "mui.text", "key": "title"}), "prefab.template.root/title.key", "used twice")
	f.rejected("prefab.updateNode", "taskRow", map[string]any{"path": "root/title", "bind": map[string]any{"text": "props.owner.name"}}, "prefab.template.root/title.bind.text", "does not declare")
	f.rejected("prefab.updateNode", "taskRow", map[string]any{"path": "root/title", "bind": map[string]any{"text": "session.name"}}, "prefab.template.root/title.bind.text", "not a binding")
	f.rejected("prefab.insertNode", "taskRow", insert(map[string]any{"type": "mui.button", "props": map[string]any{"label": "Edit"}, "on": map[string]any{"click": map[string]any{"handler": "onEdit"}}}),
		"prefab.template.root/button.on.click.handler", "not a graph")
	f.rejected("prefab.attachGraph", "taskRow", map[string]any{"name": "onToggle", "graph": "taskRow-logic"}, "prefab.graphs.onToggle", "handles an event")
	f.rejected("prefab.attachGraph", "taskRow", map[string]any{"name": "other", "graph": "some-function"}, "prefab.graphs.other", "function graph")
	f.rejected("prefab.attachGraph", "taskRow", map[string]any{"name": "other", "graph": "missing"}, "prefab.graphs.other", "no graph")
	f.rejected("prefab.detachGraph", "taskRow", map[string]any{"name": "onToggle"}, "prefab.template.root/checkbox.on.change.handler", "not a graph")
	f.rejected("prefab.insertNode", "taskRow", insert(map[string]any{"type": "slot", "props": map[string]any{"name": "footer"}}), "prefab.template.root/slot.props.name", "no slot")
	f.rejected("prefab.setInterface", "taskRow", map[string]any{"props": map[string]any{"task": map[string]any{"type": "record", "entity": "nope"}}}, "prefab.props.task.entity", "no entity type")
	f.rejected("prefab.setInterface", "taskRow", map[string]any{"props": map[string]any{}}, "prefab.template.root/checkbox.bind.checked", "does not declare")
	f.rejected("prefab.setPreview", "taskRow", map[string]any{"props": map[string]any{"colour": "red"}}, "prefab.preview.props.colour", "no prop")
	f.rejected("prefab.setInfo", "taskRow", map[string]any{"owner": "hello"}, "prefab.owner", "not an owner")
	// A prefab containing itself, directly or through another
	f.rejected("prefab.insertNode", "taskRow", insert(map[string]any{"type": "prefab.taskRow"}), "prefab.template.root/taskRow.type", "contain itself")
	f.ok("prefab.create", "taskList", map[string]any{"name": "Task list"})
	f.ok("prefab.insertNode", "taskList", map[string]any{"node": map[string]any{"type": "mui.stack", "key": "root"}})
	f.rejected("prefab.insertNode", "taskList", insert(map[string]any{"type": "prefab.taskRow"}), "prefab.template.root/taskRow.props.task", "needs the prop task")
	f.ok("prefab.insertNode", "taskList", insert(map[string]any{"type": "prefab.taskRow", "key": "row", "props": map[string]any{"task": map[string]any{"_id": "1"}}}))
	f.rejected("prefab.insertNode", "taskRow", insert(map[string]any{"type": "prefab.taskList"}), "prefab.template.root/taskList.type", "contains taskRow")
	f.rejected("prefab.moveNode", "taskRow", map[string]any{"path": "root", "parentPath": "root/title"}, "args.path", "root cannot be moved")
	f.rejected("prefab.updateNode", "taskRow", map[string]any{"path": "root/nothing", "props": map[string]any{"a": 1}}, "args.path", "no node")
	if v := f.describe("taskRow"); len(v.Prefab.Template.Children) != 4 {
		t.Errorf("a refused command changed the draft: %v", prefabs.Paths(v.Prefab.Template))
	}
}

func TestPublishVersionsAndBreakingChanges(t *testing.T) {
	f := setup(t)
	f.base()
	f.taskRow()
	rev := f.describe("taskRow").Rev
	f.ok("prefab.updateNode", "taskRow", map[string]any{"path": "root/due", "props": map[string]any{"color": "warning"}})
	f.rejected("prefab.publish", "taskRow", map[string]any{"rev": rev}, "args.rev", "changed since")
	res := f.ok("prefab.publish", "taskRow", map[string]any{"rev": rev + 1})
	if v := f.describe("taskRow"); v.Published != 1 || v.Changed {
		t.Errorf("after publishing: %+v", v)
	}
	// Version 1 reaches the tenant database with its sequence
	var row ui.PrefabVersion
	deadline := time.Now().Add(3 * time.Second)
	for f.db.Where("prefab_id = ? AND version = 1", "taskRow").First(&row).Error != nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if row.Seq != res.Seq || row.Name != "Task row" || !strings.Contains(row.Definition, `"props.task.done"`) {
		t.Fatalf("version 1 in the database: %+v (seq %d)", row, res.Seq)
	}
	var last ui.PrefabRow
	if err := f.db.Where("prefab_id = ?", "taskRow").First(&last).Error; err != nil || last.Version != 1 {
		t.Errorf("ui_prefabs: %+v %v", last, err)
	}
	data, _, fail := f.subs.Once(args(), "prefab.describe", json.RawMessage(`{"id":"taskRow","version":1}`))
	if fail != nil || !strings.Contains(fmt.Sprint(data), "Task row") {
		t.Errorf("version 1 by describe: %v %v", data, fail)
	}

	// Task list uses Task row's task prop; removing it is refused
	f.ok("prefab.create", "taskList", map[string]any{"name": "Task list"})
	f.ok("prefab.insertNode", "taskList", map[string]any{"node": map[string]any{"type": "mui.stack", "key": "root"}})
	f.ok("prefab.insertNode", "taskList", map[string]any{"parentPath": "root", "node": map[string]any{"type": "prefab.taskRow", "key": "row", "props": map[string]any{"task": map[string]any{"_id": "1"}}}})
	f.ok("prefab.publish", "taskList", map[string]any{"rev": f.describe("taskList").Rev})

	item := map[string]any{"type": "record", "entity": "task"}
	f.ok("prefab.setInterface", "taskRow", map[string]any{"props": map[string]any{"task": map[string]any{"type": "record", "entity": "task"}, "item": item}})
	f.ok("prefab.updateNode", "taskRow", map[string]any{"path": "root/checkbox", "bind": map[string]any{"checked": "props.item.done"}})
	f.ok("prefab.updateNode", "taskRow", map[string]any{"path": "root/title", "bind": map[string]any{"text": "props.item.title"}})
	f.ok("prefab.updateNode", "taskRow", map[string]any{"path": "root/due", "bind": map[string]any{"label": "props.item.due"}})
	f.ok("prefab.setPreview", "taskRow", map[string]any{"props": map[string]any{}})
	f.ok("prefab.setInterface", "taskRow", map[string]any{"props": map[string]any{"item": item}})
	rev = f.describe("taskRow").Rev
	res = f.do("prefab.publish", "taskRow", map[string]any{"rev": rev})
	if res.Common.Code != 422 || res.Common.ErrorCode != "prefab_breaking" || len(res.Problems) != 1 ||
		res.Problems[0].Path != "prefab.props.task" || !strings.Contains(res.Problems[0].Message, "taskList at template.root/row") {
		t.Fatalf("a breaking publish: %d %s %+v", res.Common.Code, res.Common.ErrorCode, res.Problems)
	}
	f.ok("prefab.publish", "taskRow", map[string]any{"rev": rev, "allowBreaking": true})
	if v := f.describe("taskRow"); v.Published != 2 {
		t.Errorf("allowBreaking did not publish: %+v", v)
	}
	// A change no published prefab uses is not breaking
	f.ok("prefab.removeNode", "taskRow", map[string]any{"path": "root/actions"})
	f.ok("prefab.setInterface", "taskRow", map[string]any{"slots": map[string]any{}})
	f.ok("prefab.publish", "taskRow", map[string]any{"rev": f.describe("taskRow").Rev})
}

func TestListFindsTheLibrary(t *testing.T) {
	f := setup(t)
	f.base()
	f.taskRow()
	f.ok("prefab.create", "", map[string]any{"name": "Page header", "description": "A title with actions"})
	f.describe("pageHeader")
	list := func(params string) []string {
		data, _, fail := f.subs.Once(args(), "prefab.list", json.RawMessage(params))
		if fail != nil {
			t.Fatal(fail.Message)
		}
		raw, _ := json.Marshal(data)
		var byKey map[string]prefabs.Summary
		_ = json.Unmarshal(raw, &byKey)
		var out []string
		for k := range byKey {
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}
	if got := fmt.Sprint(list(`{}`)); got != "[prefab.pageHeader prefab.taskRow]" {
		t.Errorf("all: %s", got)
	}
	if got := fmt.Sprint(list(`{"owner":"app:hello"}`)); got != "[prefab.taskRow]" {
		t.Errorf("app:hello's: %s", got)
	}
	if got := fmt.Sprint(list(`{"owner":"tenant"}`)); got != "[prefab.pageHeader]" {
		t.Errorf("the tenant library: %s", got)
	}
	if got := fmt.Sprint(list(`{"search":"CHECKBOX"}`)); got != "[prefab.taskRow]" {
		t.Errorf("search: %s", got)
	}
}

// tool runs a tool as a chat model's call would: its input checked, then
// run as the local principal.
func (f *fixture) tool(name string, input any) tools.Result {
	f.t.Helper()
	for _, tool := range tools.Tools.All(f.reg) {
		if tool.ID != name {
			continue
		}
		raw, _ := json.Marshal(input)
		if err := tools.CheckArgs(tool.Input, raw); err != nil {
			f.t.Fatalf("%s: %v", name, err)
		}
		res, err := tool.Run(tools.Call{ServiceArgs: args(), Name: name, Args: raw})
		if err != nil {
			f.t.Fatalf("%s: %v %v", name, err, res.Content)
		}
		return res
	}
	f.t.Fatalf("there is no tool %s", name)
	return tools.Result{}
}

// TestTheChatBuildsTaskRow runs the tools a chat model gets: the prefab.*
// commands' own tools, then prefab.describe.
func TestTheChatBuildsTaskRow(t *testing.T) {
	f := setup(t)
	f.base()
	run := f.tool
	res := run("prefab.create", map[string]any{"args": map[string]any{"name": "Task row"}})
	if res.Content["target"] != "taskRow" {
		t.Fatalf("prefab.create: %v", res.Content)
	}
	run("prefab.setInterface", map[string]any{"target": "taskRow", "args": map[string]any{"props": map[string]any{"task": map[string]any{"type": "record", "entity": "task"}}}})
	run("prefab.attachGraph", map[string]any{"target": "taskRow", "args": map[string]any{"name": "onToggle", "graph": "taskRow-onToggle"}})
	run("prefab.insertNode", map[string]any{"target": "taskRow", "args": map[string]any{"node": map[string]any{"type": "mui.stack"}}})
	res = run("prefab.insertNode", map[string]any{"target": "taskRow", "args": map[string]any{"parentPath": "stack", "node": map[string]any{"type": "mui.checkbox",
		"bind": map[string]any{"checked": "props.task.done"}, "on": map[string]any{"change": map[string]any{"handler": "onToggle"}}}}})
	report, _ := json.Marshal(res.Content["report"])
	if !strings.Contains(string(report), `"nodes":["stack","stack/checkbox"]`) || !strings.Contains(string(report), `"problems":[]`) {
		t.Errorf("the report: %s", report)
	}
	res = run("prefab.describe", map[string]any{"id": "taskRow"})
	raw, _ := json.Marshal(res.Content)
	if !strings.Contains(string(raw), `"handler":"onToggle"`) {
		t.Errorf("prefab.describe: %s", raw)
	}
	if res := run("prefab.list", map[string]any{}); res.Summary != "1 prefabs" {
		t.Errorf("prefab.list: %s", res.Summary)
	}
}

// prefab.import replaces a whole draft, or makes one, and is checked like
// any other command; presence: bindings and props expressions in query
// params are checked too (forge 04e).
func TestImportReplacesTheDraft(t *testing.T) {
	f := setup(t)
	f.base()
	def := map[string]any{"name": "Presence", "props": map[string]any{"object": map[string]any{"type": "string", "required": true}},
		"queries":  map[string]any{"one": map[string]any{"query": "prefab.describe", "bind": map[string]any{"id": "=props.object + \"Row\""}}},
		"template": map[string]any{"type": "presence.avatars", "key": "root", "bind": map[string]any{"users": "presence:props.object"}}}
	res := f.ok("prefab.import", "", map[string]any{"prefab": def})
	if res.Target != "presence" {
		t.Fatalf("the id made from the imported name is %q", res.Target)
	}
	if v := f.describe("presence"); v.Prefab.Template == nil || v.Prefab.Template.Type != "presence.avatars" {
		t.Fatalf("imported: %+v", v.Prefab)
	}
	// Importing the same definition changes nothing
	if again := f.ok("prefab.import", "presence", map[string]any{"prefab": def}); again.Seq != res.Seq {
		t.Errorf("an unchanged import committed seq %d after %d", again.Seq, res.Seq)
	}
	def["name"] = "Who is here"
	f.ok("prefab.import", "presence", map[string]any{"prefab": def})
	if v := f.describe("presence"); v.Prefab.Name != "Who is here" {
		t.Errorf("name after import: %q", v.Prefab.Name)
	}
	bad := map[string]any{"name": "Bad", "template": map[string]any{"type": "presence.avatars", "key": "root", "bind": map[string]any{"users": "presence:props.nothing"}}}
	f.rejected("prefab.import", "bad", map[string]any{"prefab": bad}, "prefab.template.root.bind.users", "does not declare")
	bad = map[string]any{"name": "Bad", "queries": map[string]any{"one": map[string]any{"query": "prefab.describe", "bind": map[string]any{"id": "=state.x"}}}}
	f.rejected("prefab.import", "bad", map[string]any{"prefab": bad}, "prefab.queries.one.bind.id", "reads state")
}
