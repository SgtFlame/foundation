package installs_test

import (
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
	"gorm.io/gorm"
)

type fixture struct {
	t    *testing.T
	bus  *bus.Bus
	subs *subscribe.Subscriptions
	p    *project.Projections
	db   *gorm.DB
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
	f := &fixture{t: t, bus: bus.New(p), subs: subscribe.New(p, reg), p: p, db: gdb}
	nodeDeps = livenodes.Deps{Engine: eng, Bus: f.bus, Subscriptions: f.subs, Store: p}
	draftDeps = drafts.Deps{Engine: eng, Projections: p}
	dataDeps = data.Deps{Projections: p, Bus: f.bus}
	entityDeps = entities.Deps{Projections: p, DB: gdb, OnPublish: func(args features.ServiceArgs) error { return data.Follow(args, dataDeps) }}
	prefabDeps = prefabs.Deps{Projections: p, DB: gdb}
	appDeps = manifests.Deps{Projections: p, DB: gdb}
	installDep = installs.Deps{Projections: p, DB: gdb, Nodes: nodeDeps}
	// As serve.go wires them
	f.bus.Resolve = installs.CommandFor(func() installs.Deps { return installDep }, livenodes.CommandFor(func() livenodes.Deps { return nodeDeps }))
	f.subs.Resolve = installs.QueryFor(func() installs.Deps { return installDep }, livenodes.QueryFor(func() livenodes.Deps { return nodeDeps }))
	return f
}

func args() features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
}

func (f *fixture) do(typ, target string, a any) bus.Result {
	raw, _ := json.Marshal(a)
	return f.bus.Execute(bus.Request{ServiceArgs: args(), ID: fmt.Sprintf("c%d", f.n.Add(1)), Type: typ, Target: target, Args: raw})
}

func (f *fixture) ok(typ, target string, a any) bus.Result {
	f.t.Helper()
	res := f.do(typ, target, a)
	if res.Common.Code >= 300 {
		f.t.Fatalf("%s: %s %+v", typ, res.Common.Message, res.Problems)
	}
	return res
}

// refused sends a command that must be refused with a problem at path
// whose message holds want.
func (f *fixture) refused(typ, target string, a any, path, want string) {
	f.t.Helper()
	res := f.do(typ, target, a)
	if res.Common.Code != 422 || !slices.ContainsFunc(res.Problems, func(p live.Problem) bool {
		return p.Path == path && strings.Contains(p.Message, want)
	}) {
		f.t.Errorf("%s was not refused at %s with %q: %d %+v", typ, path, want, res.Common.Code, res.Problems)
	}
}

func (f *fixture) wait(projector, key string, seq uint64) live.Projected {
	f.t.Helper()
	v, err := f.p.WaitFor(context.Background(), features.LocalTenantID, projector, key, seq, 3*time.Second)
	if err != nil || v.Seq < seq {
		f.t.Fatalf("%s %s did not reach %d: %v", projector, key, seq, err)
	}
	return v
}

// once reads a query's snapshot.
func (f *fixture) once(query string, params any, into any) {
	f.t.Helper()
	raw, _ := json.Marshal(params)
	v, _, fail := f.subs.Once(args(), query, raw)
	if fail != nil {
		f.t.Fatalf("%s: %s", query, fail.Message)
	}
	b, _ := json.Marshal(v)
	_ = json.Unmarshal(b, into)
}

func node(id, typ string, config map[string]any) map[string]any {
	n := map[string]any{"id": id, "type": typ, "position": map[string]any{"x": 0, "y": 0}}
	if config != nil {
		n["config"] = config
	}
	return n
}

func edges(specs ...string) []map[string]any {
	var out []map[string]any
	for _, s := range specs {
		from, to, _ := strings.Cut(s, " -> ")
		fn, fp, _ := strings.Cut(from, ".")
		tn, tp, _ := strings.Cut(to, ".")
		out = append(out, map[string]any{"from": map[string]any{"node": fn, "pin": fp}, "to": map[string]any{"node": tn, "pin": tp}})
	}
	return out
}

// reassign is the Tasks app's task.reassign graph: it sets the task's
// assignee, with suffix added, so a test tells versions apart.
func reassign(suffix string) map[string]any {
	return map[string]any{"name": "Reassign a task", "kind": "command", "owner": "app:tasks",
		"nodes": []any{
			node("event", livenodes.OnCommand, map[string]any{"outputs": []any{map[string]any{"id": "assignee", "type": "string"}}}),
			node("set", "data.expression", map[string]any{"expr": `{assignee: assignee + "` + suffix + `"}`, "type": "json"}),
			node("up", "records.update", map[string]any{"entity": "task"}),
		},
		"edges": edges("event.then -> up.then", "event.target -> up.id", "event.assignee -> set.assignee", "set.value -> up.set"),
		"spec": livenodes.CommandSpec{Command: "task.reassign", Title: "Reassign", Description: "Gives a task to someone else",
			Args: json.RawMessage(`{"type":"object","properties":{"assignee":{"type":"string"}},"required":["assignee"]}`),
			Area: "rec", TargetType: "task", Projection: data.Projector, Action: "record.update", Events: []string{"updated"}},
	}
}

// myTasks is the Tasks app's myTasks query graph: it returns {version}.
func myTasks(version int) map[string]any {
	return map[string]any{"name": "My tasks", "kind": "data.query", "owner": "app:tasks",
		"nodes": []any{
			node("event", livenodes.OnQuery, nil),
			node("value", "data.expression", map[string]any{"expr": fmt.Sprintf("{version: %d}", version), "type": "json"}),
			node("ret", "flow.return", map[string]any{"outputs": []any{map[string]any{"id": "result", "type": "json"}}}),
		},
		"edges": edges("event.then -> ret.then", "value.value -> ret.result"),
		"spec":  livenodes.QuerySpec{Query: "myTasks", Title: "My tasks", Description: "The version answering", Action: "record.read"},
	}
}

// publishGraph imports a graph and publishes it.
func (f *fixture) publishGraph(id string, g map[string]any) {
	f.t.Helper()
	res := f.ok("bp.import", id, map[string]any{"graph": g})
	var d drafts.Draft
	_ = json.Unmarshal(f.wait(drafts.DraftProjector, "graph."+id, res.Seq).Value, &d)
	res = f.ok("bp.publish", id, map[string]any{"rev": d.Rev})
	f.wait(drafts.PublishedProjector, "graph."+id, res.Seq)
}

// rev is an app draft's rev, once its last commit is projected.
func (f *fixture) draft(id string, seq uint64) manifests.Draft {
	f.t.Helper()
	var d manifests.Draft
	_ = json.Unmarshal(f.wait(manifests.DraftProjector, "app."+id, seq).Value, &d)
	return d
}

// tasks makes the Tasks app's parts and draft; publish publishes the
// parts.
func (f *fixture) tasks(publish bool) uint64 {
	own := "app:tasks"
	f.ok("entity.create", "task", map[string]any{"name": "Task", "owner": own, "fields": []map[string]any{
		{"name": "title", "type": "text"}, {"name": "assignee", "type": "text"}}})
	f.ok("prefab.create", "myTasks", map[string]any{"name": "My tasks", "owner": own})
	f.ok("prefab.create", "taskDetail", map[string]any{"name": "Task detail", "owner": own})
	f.ok("prefab.setInterface", "taskDetail", map[string]any{"props": map[string]any{"id": map[string]any{"type": "string", "required": true}}})
	f.ok("bp.import", "cmd-tasks-task-reassign", map[string]any{"graph": reassign("")})
	f.ok("bp.import", "qry-tasks-myTasks", map[string]any{"graph": myTasks(1)})
	if publish {
		f.publishParts()
	}
	f.ok("app.create", "", map[string]any{"name": "Tasks", "icon": "checklist"})
	f.ok("app.addEntity", "tasks", map[string]any{"entity": "task"})
	f.ok("app.addPage", "tasks", map[string]any{"route": "/mine", "prefab": "myTasks", "title": "My tasks"})
	f.ok("app.addPage", "tasks", map[string]any{"route": "/task/:id", "prefab": "taskDetail"})
	f.ok("app.addEditorKind", "tasks", map[string]any{"kind": "task", "page": "/task/:id", "title": "{record.title}"})
	f.ok("app.addCommand", "tasks", map[string]any{"type": "task.reassign", "graph": "cmd-tasks-task-reassign", "action": "entity:task.update"})
	f.ok("app.addQuery", "tasks", map[string]any{"name": "myTasks", "graph": "qry-tasks-myTasks"})
	f.ok("app.addTrigger", "tasks", map[string]any{"id": "taskDone", "title": "A task is done"})
	return f.ok("app.setInfo", "tasks", map[string]any{"requires": map[string]any{"primitives": []string{"mui"}}}).Seq
}

func (f *fixture) publishParts() {
	f.t.Helper()
	res := f.ok("entity.publish", "task", map[string]any{"rev": 1})
	f.wait(entities.PublishedProjector, "entity.task", res.Seq)
	res = f.ok("prefab.publish", "myTasks", map[string]any{"rev": 1})
	f.wait(prefabs.PublishedProjector, "prefab.myTasks", res.Seq)
	res = f.ok("prefab.publish", "taskDetail", map[string]any{"rev": 2})
	f.wait(prefabs.PublishedProjector, "prefab.taskDetail", res.Seq)
	f.publishGraph("cmd-tasks-task-reassign", reassign(""))
	f.publishGraph("qry-tasks-myTasks", myTasks(1))
}

// publish publishes the app at its draft's rev.
func (f *fixture) publish(seq uint64) manifests.Draft {
	f.t.Helper()
	d := f.draft("tasks", seq)
	res := f.ok("app.publish", "tasks", map[string]any{"rev": d.Rev})
	// The draft's event comes first in the publish's batch, the version's last
	d = f.draft("tasks", res.Seq-1)
	f.wait(manifests.VersionProjector, "appVersion."+apps.VersionID("tasks", d.Published), res.Seq)
	return d
}

// lifecycle installs, upgrades or uninstalls an app and waits until the
// install is projected, which is what commands and queries read.
func (f *fixture) lifecycle(typ, app string, a any) {
	f.t.Helper()
	res := f.ok(typ, app, a)
	f.wait(installs.Projector, "appInstall."+app, res.Seq)
}

func (f *fixture) record(id string) map[string]any {
	f.t.Helper()
	var r map[string]any
	f.once("records.get", map[string]any{"entity": "task", "id": id}, &r)
	return r
}

// sink collects a subscription's results.
type sink struct {
	mu        sync.Mutex
	snapshots []map[string]any
	failed    string
	got       chan struct{}
}

func (s *sink) Snapshot(data any, _ live.Position) {
	raw, _ := json.Marshal(data)
	var v map[string]any
	_ = json.Unmarshal(raw, &v)
	s.mu.Lock()
	s.snapshots = append(s.snapshots, v)
	s.mu.Unlock()
	s.got <- struct{}{}
}
func (s *sink) Change(live.Delta, live.Position) {}
func (s *sink) Failed(resp features.ServiceResponse) {
	s.mu.Lock()
	s.failed = resp.Message
	s.mu.Unlock()
	s.got <- struct{}{}
}

func (s *sink) until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		s.mu.Lock()
		done := ok()
		s.mu.Unlock()
		if done {
			return
		}
		select {
		case <-s.got:
		case <-deadline:
			t.Fatalf("no %s arrived", what)
		}
	}
}

func TestPublishInstallUpgradeUninstall(t *testing.T) {
	f := setup(t)
	seq := f.tasks(false)

	// Nothing published: the publish names each part
	d := f.draft("tasks", seq)
	res := f.do("app.publish", "tasks", map[string]any{"rev": d.Rev})
	for _, want := range []string{"parts.prefab[myTasks]", "parts.prefab[taskDetail]", "parts.graph[cmd-tasks-task-reassign]", "parts.graph[qry-tasks-myTasks]", "parts.entity[task]"} {
		if !slices.ContainsFunc(res.Problems, func(p live.Problem) bool { return p.Path == want && p.Code == "app_unpublished" }) {
			t.Errorf("the publish did not name %s: %+v", want, res.Problems)
		}
	}
	f.refused("app.publish", "tasks", map[string]any{"rev": d.Rev + 1}, "args.rev", "has no rev")
	f.publishParts()
	r := f.publish(seq)
	if r.Published != 1 {
		t.Fatalf("published %d", r.Published)
	}
	var versions manifests.Versions
	f.once("app.versions", map[string]any{"id": "tasks"}, &versions)
	if versions.Published != 1 || len(versions.Versions) != 1 || versions.PublishedRev != versions.Rev {
		t.Errorf("versions: %+v", versions)
	}
	v, ok := manifests.GetVersion(args(), f.p, "tasks", 0)
	if !ok || v.Version != 1 || v.Prefabs["myTasks"] != 1 || v.Prefabs["taskDetail"] != 1 || v.Entities["task"] != 1 ||
		v.Graphs["cmd-tasks-task-reassign"].Version != 1 || v.Graphs["qry-tasks-myTasks"].Version != 1 || v.Manifest.Version != 1 {
		t.Errorf("the version's pins: %+v", v)
	}

	// Not installed: neither its scoped name nor its own is a command
	if res := f.do("tasks.task.reassign", "t1", map[string]any{"assignee": "bob"}); res.Common.Code != 404 {
		t.Errorf("an app not installed has a command: %d", res.Common.Code)
	}
	if res := f.do("task.reassign", "t1", map[string]any{"assignee": "bob"}); res.Common.Code != 404 {
		t.Errorf("the app's command is live under its own name: %d", res.Common.Code)
	}

	// Install: the command is decided by its graph, the query answered by its
	f.lifecycle("app.install", "tasks", map[string]any{})
	f.refused("app.install", "tasks", map[string]any{}, "target", "installed already")
	var all map[string]installs.Summary
	f.once("app.installs", map[string]any{}, &all)
	s := all["app.tasks"]
	if s.Version != 1 || s.Status != apps.InstallInstalled || len(s.Commands) != 1 || s.Commands[0].Name != "tasks.task.reassign" ||
		s.Queries[0].Name != "tasks.myTasks" || s.EditorKinds[0].Kind != "task" || s.Triggers[0].ID != "taskDone" {
		t.Errorf("app.installs: %+v", s)
	}
	f.ok("record.create", "t1", map[string]any{"entity": "task", "values": map[string]any{"title": "Write the plan"}})
	f.ok("tasks.task.reassign", "t1", map[string]any{"assignee": "bob"})
	if got := f.record("t1")["assignee"]; got != "bob" {
		t.Errorf("the assignee after reassigning: %v", got)
	}
	sk := &sink{got: make(chan struct{}, 64)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if fail := f.subs.Open(subscribe.OpenArgs{ServiceArgs: features.ServiceArgs{Ctx: ctx, Principal: features.LocalPrincipal()}, Query: "tasks.myTasks", Params: json.RawMessage(`{}`), Sink: sk}); fail != nil {
		t.Fatal(fail.Message)
	}
	sk.until(t, "first snapshot", func() bool { return len(sk.snapshots) == 1 && sk.snapshots[0]["version"] == float64(1) })

	// Its parts published again change nothing until the app is published and upgraded
	f.publishGraph("cmd-tasks-task-reassign", reassign("-v2"))
	f.publishGraph("qry-tasks-myTasks", myTasks(2))
	f.ok("tasks.task.reassign", "t1", map[string]any{"assignee": "carol"})
	if got := f.record("t1")["assignee"]; got != "carol" {
		t.Errorf("a part published again reached the installed app: %v", got)
	}
	f.refused("app.upgrade", "tasks", map[string]any{}, "args.version", "not newer")
	r = f.publish(seq)
	f.lifecycle("app.upgrade", "tasks", map[string]any{})
	f.ok("tasks.task.reassign", "t1", map[string]any{"assignee": "dan"})
	if got := f.record("t1")["assignee"]; got != "dan-v2" {
		t.Errorf("after the upgrade the assignee is %v, not decided by the new graph", got)
	}
	// The open subscription takes a fresh snapshot under the new query
	sk.until(t, "a fresh snapshot", func() bool {
		return len(sk.snapshots) >= 2 && sk.snapshots[len(sk.snapshots)-1]["version"] == float64(2)
	})

	// An upgrade that removes the command refuses it from then on; one
	// that removes the query ends its subscription
	f.ok("app.removeCommand", "tasks", map[string]any{"type": "task.reassign"})
	seq = f.ok("app.removeQuery", "tasks", map[string]any{"name": "myTasks"}).Seq
	r = f.publish(seq)
	if r.Published != 3 {
		t.Fatalf("published %d", r.Published)
	}
	f.lifecycle("app.upgrade", "tasks", map[string]any{"version": 3})
	if res := f.do("tasks.task.reassign", "t1", map[string]any{"assignee": "eve"}); res.Common.Code != 404 {
		t.Errorf("a removed command was decided: %d %+v", res.Common.Code, res.Problems)
	}
	sk.until(t, "the end of the subscription", func() bool { return strings.Contains(sk.failed, "no query myTasks") })

	// Uninstall keeps the records; installing again takes the last version
	f.lifecycle("app.uninstall", "tasks", map[string]any{})
	f.refused("app.uninstall", "tasks", map[string]any{}, "target", "not installed")
	f.once("app.installs", map[string]any{}, &all)
	if all["app.tasks"].Status != apps.InstallUninstalled {
		t.Errorf("after uninstalling: %+v", all["app.tasks"])
	}
	if got := f.record("t1")["assignee"]; got != "dan-v2" {
		t.Errorf("the records after uninstalling: %v", got)
	}
	f.refused("app.upgrade", "tasks", map[string]any{}, "target", "not installed")
	f.lifecycle("app.install", "tasks", map[string]any{"version": 2})
	f.ok("tasks.task.reassign", "t1", map[string]any{"assignee": "fay"})
	if got := f.record("t1")["assignee"]; got != "fay-v2" {
		t.Errorf("after installing version 2 again: %v", got)
	}

	// The tenant database follows
	deadline := time.Now().Add(3 * time.Second)
	for {
		var vs []apps.VersionRow
		var in apps.InstallRow
		f.db.Order("version").Find(&vs)
		f.db.Where("app_id = ?", "tasks").First(&in)
		if len(vs) == 3 && in.Version == 2 && in.Status == apps.InstallInstalled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the tenant database: %d versions, install %+v", len(vs), in)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestPublishAtTheRevSeen(t *testing.T) {
	f := setup(t)
	seq := f.tasks(true)
	seen := f.draft("tasks", seq).Rev
	// Another author adds a trigger after the publisher last looked
	seq = f.ok("app.addTrigger", "tasks", map[string]any{"id": "taskLate"}).Seq
	f.draft("tasks", seq)
	res := f.ok("app.publish", "tasks", map[string]any{"rev": seen})
	d := f.draft("tasks", res.Seq-1)
	f.wait(manifests.VersionProjector, "appVersion.tasks-v1", res.Seq)
	v, _ := manifests.GetVersion(args(), f.p, "tasks", 1)
	if v.Rev != seen || len(v.Manifest.Triggers) != 1 {
		t.Errorf("the version took the draft at rev %d with triggers %+v", v.Rev, v.Manifest.Triggers)
	}
	if d.Published != 1 || d.PublishedRev != seen || len(d.Manifest.Triggers) != 2 {
		t.Errorf("the draft keeps the later change for the next version: published %d at %d, triggers %d", d.Published, d.PublishedRev, len(d.Manifest.Triggers))
	}
	var view manifests.View
	f.once("app.describe", map[string]any{"id": "tasks"}, &view)
	if !view.Changed || view.Published != 1 {
		t.Errorf("app.describe does not show the changes since: %+v", view)
	}
	// Published at its last rev, nothing waits
	res = f.ok("app.publish", "tasks", map[string]any{"rev": d.Rev})
	d = f.draft("tasks", res.Seq-1)
	if d.Published != 2 || d.PublishedRev != d.Rev {
		t.Errorf("published at the last rev: %d at %d, rev %d", d.Published, d.PublishedRev, d.Rev)
	}
}

func TestRequires(t *testing.T) {
	f := setup(t)
	seq := f.tasks(true)
	f.publish(seq)
	f.ok("app.create", "", map[string]any{"name": "Needy"})
	seq = f.ok("app.setInfo", "needy", map[string]any{"requires": map[string]any{"apps": []string{"tasks"}}}).Seq
	res := f.ok("app.publish", "needy", map[string]any{"rev": f.draft("needy", seq).Rev})
	f.wait(manifests.VersionProjector, "appVersion.needy-v1", res.Seq)
	f.refused("app.install", "needy", map[string]any{}, "requires.apps[tasks]", "not installed")
	f.lifecycle("app.install", "tasks", map[string]any{})
	f.lifecycle("app.install", "needy", map[string]any{})
	f.refused("app.install", "nothing", map[string]any{}, "target", "no published version")
	f.refused("app.upgrade", "tasks", map[string]any{"version": 9}, "args.version", "no version 9")
	// A primitive the server lacks, as a tenant importing an app finds it
	got := apps.Missing(apps.Requires{Primitives: []string{"mui", "chart"}}, map[string]ui.Component{"mui.table": {}}, func(string) bool { return true })
	if len(got) != 1 || got[0].Path != "requires.primitives[chart]" {
		t.Errorf("missing primitives: %+v", got)
	}
}

// A command graph a manifest app owns is live only through its install,
// while a Go app's (Hello's) stays live by itself.
func TestManifestOwnedGraphsAreNotLiveAlone(t *testing.T) {
	f := setup(t)
	f.tasks(true)
	g := reassign("")
	g["spec"] = livenodes.CommandSpec{Command: "task.archive", Title: "Archive", Description: "Archives", Area: "rec", TargetType: "task", Projection: data.Projector, Action: "record.update", Events: []string{"updated"}}
	f.publishGraph("cmd-task-archive", g)
	if res := f.do("task.archive", "t1", map[string]any{"assignee": "x"}); res.Common.Code != 404 {
		t.Errorf("a manifest app's graph is a command by itself: %d", res.Common.Code)
	}
	g["owner"] = "app:hello"
	f.publishGraph("cmd-task-archive", g)
	f.ok("record.create", "t1", map[string]any{"entity": "task", "values": map[string]any{"title": "x"}})
	f.ok("task.archive", "t1", map[string]any{"assignee": "x"})
	// The app's own id for its graphs is also refused under another app
	f.refused("bp.create", "cmd-crm-task-reassign", map[string]any{"kind": "command", "name": "x", "owner": "app:tasks",
		"spec": map[string]any{"command": "task.reassign", "title": "x", "description": "x", "area": "rec", "targetType": "task", "projection": data.Projector, "action": "record.update"}},
		"target", `must have the id "cmd-tasks-task-reassign"`)
}
