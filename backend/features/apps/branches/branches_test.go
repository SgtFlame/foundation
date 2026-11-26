package branches_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/branches"
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
	"github.com/SgtFlame/foundation/backend/features/records/entities"
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
		prefabDeps prefabs.Deps
		appDeps    manifests.Deps
		branchDeps branches.Deps
		undoDeps   ops.Deps
	)
	livenodes.Contribute(reg, func() livenodes.Deps { return nodeDeps })
	drafts.Contribute(reg, func() drafts.Deps { return draftDeps })
	entities.Contribute(reg, func() entities.Deps { return entityDeps })
	prefabs.Contribute(reg, func() prefabs.Deps { return prefabDeps })
	manifests.Contribute(reg, func() manifests.Deps { return appDeps })
	branches.Contribute(reg, func() branches.Deps { return branchDeps })
	ops.Contribute(reg)
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
	nodeDeps = livenodes.Deps{Engine: eng, Bus: f.bus, Subscriptions: f.subs, Store: p}
	draftDeps = drafts.Deps{Engine: eng, Projections: p}
	entityDeps = entities.Deps{Projections: p, DB: gdb}
	prefabDeps = prefabs.Deps{Projections: p, DB: gdb}
	appDeps = manifests.Deps{Projections: p, DB: gdb}
	branchDeps = branches.Deps{Projections: p}
	undoDeps = ops.Deps{Bus: f.bus, Store: p}
	return f
}

var ada = features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, UserID: 1, TenantID: 1}}

// send sends a command on a branch ("" for main) of app.
func (f *fixture) send(app, branch, typ, target string, a any) bus.Result {
	f.t.Helper()
	raw, _ := json.Marshal(a)
	return f.bus.Execute(bus.Request{ServiceArgs: ada, ID: fmt.Sprintf("c%d", f.n.Add(1)), Type: typ, Target: target, App: app, Branch: branch, Args: raw})
}

func (f *fixture) ok(app, branch, typ, target string, a any) bus.Result {
	f.t.Helper()
	res := f.send(app, branch, typ, target, a)
	if res.Common.Code >= 300 {
		f.t.Fatalf("%s on %s (branch %q): %s %+v", typ, target, branch, res.Common.Message, res.Problems)
	}
	return res
}

func (f *fixture) refused(code, app, branch, typ, target string, a any) bus.Result {
	f.t.Helper()
	res := f.send(app, branch, typ, target, a)
	if res.Common.Code < 300 {
		f.t.Fatalf("%s on %s (branch %q) was not refused", typ, target, branch)
	}
	if code != "" && res.Common.ErrorCode != code {
		f.t.Fatalf("%s on %s (branch %q): refused with %s (%s), not %s", typ, target, branch, res.Common.ErrorCode, res.Common.Message, code)
	}
	return res
}

// tasks makes the Tasks app's parts and manifest on main.
func (f *fixture) tasks() {
	own := "app:tasks"
	f.ok("tasks", "", "entity.create", "task", map[string]any{"name": "Task", "owner": own, "fields": []map[string]any{{"name": "title", "type": "text"}}})
	for _, id := range []string{"myTasks", "allTasks"} {
		f.ok("tasks", "", "prefab.create", id, map[string]any{"name": id, "owner": own})
	}
	f.ok("tasks", "", "bp.create", "qry-myTasks", map[string]any{"kind": "data.query", "name": "My tasks", "owner": own, "spec": map[string]any{"query": "myTasks", "title": "My tasks", "description": "My open tasks", "action": "record.read"}})
	f.ok("tasks", "", "prefab.create", "pageHeader", map[string]any{"name": "Page header"}) // the tenant's library
	f.ok("", "", "app.create", "", map[string]any{"name": "Tasks"})
	f.ok("tasks", "", "app.addEntity", "tasks", map[string]any{"entity": "task"})
	f.ok("tasks", "", "app.addPage", "tasks", map[string]any{"route": "/mine", "prefab": "myTasks", "title": "Mine"})
	f.ok("tasks", "", "app.addQuery", "tasks", map[string]any{"name": "myTasks", "graph": "qry-myTasks"})
}

// read is a part's draft as a branch has it, through a branch's store,
// once seq is projected.
func (f *fixture) read(branch, projector, key string, seq uint64) (json.RawMessage, bool) {
	f.t.Helper()
	if seq > 0 {
		if err := f.p.Settle(context.Background(), 1, projector); err != nil {
			f.t.Fatal(err)
		}
	}
	store := live.OnBranch(f.p, f.reg, live.Branch{App: "tasks", Name: branch})
	v, ok, err := store.Get(context.Background(), 1, projector, key)
	if err != nil {
		f.t.Fatal(err)
	}
	return v.Value, ok && v.Value != nil
}

func description(raw json.RawMessage) string {
	var d struct {
		Prefab struct{ Description string }
		Graph  struct{ Description string }
	}
	_ = json.Unmarshal(raw, &d)
	return d.Prefab.Description + d.Graph.Description
}

func TestBranchDrafts(t *testing.T) {
	f := setup(t)
	f.tasks()
	// A branch of an app that has no commit commits main first
	res := f.ok("tasks", "", "app.branch.create", "", map[string]any{"app": "tasks", "name": "refunds", "title": "Refunds"})
	if res.Target != "tasks_refunds" {
		t.Fatalf("the branch's target is %q", res.Target)
	}
	_ = f.p.Settle(context.Background(), 1, branches.Projector)
	_ = f.p.Settle(context.Background(), 1, branches.CommitProjector)
	main, ok := branches.GetRecord(context.Background(), f.p, 1, "tasks", "main")
	if !ok || main.Head == "" || main.Commits != 1 {
		t.Fatalf("main's record: %+v %v", main, ok)
	}
	br, _ := branches.GetRecord(context.Background(), f.p, 1, "tasks", "refunds")
	if br.Head != main.Head || br.Base.Commit != main.Head || br.Base.Branch != "main" {
		t.Fatalf("the branch's base: %+v", br)
	}
	first, _ := branches.GetCommit(context.Background(), f.p, 1, main.Head)
	var keys []string
	for _, p := range first.Parts {
		keys = append(keys, p.Key())
	}
	if !slices.Equal(keys, []string{"app.tasks", "entity.task", "graph.qry-myTasks", "prefab.allTasks", "prefab.myTasks"}) {
		t.Fatalf("main's first commit holds %v", keys)
	}

	// The first change on the branch copies the part; main is untouched
	seq := f.ok("tasks", "refunds", "prefab.setInfo", "myTasks", map[string]any{"description": "Mine, on the branch"}).Seq
	if v, _ := f.read("refunds", prefabs.DraftProjector, "prefab.myTasks", seq); description(v) != "Mine, on the branch" {
		t.Fatalf("on the branch: %s", v)
	}
	if v, _ := f.read("", prefabs.DraftProjector, "prefab.myTasks", seq); description(v) != "" {
		t.Fatalf("main changed: %s", v)
	}
	var d prefabs.Draft
	v, _ := f.read("refunds", prefabs.DraftProjector, "prefab.myTasks", seq)
	_ = json.Unmarshal(v, &d)
	if d.Prefab.ID != "myTasks" || d.Prefab.Owner != "app:tasks" {
		t.Fatalf("the branch's draft is %s owned by %s", d.Prefab.ID, d.Prefab.Owner)
	}
	// An unchanged part reads as the base commit has it, even after main moves on
	seq = f.ok("tasks", "", "prefab.setInfo", "allTasks", map[string]any{"description": "All, on main"}).Seq
	if v, ok := f.read("refunds", prefabs.DraftProjector, "prefab.allTasks", seq); !ok || description(v) != "" {
		t.Fatalf("an inherited part on the branch: %s", v)
	}
	if v, _ := f.read("", prefabs.DraftProjector, "prefab.allTasks", seq); description(v) != "All, on main" {
		t.Fatalf("main: %s", v)
	}
	// A library part is read as main has it, but not changed on the branch
	if _, ok := f.read("refunds", prefabs.DraftProjector, "prefab.pageHeader", 0); !ok {
		t.Fatal("the library prefab is not read on the branch")
	}
	f.refused("not_owned", "tasks", "refunds", "prefab.setInfo", "pageHeader", map[string]any{"description": "x"})
	// Publishing is main's
	f.refused("not_on_branch", "tasks", "refunds", "prefab.publish", "myTasks", map[string]any{"rev": 1})
	// A part made on the branch is there alone
	seq = f.ok("tasks", "refunds", "entity.create", "refund", map[string]any{"name": "Refund", "owner": "app:tasks", "fields": []map[string]any{{"name": "amount", "type": "number"}}}).Seq
	if _, ok := f.read("refunds", entities.DraftProjector, "entity.refund", seq); !ok {
		t.Fatal("the new entity type is not on the branch")
	}
	if _, ok := f.read("", entities.DraftProjector, "entity.refund", seq); ok {
		t.Fatal("the new entity type is on main")
	}
	// The manifest on the branch checks against the branch's parts
	seq = f.ok("tasks", "refunds", "app.addEntity", "tasks", map[string]any{"entity": "refund"}).Seq
	f.refused("", "tasks", "", "app.addEntity", "tasks", map[string]any{"entity": "refund"})
	var m manifests.Draft
	v, _ = f.read("refunds", manifests.DraftProjector, "app.tasks", seq)
	_ = json.Unmarshal(v, &m)
	if m.Manifest.ID != "tasks" || !slices.Contains(m.Manifest.Entities, "refund") {
		t.Fatalf("the branch's manifest: %+v", m.Manifest)
	}
	// A graph deleted on the branch is gone there and present on main
	seq = f.ok("tasks", "refunds", "bp.delete", "qry-myTasks", map[string]any{}).Seq
	if _, ok := f.read("refunds", drafts.DraftProjector, "graph.qry-myTasks", seq); ok {
		t.Fatal("the deleted graph shows on the branch")
	}
	if _, ok := f.read("", drafts.DraftProjector, "graph.qry-myTasks", seq); !ok {
		t.Fatal("the graph is gone from main")
	}

	// A commit holds only what the branch changed
	res = f.ok("tasks", "", "app.commit", "", map[string]any{"app": "tasks", "branch": "refunds", "message": "Refunds", "expectedHead": br.Head})
	_ = f.p.Settle(context.Background(), 1, branches.Projector)
	_ = f.p.Settle(context.Background(), 1, branches.CommitProjector)
	br, _ = branches.GetRecord(context.Background(), f.p, 1, "tasks", "refunds")
	c, _ := branches.GetCommit(context.Background(), f.p, 1, br.Head)
	keys = nil
	for _, p := range c.Parts {
		keys = append(keys, fmt.Sprintf("%s:%v", p.Key(), p.Removed))
	}
	if c.Parent != main.Head || !slices.Equal(keys, []string{"app.tasks:false", "entity.refund:false", "graph.qry-myTasks:true", "prefab.myTasks:false"}) {
		t.Fatalf("the commit: parent %s, parts %v", c.Parent, keys)
	}
	// A racing commit is refused
	f.ok("tasks", "refunds", "prefab.setInfo", "myTasks", map[string]any{"description": "Again"})
	f.refused("app_head_moved", "tasks", "", "app.commit", "", map[string]any{"app": "tasks", "branch": "refunds", "message": "Late", "expectedHead": main.Head})
	// Discarding goes back to the head
	seq = f.ok("tasks", "", "app.discard", "", map[string]any{"app": "tasks", "branch": "refunds", "type": "prefab", "id": "myTasks"}).Seq
	if v, _ := f.read("refunds", prefabs.DraftProjector, "prefab.myTasks", seq); description(v) != "Mine, on the branch" {
		t.Fatalf("discarded: %s", v)
	}
	f.refused("nothing_to_commit", "tasks", "", "app.commit", "", map[string]any{"app": "tasks", "branch": "refunds", "message": "Nothing"})
	// main's change since its first commit is main's to commit
	res = f.ok("tasks", "", "app.commit", "", map[string]any{"app": "tasks", "message": "All tasks described"})
	_ = f.p.Settle(context.Background(), 1, branches.CommitProjector)
	_ = f.p.Settle(context.Background(), 1, branches.Projector)
	main, _ = branches.GetRecord(context.Background(), f.p, 1, "tasks", "main")
	c, _ = branches.GetCommit(context.Background(), f.p, 1, main.Head)
	if len(c.Parts) != 1 || c.Parts[0].Key() != "prefab.allTasks" {
		t.Fatalf("main's second commit: %+v", c.Parts)
	}
	// A part read at an earlier commit is as it was then
	at := branches.PartAt(f.reg, func() branches.Deps { return branches.Deps{Projections: f.p} })
	got, ok, err := at(ada, first.ID, live.Target{Area: live.AreaDefinitions, Type: "prefab", ID: "allTasks"})
	if err != nil || !ok || description(got.Value) != "" {
		t.Fatalf("allTasks at the first commit: %s %v %v", got.Value, ok, err)
	}
	// Listings of main never show a branch's drafts
	data, _, fail := f.subs.Once(ada, "prefab.list", json.RawMessage(`{}`))
	if fail != nil {
		t.Fatal(fail.Message)
	}
	raw, _ := json.Marshal(data)
	if json.Valid(raw) && containsKey(raw, "__b_") {
		t.Fatalf("prefab.list shows a branch: %s", raw)
	}
	// The branches query lists both, with their changes
	data, _, fail = f.subs.Once(ada, "app.branches", json.RawMessage(`{"app":"tasks"}`))
	if fail != nil {
		t.Fatal(fail.Message)
	}
	var bs branches.Branches
	raw, _ = json.Marshal(data)
	_ = json.Unmarshal(raw, &bs)
	if len(bs.Branches) != 2 || bs.Branches[0].Name != "main" || bs.Branches[1].Name != "refunds" || len(bs.Branches[1].Changed) != 0 {
		t.Fatalf("app.branches: %s", raw)
	}
}

func containsKey(raw []byte, s string) bool {
	return len(raw) > 0 && json.Valid(raw) && (string(raw) != "" && indexOf(string(raw), s) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestBranchOpsAndUndo(t *testing.T) {
	f := setup(t)
	f.tasks()
	f.ok("tasks", "", "app.branch.create", "", map[string]any{"app": "tasks", "name": "rename"})
	// A batch on the branch logs every op with it
	tr := f.bus.Transact(bus.Request{ServiceArgs: ada, ID: "batch1", RequestID: "round-1", App: "tasks", Branch: "rename"}, []live.Op{
		live.OpOf("prefab.setInfo", "myTasks", map[string]any{"description": "Batch on the branch"}),
		live.OpOf("app.updatePage", "tasks", map[string]any{"route": "/mine", "title": "My work"}),
	}, false)
	if tr.Common.Code >= 300 {
		t.Fatalf("the batch: %s %+v", tr.Common.Message, tr.Problems)
	}
	f.ok("tasks", "", "prefab.setInfo", "myTasks", map[string]any{"description": "On main"})
	entries, err := ops.Entries(ada, f.p, "tasks")
	if err != nil {
		t.Fatal(err)
	}
	var onBranch int
	for _, e := range entries {
		if e.Request == "round-1" {
			onBranch++
			if e.Branch != "rename" {
				t.Fatalf("an op of the batch is on %q", e.Branch)
			}
		}
	}
	if onBranch != 2 {
		t.Fatalf("%d ops of the batch were logged", onBranch)
	}
	// Undoing the batch undoes it on the branch alone
	res, fail := ops.Undo(ada, ops.Deps{Bus: f.bus, Store: f.p}, ops.UndoArgs{Request: "round-1"}, false, "")
	if fail != nil || res.Status != "done" {
		t.Fatalf("undo: %+v %v", res, fail)
	}
	if v, _ := f.read("rename", prefabs.DraftProjector, "prefab.myTasks", res.Seq); description(v) != "" {
		t.Fatalf("on the branch after undo: %s", v)
	}
	if v, _ := f.read("", prefabs.DraftProjector, "prefab.myTasks", res.Seq); description(v) != "On main" {
		t.Fatalf("main after undo: %s", v)
	}
	var m manifests.Draft
	v, _ := f.read("rename", manifests.DraftProjector, "app.tasks", res.Seq)
	_ = json.Unmarshal(v, &m)
	if m.Manifest.Pages[0].Title != "Mine" {
		t.Fatalf("the page on the branch after undo: %+v", m.Manifest.Pages[0])
	}
	// The branch's undo stack is its own: main's scope has main's requests
	title := func(c string) string { return c }
	reqs := ops.Requests(mustEntries(t, f))
	mainStack := ops.StackOf(reqs, ops.Scope{Kind: "app", ID: "tasks"}, 1, title)
	branchStack := ops.StackOf(reqs, ops.Scope{Kind: "app", ID: "tasks", Branch: "rename"}, 1, title)
	for _, it := range mainStack.Undo {
		if it.Request == "round-1" || it.Request == res.Sent {
			t.Fatalf("main's undo stack holds the branch's %s", it.Request)
		}
	}
	if len(branchStack.Redo) != 1 || branchStack.Redo[0].Request != res.Sent {
		t.Fatalf("the branch's stacks: %+v", branchStack)
	}
}

func mustEntries(t *testing.T, f *fixture) []ops.Entry {
	e, err := ops.Entries(ada, f.p, "tasks")
	if err != nil {
		t.Fatal(err)
	}
	return e
}
