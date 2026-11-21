package manifests_test

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
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	apptools "github.com/SgtFlame/foundation/backend/features/apps/tools"
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
	livetools "github.com/SgtFlame/foundation/backend/features/live/tools"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/SgtFlame/foundation/backend/features/ui"
	uicomponents "github.com/SgtFlame/foundation/backend/features/ui/components"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
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
	seq  map[string]uint64 // each target's last commit
}

func setup(t *testing.T) *fixture {
	reg := extensions.NewRegistry()
	cognitionnodes.Contribute(reg)
	uinodes.Contribute(reg)
	livenodes.Contribute(reg, func() livenodes.Deps { return livenodes.Deps{} })
	if err := uicomponents.Contribute(reg); err != nil {
		t.Fatal(err)
	}
	var (
		draftDeps  drafts.Deps
		entityDeps entities.Deps
		prefabDeps prefabs.Deps
		deps       manifests.Deps
		subs       *subscribe.Subscriptions
		toolDeps   livetools.Deps
	)
	drafts.Contribute(reg, func() drafts.Deps { return draftDeps })
	entities.Contribute(reg, func() entities.Deps { return entityDeps })
	prefabs.Contribute(reg, func() prefabs.Deps { return prefabDeps })
	manifests.Contribute(reg, func() manifests.Deps { return deps })
	apptools.Contribute(reg, func() *subscribe.Subscriptions { return subs })
	livetools.Contribute(reg, func() livetools.Deps { return toolDeps })
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
	draftDeps = drafts.Deps{Engine: eng, Projections: p}
	entityDeps = entities.Deps{Projections: p, DB: gdb}
	prefabDeps = prefabs.Deps{Projections: p, DB: gdb}
	deps = manifests.Deps{Projections: p, DB: gdb}
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

// describe reads an app's draft once its last commit is projected.
func (f *fixture) describe(id string) manifests.View {
	f.t.Helper()
	if _, err := f.p.WaitFor(context.Background(), features.LocalTenantID, manifests.DraftProjector, "app."+id, f.seq[id], 2*time.Second); err != nil {
		f.t.Fatal(err)
	}
	data, _, fail := f.subs.Once(args(), "app.describe", json.RawMessage(`{"id":"`+id+`"}`))
	if fail != nil {
		f.t.Fatal(fail.Message)
	}
	raw, _ := json.Marshal(data)
	var v manifests.View
	_ = json.Unmarshal(raw, &v)
	return v
}

// parts makes what the Tasks app names: its entity type, its pages'
// prefabs and its command and query graphs, all owned by app:tasks.
func (f *fixture) parts() {
	own := "app:tasks"
	f.ok("entity.create", "task", map[string]any{"name": "Task", "owner": own, "fields": []map[string]any{
		{"name": "title", "type": "text"}, {"name": "done", "type": "bool"}, {"name": "assignee", "type": "relation", "relation": map[string]any{"entity": "user"}}}})
	for _, id := range []string{"myTasks", "allTasks"} {
		f.ok("prefab.create", id, map[string]any{"name": id, "owner": own})
	}
	f.ok("prefab.create", "taskDetail", map[string]any{"name": "Task detail", "owner": own})
	f.ok("prefab.setInterface", "taskDetail", map[string]any{"props": map[string]any{"id": map[string]any{"type": "string", "required": true}}})
	f.ok("bp.create", "cmd-task-reassign", map[string]any{"kind": "command", "name": "Reassign a task", "owner": own, "spec": map[string]any{"command": "task.reassign",
		"title": "Reassign", "description": "Gives a task to someone else", "area": "rec", "targetType": "task", "projection": "records.record", "action": "record.update"}})
	f.ok("bp.create", "qry-myTasks", map[string]any{"kind": "data.query", "name": "My tasks", "owner": own, "spec": map[string]any{"query": "myTasks",
		"title": "My tasks", "description": "My open tasks", "action": "record.read"}})
	f.ok("bp.create", "tasks-handler", map[string]any{"kind": "ui.handler", "name": "Not a command", "owner": own})
	// Another app's prefab, and one of the tenant library
	f.ok("prefab.create", "crmCard", map[string]any{"name": "CRM card", "owner": "app:crm"})
	f.ok("prefab.create", "pageHeader", map[string]any{"name": "Page header"})
	f.ok("entity.create", "customer", map[string]any{"name": "Customer", "owner": "app:crm"})
}

// tasks builds the Tasks manifest by commands alone.
func (f *fixture) tasks() {
	f.t.Helper()
	res := f.ok("app.create", "", map[string]any{"name": "Tasks", "icon": "checklist"})
	if res.Target != "tasks" {
		f.t.Fatalf("the id made from the name is %q", res.Target)
	}
	f.ok("app.addEntity", "tasks", map[string]any{"entity": "task"})
	f.ok("app.addPage", "tasks", map[string]any{"route": "/mine", "prefab": "myTasks", "title": "My tasks"})
	f.ok("app.addPage", "tasks", map[string]any{"route": "/all", "prefab": "allTasks", "title": "All tasks"})
	f.ok("app.addPage", "tasks", map[string]any{"route": "/task/:id", "prefab": "taskDetail"})
	f.ok("app.setNav", "tasks", map[string]any{"nav": []map[string]any{{"title": "My tasks", "route": "/mine", "icon": "person"}, {"title": "All tasks", "route": "/all"}}})
	f.ok("app.setInfo", "tasks", map[string]any{"home": "/mine", "requires": map[string]any{"primitives": []string{"mui"}}})
	f.ok("app.addEditorKind", "tasks", map[string]any{"kind": "task", "page": "/task/:id", "title": "{record.title}", "subject": map[string]any{"type": "task", "label": "{record.title}"}})
	f.ok("app.addCommand", "tasks", map[string]any{"type": "task.reassign", "graph": "cmd-task-reassign", "action": "entity:task.update"})
	f.ok("app.addQuery", "tasks", map[string]any{"name": "myTasks", "graph": "qry-myTasks"})
	f.ok("app.addRole", "tasks", map[string]any{"id": "manager", "title": "Manager", "actions": []string{"entity:task.*"}})
	f.ok("app.addRole", "tasks", map[string]any{"id": "member", "title": "Member", "actions": []string{"entity:task.read", "entity:task.create"},
		"rowRules": map[string]any{"task": "record.assignee == principal.user"}})
	f.ok("app.setSettingsSchema", "tasks", map[string]any{"schema": map[string]any{"type": "object", "properties": map[string]any{"defaultDueDays": map[string]any{"type": "integer", "title": "Default due in N days"}}},
		"defaults": map[string]any{"defaultDueDays": 7}})
	f.ok("app.addTrigger", "tasks", map[string]any{"id": "taskDone", "title": "A task is done"})
}

// sink collects a subscription's views.
type sink struct {
	mu    sync.Mutex
	views []manifests.View
	got   chan struct{}
}

func (s *sink) add(raw []byte) {
	var v manifests.View
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

// until waits for a view that passes ok.
func (s *sink) until(t *testing.T, what string, ok func(manifests.View) bool) manifests.View {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		s.mu.Lock()
		if n := len(s.views); n > 0 && ok(s.views[n-1]) {
			v := s.views[n-1]
			s.mu.Unlock()
			return v
		}
		s.mu.Unlock()
		select {
		case <-s.got:
		case <-deadline:
			t.Fatalf("no view with %s arrived", what)
		}
	}
}

func TestTasksIsBuiltByCommands(t *testing.T) {
	f := setup(t)
	f.parts()
	f.ok("app.create", "", map[string]any{"name": "Tasks"})
	f.describe("tasks")
	s := &sink{got: make(chan struct{}, 256)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if fail := f.subs.Open(subscribe.OpenArgs{ServiceArgs: features.ServiceArgs{Ctx: ctx, Principal: features.LocalPrincipal()}, Query: "app.describe", Params: json.RawMessage(`{"id":"tasks"}`), Sink: s}); fail != nil {
		t.Fatal(fail.Message)
	}
	s.until(t, "rev 1", func(v manifests.View) bool { return v.Rev == 1 })

	// A second subscriber sees each change as it lands
	f.ok("app.addEntity", "tasks", map[string]any{"entity": "task"})
	s.until(t, "the entity", func(v manifests.View) bool { return v.Rev == 2 && slices.Equal(v.Manifest.Entities, []string{"task"}) })
	f.ok("app.addPage", "tasks", map[string]any{"route": "/mine", "prefab": "myTasks", "title": "My tasks"})
	s.until(t, "the page", func(v manifests.View) bool {
		return v.Rev == 3 && len(v.Manifest.Pages) == 1 && v.Manifest.Pages[0].Prefab == "myTasks"
	})
	// And a change to a part the app names
	f.ok("prefab.setInfo", "myTasks", map[string]any{"name": "Mine"})
	s.until(t, "the renamed prefab", func(v manifests.View) bool {
		return slices.ContainsFunc(v.Parts, func(p manifests.Part) bool { return p.ID == "myTasks" && p.Name == "Mine" && p.Named })
	})
	cancel()

	// The rest of the Tasks manifest
	f.ok("app.addPage", "tasks", map[string]any{"route": "/all", "prefab": "allTasks", "title": "All tasks"})
	f.ok("app.addPage", "tasks", map[string]any{"route": "/task/:id", "prefab": "taskDetail"})
	f.ok("app.setNav", "tasks", map[string]any{"nav": []map[string]any{{"title": "My tasks", "route": "/mine"}, {"title": "All tasks", "route": "/all"}}})
	f.ok("app.setInfo", "tasks", map[string]any{"home": "/mine", "icon": "checklist"})
	f.ok("app.addEditorKind", "tasks", map[string]any{"kind": "task", "page": "/task/:id", "title": "{record.title}"})
	f.ok("app.addCommand", "tasks", map[string]any{"type": "task.reassign", "graph": "cmd-task-reassign", "action": "entity:task.update"})
	f.ok("app.addQuery", "tasks", map[string]any{"name": "myTasks", "graph": "qry-myTasks"})
	f.ok("app.addRole", "tasks", map[string]any{"id": "member", "actions": []string{"entity:task.read"}, "rowRules": map[string]any{"task": "record.assignee == principal.user"}})
	f.ok("app.addRole", "tasks", map[string]any{"id": "manager", "actions": []string{"entity:task.*"}, "index": 0})
	f.ok("app.setSettingsSchema", "tasks", map[string]any{"schema": map[string]any{"type": "object", "properties": map[string]any{"defaultDueDays": map[string]any{"type": "integer"}}}, "defaults": map[string]any{"defaultDueDays": 7}})
	f.ok("app.addTrigger", "tasks", map[string]any{"id": "taskDone"})
	v := f.describe("tasks")
	m := v.Manifest
	if m.Home != "/mine" || m.Icon != "checklist" || len(m.Pages) != 3 || len(m.Nav) != 2 || m.EditorKinds[0].Page != "/task/:id" ||
		m.Commands[0].Graph != "cmd-task-reassign" || m.Queries[0].Name != "myTasks" || m.Settings.Defaults["defaultDueDays"] != float64(7) || m.Triggers[0].ID != "taskDone" {
		t.Errorf("the manifest: %+v", m)
	}
	if got := manifests.Keys(m, "role"); !slices.Equal(got, []string{"manager", "member"}) {
		t.Errorf("roles in order: %v", got)
	}
	// Its parts: those it names, and those it owns but does not name
	var parts []string
	for _, p := range v.Parts {
		parts = append(parts, fmt.Sprintf("%s:%s:%v:%v", p.Type, p.ID, p.Named, p.Owned))
	}
	if got := strings.Join(parts, " "); got != "prefab:myTasks:true:true prefab:allTasks:true:true prefab:taskDetail:true:true graph:cmd-task-reassign:true:true graph:qry-myTasks:true:true graph:tasks-handler:false:true entity:task:true:true" {
		t.Errorf("parts: %s", got)
	}

	// Updates merge per field; a role's row rules per entity type
	f.ok("app.updateRole", "tasks", map[string]any{"id": "member", "title": "Member"})
	f.ok("app.updateRole", "tasks", map[string]any{"id": "member", "rowRules": map[string]any{"task": nil}})
	r := f.describe("tasks").Manifest.Roles[1]
	if r.Title != "Member" || len(r.RowRules) != 0 || !slices.Equal(r.Actions, []string{"entity:task.read"}) {
		t.Errorf("the merged role: %+v", r)
	}
	// An update that changes nothing commits nothing
	before := f.seq["tasks"]
	if res := f.ok("app.updateRole", "tasks", map[string]any{"id": "member", "title": "Member"}); res.Seq > before {
		t.Errorf("an unchanged update committed seq %d", res.Seq)
	}
	// Renaming a route follows it in the home, the nav and the editor kinds
	f.ok("app.updatePage", "tasks", map[string]any{"route": "/mine", "rename": "/my"})
	f.ok("app.updatePage", "tasks", map[string]any{"route": "/task/:id", "rename": "/tasks/:id"})
	m = f.describe("tasks").Manifest
	if m.Home != "/my" || m.Nav[0].Route != "/my" || m.EditorKinds[0].Page != "/tasks/:id" {
		t.Errorf("after renaming routes: home %s, nav %+v, kinds %+v", m.Home, m.Nav, m.EditorKinds)
	}
	f.ok("app.removeTrigger", "tasks", map[string]any{"id": "taskDone"})
	f.ok("app.removeQuery", "tasks", map[string]any{"name": "myTasks"})
	if m := f.describe("tasks").Manifest; len(m.Triggers) != 0 || len(m.Queries) != 0 {
		t.Errorf("after removing: %+v", m)
	}

	// The app reaches the tenant database
	var row apps.AppRow
	deadline := time.Now().Add(3 * time.Second)
	for (f.db.Where("app_id = ?", "tasks").First(&row).Error != nil || row.Icon != "checklist") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if row.Name != "Tasks" || row.Icon != "checklist" || row.TenantID != features.LocalTenantID {
		t.Errorf("apps_apps: %+v", row)
	}
}

func TestTwoAuthorsEditOneDraft(t *testing.T) {
	f := setup(t)
	f.parts()
	f.tasks()
	// One author adds a page while another edits a role
	for round := range 5 {
		var wg sync.WaitGroup
		results := make([]bus.Result, 2)
		wg.Go(func() {
			results[0] = f.do("app.addPage", "tasks", map[string]any{"route": fmt.Sprintf("/view%d", round), "prefab": "allTasks"})
		})
		wg.Go(func() {
			results[1] = f.do("app.updateRole", "tasks", map[string]any{"id": "member", "description": fmt.Sprintf("round %d", round)})
		})
		wg.Wait()
		for _, r := range results {
			if r.Common.Code >= 300 {
				t.Fatalf("round %d: %s %+v", round, r.Common.Message, r.Problems)
			}
		}
		m := f.describe("tasks").Manifest
		if !slices.Contains(manifests.Keys(m, "page"), fmt.Sprintf("/view%d", round)) {
			t.Errorf("round %d: the page did not land", round)
		}
		if d := m.Roles[1].Description; d != fmt.Sprintf("round %d", round) {
			t.Errorf("round %d: the role edit did not land: %q", round, d)
		}
	}
	// Two authors on different fields of one role both land
	var wg sync.WaitGroup
	wg.Go(func() {
		f.ok("app.updateRole", "tasks", map[string]any{"id": "member", "title": "Team member"})
	})
	wg.Go(func() {
		f.ok("app.updateRole", "tasks", map[string]any{"id": "member", "rowRules": map[string]any{"task": "record.assignee == principal.user || record.done"}})
	})
	wg.Wait()
	if r := f.describe("tasks").Manifest.Roles[1]; r.Title != "Team member" || !strings.Contains(r.RowRules["task"], "record.done") {
		t.Errorf("one of two edits of one role was lost: %+v", r)
	}
}

func TestBadChangesAreRefusedWithPaths(t *testing.T) {
	f := setup(t)
	f.parts()
	f.tasks()
	f.rejected("app.addPage", "tasks", map[string]any{"route": "/x", "prefab": "nothing"}, "manifest.pages[/x].prefab", "no prefab")
	f.rejected("app.addPage", "tasks", map[string]any{"route": "/mine", "prefab": "allTasks"}, "args.route", "already a page")
	f.rejected("app.addPage", "tasks", map[string]any{"route": "mine", "prefab": "allTasks"}, "manifest.pages[mine].route", "starts with /")
	f.rejected("app.addPage", "tasks", map[string]any{"route": "/x/:name", "prefab": "allTasks"}, "manifest.pages[/x/:name].route", "no prop name")
	f.rejected("app.addPage", "tasks", map[string]any{"route": "/detail", "prefab": "taskDetail"}, "manifest.pages[/detail].prefab", "needs the prop id")
	f.rejected("app.addPage", "tasks", map[string]any{"route": "/crm", "prefab": "crmCard"}, "manifest.pages[/crm].prefab", "belongs to the app crm")
	f.ok("app.addPage", "tasks", map[string]any{"route": "/header", "prefab": "pageHeader"}) // the tenant library's
	f.rejected("app.setNav", "tasks", map[string]any{"nav": []map[string]any{{"title": "Gone", "route": "/gone"}}}, "manifest.nav[0].route", "no page at /gone")
	f.rejected("app.setNav", "tasks", map[string]any{"nav": []map[string]any{{"title": "One", "route": "/task/:id"}}}, "manifest.nav[0].route", "needs params")
	f.rejected("app.removePage", "tasks", map[string]any{"route": "/mine"}, "manifest.home", "no page at /mine")
	f.rejected("app.removePage", "tasks", map[string]any{"route": "/nothing"}, "args.route", "no page")
	f.rejected("app.setInfo", "tasks", map[string]any{"frame": "prefab.nothing"}, "manifest.frame", "no prefab")
	f.rejected("app.setInfo", "tasks", map[string]any{"requires": map[string]any{"primitives": []string{"blink"}}}, "manifest.requires.primitives[0]", "no primitive")
	f.rejected("app.addEditorKind", "tasks", map[string]any{"kind": "task", "page": "/mine", "title": "x"}, "args.kind", "already")
	f.rejected("app.addEditorKind", "tasks", map[string]any{"kind": "other", "page": "/gone", "title": "x"}, "manifest.editorKinds[other].page", "no page")
	f.rejected("app.addCommand", "tasks", map[string]any{"type": "task.other", "graph": "tasks-handler"}, "manifest.commands[task.other].graph", "not a command graph")
	f.rejected("app.addCommand", "tasks", map[string]any{"type": "task.other", "graph": "cmd-task-reassign"}, "manifest.commands[task.other].graph", "defines the command \"task.reassign\"")
	f.rejected("app.addCommand", "tasks", map[string]any{"type": "task.other", "graph": "missing"}, "manifest.commands[task.other].graph", "no graph")
	f.rejected("app.updateCommand", "tasks", map[string]any{"type": "task.reassign", "action": "update"}, "manifest.commands[task.reassign].action", "not an action")
	f.rejected("app.addQuery", "tasks", map[string]any{"name": "task.reassign", "graph": "cmd-task-reassign"}, "manifest.queries[task.reassign].graph", "not a data.query graph")
	f.rejected("app.updateRole", "tasks", map[string]any{"id": "member", "rowRules": map[string]any{"customer": "true"}}, "manifest.roles[member].rowRules.customer", "does not use")
	f.rejected("app.updateRole", "tasks", map[string]any{"id": "member", "rowRules": map[string]any{"task": "record.assignee =="}}, "manifest.roles[member].rowRules.task", "does not compile")
	f.rejected("app.addEntity", "tasks", map[string]any{"entity": "customer"}, "manifest.entities[customer]", "belongs to the app crm")
	f.rejected("app.addEntity", "tasks", map[string]any{"entity": "nothing"}, "manifest.entities[nothing]", "no entity type")
	f.rejected("app.removeEntity", "tasks", map[string]any{"entity": "task"}, "manifest.roles[member].rowRules.task", "does not use")
	f.rejected("app.setSettingsSchema", "tasks", map[string]any{"schema": map[string]any{"type": "object", "properties": map[string]any{"n": map[string]any{"type": "integer"}}}, "defaults": map[string]any{"n": "seven"}}, "manifest.settings.defaults", "do not fit")
	f.rejected("app.updatePage", "tasks", map[string]any{"route": "/mine", "rename": "/all"}, "args.rename", "already a page")
	f.rejected("app.setInfo", "tasks", map[string]any{"requires": map[string]any{"apps": []string{"tasks"}}}, "manifest.requires.apps[0]", "require itself")
	f.rejected("app.addPage", "nothing", map[string]any{"route": "/x", "prefab": "allTasks"}, "target", "no app")

	// Requiring the CRM lets the app use its parts
	f.ok("app.setInfo", "tasks", map[string]any{"requires": map[string]any{"primitives": []string{"mui"}, "apps": []string{"crm"}}})
	f.ok("app.addEntity", "tasks", map[string]any{"entity": "customer"})
	f.ok("app.addPage", "tasks", map[string]any{"route": "/crm", "prefab": "crmCard"})
	if m := f.describe("tasks").Manifest; len(m.Pages) != 5 || len(m.Entities) != 2 {
		t.Errorf("a refused command changed the draft: %v %v", manifests.Keys(m, "page"), m.Entities)
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

// TestTheChatBuildsAnApp runs the tools a chat model gets: the app.*
// commands' own tools, then app.describe and app.list.
func TestTheChatBuildsAnApp(t *testing.T) {
	f := setup(t)
	f.parts()
	run := f.tool
	res := run("app.create", map[string]any{"args": map[string]any{"name": "Tasks"}})
	if res.Content["target"] != "tasks" {
		t.Fatalf("app.create: %v", res.Content)
	}
	run("app.addEntity", map[string]any{"target": "tasks", "args": map[string]any{"entity": "task"}})
	res = run("app.addPage", map[string]any{"target": "tasks", "args": map[string]any{"route": "/mine", "prefab": "myTasks"}})
	report, _ := json.Marshal(res.Content["report"])
	if !strings.Contains(string(report), `"pages":["/mine"]`) || !strings.Contains(string(report), `"problems":[]`) {
		t.Errorf("the report: %s", report)
	}
	res = run("app.describe", map[string]any{"id": "tasks"})
	raw, _ := json.Marshal(res.Content)
	if !strings.Contains(string(raw), `"prefab":"myTasks"`) || !strings.Contains(string(raw), `"parts":[`) {
		t.Errorf("app.describe: %s", raw)
	}
	if res := run("app.list", map[string]any{}); res.Summary != "1 apps" {
		t.Errorf("app.list: %s", res.Summary)
	}
	// Every command's example fits its args, for a model to copy
	for _, c := range live.Commands.All(f.reg) {
		if strings.HasPrefix(c.ID, "app.") {
			if err := tools.CheckArgs(c.Args, c.Examples[0]); err != nil {
				t.Errorf("%s's example: %v", c.ID, err)
			}
		}
	}
	// Every list's commands are tools
	for _, l := range manifests.Lists {
		for _, op := range []string{"add", "remove"} {
			name := "app." + op + strings.ToUpper(l.Item[:1]) + l.Item[1:]
			if !slices.ContainsFunc(tools.Tools.All(f.reg), func(tl tools.Tool) bool { return tl.ID == name }) {
				t.Errorf("no tool %s", name)
			}
		}
	}
}
