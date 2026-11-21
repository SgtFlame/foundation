package ops_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/apps/ops"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
)

var bob = as(2, 1)

func (f *fixture) deps() ops.Deps { return ops.Deps{Bus: f.bus, Store: f.p} }

// undo undoes (or redoes) as args, failing the test on a refusal.
func (f *fixture) undo(args features.ServiceArgs, a ops.UndoArgs, redo bool) ops.UndoResult {
	f.t.Helper()
	res, fail := ops.Undo(args, f.deps(), a, redo, "")
	if fail != nil {
		f.t.Fatalf("undo %+v: %s", a, fail.Message)
	}
	return res
}

// stack is a principal's stacks in a scope, newest first, by request.
func (f *fixture) stack(args features.ServiceArgs, scope string) (undo, redo []string) {
	f.t.Helper()
	s, err := ops.ParseScope(scope)
	if err != nil {
		f.t.Fatal(err)
	}
	st := ops.StackOf(ops.Requests(f.entries(args, "")), s, args.Principal.UserID, func(c string) string { return c })
	for _, i := range st.Undo {
		undo = append(undo, i.Request)
	}
	for _, i := range st.Redo {
		redo = append(redo, i.Request)
	}
	return undo, redo
}

// app is the Tasks draft as it is once every event is folded.
func (f *fixture) app(id string) manifests.Draft {
	f.t.Helper()
	v, err := f.read(manifests.DraftProjector, manifests.TargetType+"."+id, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	var d manifests.Draft
	_ = json.Unmarshal(v.Value, &d)
	return d
}

func (f *fixture) prefab(id string) prefabs.Draft {
	f.t.Helper()
	v, err := f.read(prefabs.DraftProjector, prefabs.TargetType+"."+id, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	var d prefabs.Draft
	_ = json.Unmarshal(v.Value, &d)
	return d
}

func routes(m apps.Manifest) []string {
	var out []string
	for _, p := range m.Pages {
		out = append(out, p.Route)
	}
	return out
}

func roleIDs(m apps.Manifest) []string {
	var out []string
	for _, r := range m.Roles {
		out = append(out, r.ID)
	}
	return out
}

// TestUndoTwiceRedoOnce builds the Tasks manifest in three requests, then
// undoes twice and redoes once: the draft is as the second request left
// it, and the stacks say so.
func TestUndoTwiceRedoOnce(t *testing.T) {
	f := setup(t)
	f.parts(ada, "parts")
	send := func(request, typ string, a any) { f.send(ada, request, "", typ, "tasks", a) }
	f.send(ada, "m1", "", "app.create", "", m{"name": "Tasks"})
	send("m1", "app.addEntity", m{"entity": "task"})
	send("m2", "app.addPage", m{"route": "/mine", "prefab": "myTasks"})
	send("m2", "app.addPage", m{"route": "/all", "prefab": "allTasks"})
	second := f.state("app.x", "tasks", 0)
	send("m3", "app.addRole", m{"id": "manager", "actions": []string{"entity:task.*"}})
	send("m3", "app.setInfo", m{"home": "/all"})

	st := f.open(ada, ops.Params{App: "tasks"}) // the log keeps up as undo writes
	_ = st
	first := f.undo(ada, ops.UndoArgs{Scope: "app:tasks"}, false)
	if first.Status != "done" || first.Request != "m3" || len(first.Ops) != 2 {
		t.Fatalf("the first undo: %s", mustJSON(first))
	}
	if got := f.app("tasks").Manifest; got.Home != "" || len(got.Roles) != 0 {
		t.Fatalf("m3 not undone: %s", mustJSON(got))
	}
	if r := f.undo(ada, ops.UndoArgs{Scope: "app:tasks"}, false); r.Request != "m2" {
		t.Fatalf("the second undo undid %s", r.Request)
	}
	if got := routes(f.app("tasks").Manifest); len(got) != 0 {
		t.Fatalf("m2 not undone: pages %v", got)
	}
	undo, redo := f.stack(ada, "app:tasks")
	if len(redo) != 2 || undo[0] != "m1" {
		t.Fatalf("after two undos: undo %v, redo %v", undo, redo)
	}
	r := f.undo(ada, ops.UndoArgs{Scope: "app:tasks"}, true)
	if r.Status != "done" || !strings.HasPrefix(r.Sent, "redo-") || !strings.Contains(r.Summary, "/mine") {
		t.Fatalf("the redo: %s", mustJSON(r))
	}
	if got := f.state("app.x", "tasks", 0); got != second {
		t.Fatalf("after undo, undo, redo the draft is\n  %s\nnot as m2 left it\n  %s", got, second)
	}
	undo, redo = f.stack(ada, "app:tasks")
	if len(redo) != 1 || undo[0] != r.Sent || undo[1] != "m1" {
		t.Fatalf("after the redo: undo %v, redo %v", undo, redo)
	}
	// Undoing the redo undoes m2 again; a new request empties the redo list
	if r := f.undo(ada, ops.UndoArgs{Scope: "app:tasks"}, false); r.Request != undo[0] {
		t.Fatalf("undoing the redo undid %s", r.Request)
	}
	if got := routes(f.app("tasks").Manifest); len(got) != 0 {
		t.Fatalf("the redo not undone: pages %v", got)
	}
	send("m4", "app.setInfo", m{"description": "Mine"})
	if _, redo := f.stack(ada, "app:tasks"); len(redo) != 0 {
		t.Fatalf("a new request left the redo list %v", redo)
	}
}

// TestUndoLeavesAnotherAuthorsChange: Ada's request adds a page and a
// role; Bob then renames a field. Ada's undo removes her page and role,
// and Bob's rename stays.
func TestUndoLeavesAnotherAuthorsChange(t *testing.T) {
	f := setup(t)
	f.parts(ada, "parts")
	f.manifest(ada, "manifest")
	f.send(ada, "ada", "", "app.addPage", "tasks", m{"route": "/extra", "prefab": "allTasks"})
	f.send(ada, "ada", "", "app.addRole", "tasks", m{"id": "viewer", "actions": []string{"entity:task.read"}})
	f.send(bob, "bob", "tasks", "entity.updateField", "task", m{"field": "title", "name": "heading"})

	if undo, _ := f.stack(bob, "app:tasks"); len(undo) != 1 || undo[0] != "bob" {
		t.Fatalf("Bob's stack reaches %v, not only his own request", undo)
	}
	r := f.undo(ada, ops.UndoArgs{Scope: "app:tasks"}, false)
	if r.Status != "done" || r.Request != "ada" {
		t.Fatalf("Ada's undo: %s", mustJSON(r))
	}
	got := f.app("tasks").Manifest
	if slices.Contains(routes(got), "/extra") || slices.Contains(roleIDs(got), "viewer") {
		t.Fatalf("Ada's page and role remain: %v %v", routes(got), roleIDs(got))
	}
	v, _ := f.read(entities.DraftProjector, entities.TargetType+".task", 0)
	var d entities.Draft
	_ = json.Unmarshal(v.Value, &d)
	if fld, _, _ := d.Entity.FieldByID("title"); fld.Name != "heading" {
		t.Fatalf("Bob's rename was lost: the field is %q", fld.Name)
	}
}

// TestUndoConflict: Bob changes the role Ada's request added. Ada's undo is
// refused naming the role and Bob; forced, her page goes and the role
// stays as Bob left it.
func TestUndoConflict(t *testing.T) {
	f := setup(t)
	f.parts(ada, "parts")
	f.manifest(ada, "manifest")
	f.send(ada, "ada", "", "app.addPage", "tasks", m{"route": "/extra", "prefab": "allTasks"})
	f.send(ada, "ada", "", "app.addRole", "tasks", m{"id": "viewer", "actions": []string{"entity:task.read"}})
	f.send(bob, "bob", "", "app.updateRole", "tasks", m{"id": "viewer", "title": "Viewers"})

	r := f.undo(ada, ops.UndoArgs{Scope: "app:tasks"}, false)
	if r.Status != "conflicts" || len(r.Conflicts) != 1 {
		t.Fatalf("the undo was not refused: %s", mustJSON(r))
	}
	c := r.Conflicts[0]
	if c.Item.Kind != "role" || c.Item.Key != "viewer" || c.Principal != 2 || c.Request != "bob" {
		t.Fatalf("the conflict names %s", mustJSON(c))
	}
	if !slices.Contains(routes(f.app("tasks").Manifest), "/extra") {
		t.Fatal("a refused undo changed the draft")
	}
	preview := f.undo(ada, ops.UndoArgs{Scope: "app:tasks", Force: true, DryRun: true}, false)
	if preview.Status != "preview" || len(preview.Ops) != 1 || !slices.Contains(routes(f.app("tasks").Manifest), "/extra") {
		t.Fatalf("the forced preview: %s", mustJSON(preview))
	}
	r = f.undo(ada, ops.UndoArgs{Scope: "app:tasks", Force: true}, false)
	if r.Status != "done" || len(r.Skipped) != 1 || r.Skipped[0].Command != "app.addRole" {
		t.Fatalf("the forced undo: %s", mustJSON(r))
	}
	got := f.app("tasks").Manifest
	if slices.Contains(routes(got), "/extra") {
		t.Fatal("the page stayed")
	}
	i := slices.IndexFunc(got.Roles, func(r apps.Role) bool { return r.ID == "viewer" })
	if i < 0 || got.Roles[i].Title != "Viewers" {
		t.Fatalf("the role is not as Bob left it: %s", mustJSON(got.Roles))
	}
}

// TestUndoAfterPublishing: a request that edited and published a prefab is
// undone in its draft, the publish named as not undone, and the published
// version stays.
func TestUndoAfterPublishing(t *testing.T) {
	f := setup(t)
	f.send(ada, "setup", "", "prefab.create", "card", m{"name": "Card"})
	f.send(ada, "setup", "", "prefab.insertNode", "card", m{"node": m{"type": "mui.text", "key": "root", "props": m{"text": "Hello"}}})
	f.send(ada, "pub", "", "prefab.updateNode", "card", m{"path": "root", "props": m{"text": "Hi"}})
	f.send(ada, "pub", "", "prefab.publish", "card", m{"rev": f.rev("prefab.x", "card", 0)})

	r := f.undo(ada, ops.UndoArgs{Request: "pub"}, false)
	if r.Status != "done" || len(r.Skipped) != 1 || r.Skipped[0].Command != "prefab.publish" {
		t.Fatalf("the undo: %s", mustJSON(r))
	}
	if text := f.prefab("card").Prefab.Template.Props["text"]; text != "Hello" {
		t.Fatalf("the draft's text is %v", text)
	}
	v, _ := f.read(prefabs.PublishedProjector, prefabs.TargetType+".card", 0)
	var p ui.Prefab
	_ = json.Unmarshal(v.Value, &p)
	if p.Version != 1 || p.Template.Props["text"] != "Hi" {
		t.Fatalf("the published version changed: %s", mustJSON(p))
	}
}

// TestUndoCreation: undoing app.create hides the app and its own parts,
// and redo brings them back; undoing the making of a prefab a published
// prefab uses is refused, naming it.
func TestUndoCreation(t *testing.T) {
	f := setup(t)
	f.send(ada, "make", "", "app.create", "", m{"name": "Notes"})
	f.send(ada, "make", "notes", "prefab.create", "noteList", m{"name": "Note list", "owner": "app:notes"})
	f.send(ada, "make", "", "app.addPage", "notes", m{"route": "/list", "prefab": "noteList"})

	if r := f.undo(ada, ops.UndoArgs{Scope: "app:notes"}, false); r.Status != "done" || len(r.Ops) != 3 {
		t.Fatalf("undoing the making: %s", mustJSON(r))
	}
	if !f.app("notes").Deleted || !f.prefab("noteList").Deleted {
		t.Fatal("the app and its prefab are not hidden")
	}
	if r := f.undo(ada, ops.UndoArgs{Scope: "app:notes"}, true); r.Status != "done" {
		t.Fatalf("the redo: %s", mustJSON(r))
	}
	if d := f.app("notes"); d.Deleted || f.prefab("noteList").Deleted || !slices.Contains(routes(d.Manifest), "/list") {
		t.Fatalf("the redo did not bring them back: %s", mustJSON(d))
	}

	f.send(ada, "lib", "", "prefab.create", "badge", m{"name": "Badge"})
	f.send(ada, "lib", "", "prefab.insertNode", "badge", m{"node": m{"type": "mui.chip", "key": "root", "props": m{"label": "New"}}})
	f.send(ada, "use", "", "prefab.publish", "badge", m{"rev": f.rev("prefab.x", "badge", 0)})
	f.send(ada, "use", "", "prefab.create", "card", m{"name": "Card"})
	f.send(ada, "use", "", "prefab.insertNode", "card", m{"node": m{"type": "prefab.badge", "key": "root"}})
	f.send(ada, "use", "", "prefab.publish", "card", m{"rev": f.rev("prefab.x", "card", 0)})
	_, fail := ops.Undo(ada, f.deps(), ops.UndoArgs{Request: "lib"}, false, "")
	if fail == nil || !strings.Contains(fail.Message, "card") {
		t.Fatalf("undoing the badge's making was not refused for the card: %+v", fail)
	}
	if f.prefab("badge").Deleted || f.prefab("badge").Prefab.Template == nil {
		t.Fatal("a refused undo changed the badge")
	}
}

// TestUndoScopes: a request touching a prefab and the manifest is in both
// stacks; a new request empties the redo list of its own scopes only.
func TestUndoScopes(t *testing.T) {
	f := setup(t)
	f.parts(ada, "parts")
	f.manifest(ada, "manifest")
	f.send(ada, "both", "tasks", "prefab.insertNode", "myTasks", m{"node": m{"type": "mui.stack", "key": "root"}})
	f.send(ada, "both", "", "app.updatePage", "tasks", m{"route": "/mine", "title": "Mine"})
	for _, scope := range []string{"prefab:myTasks", "app:tasks/manifest", "app:tasks"} {
		if undo, _ := f.stack(ada, scope); len(undo) == 0 || undo[0] != "both" {
			t.Fatalf("%s's stack starts %v", scope, undo)
		}
	}
	if undo, _ := f.stack(ada, "prefab:allTasks"); slices.Contains(undo, "both") {
		t.Fatalf("another prefab's stack holds %v", undo)
	}
	r := f.undo(ada, ops.UndoArgs{Scope: "prefab:myTasks"}, false)
	if r.Request != "both" || f.prefab("myTasks").Prefab.Template != nil {
		t.Fatalf("the undo: %s", mustJSON(r))
	}
	f.send(ada, "p2", "tasks", "prefab.setInfo", "myTasks", m{"description": "Mine"})
	if _, redo := f.stack(ada, "prefab:myTasks"); len(redo) != 0 {
		t.Fatalf("the prefab's redo list is %v after a new request", redo)
	}
	if _, redo := f.stack(ada, "app:tasks/manifest"); len(redo) != 1 || redo[0] != r.Sent {
		t.Fatalf("the manifest's redo list is %v, not the undo", redo)
	}
}

// stackSink collects an ops.stack subscription's result.
type stackSink struct {
	got chan ops.Stack
}

func (s *stackSink) Snapshot(data any, _ live.Position) {
	raw, _ := json.Marshal(data)
	var st ops.Stack
	_ = json.Unmarshal(raw, &st)
	s.got <- st
}
func (s *stackSink) Change(d live.Delta, _ live.Position) {
	if d.Op == "replace" {
		var st ops.Stack
		_ = json.Unmarshal(d.Value, &st)
		s.got <- st
	}
}
func (s *stackSink) Failed(resp features.ServiceResponse) { panic(resp.Message) }

// TestStackQuery: ops.stack follows the caller's requests and undos.
func TestStackQuery(t *testing.T) {
	f := setup(t)
	f.parts(ada, "parts")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	snk := &stackSink{got: make(chan ops.Stack, 100)}
	args := ada
	args.Ctx = ctx
	if fail := f.subs.Open(subscribe.OpenArgs{ServiceArgs: args, Query: "ops.stack", Params: json.RawMessage(`{"scope":"prefab:myTasks"}`), Sink: snk}); fail != nil {
		t.Fatal(fail.Message)
	}
	wait := func(ok func(ops.Stack) bool) ops.Stack {
		t.Helper()
		for st := range snk.got {
			if ok(st) {
				return st
			}
		}
		return ops.Stack{}
	}
	wait(func(s ops.Stack) bool { return len(s.Undo) == 1 && s.Undo[0].Request == "parts" })
	f.send(ada, "g1", "tasks", "prefab.insertNode", "myTasks", m{"node": m{"type": "mui.stack", "key": "root"}})
	st := wait(func(s ops.Stack) bool { return len(s.Undo) == 2 })
	if st.Undo[0].Request != "g1" || !strings.Contains(st.Undo[0].Summary, "Insert a node") {
		t.Fatalf("the stack after g1: %s", mustJSON(st))
	}
	f.undo(ada, ops.UndoArgs{Scope: "prefab:myTasks"}, false)
	st = wait(func(s ops.Stack) bool { return len(s.Redo) == 1 })
	if !strings.Contains(st.Redo[0].Summary, "Insert a node") || len(st.Undo) != 1 {
		t.Fatalf("the stack after undo: %s", mustJSON(st))
	}
}

// TestStatusOfARound: a chat round's status follows its undo and redo.
func TestStatusOfARound(t *testing.T) {
	f := setup(t)
	f.parts(ada, "parts")
	f.send(ada, "round-7", "tasks", "prefab.setInfo", "myTasks", m{"description": "Mine"})
	f.send(ada, "round-7", "tasks", "prefab.publish", "myTasks", m{"rev": f.rev("prefab.x", "myTasks", 0)})
	status := func() ops.Status { return ops.StatusOf(ops.Requests(f.entries(ada, "")), "round-7") }
	if s := status(); s.Ops != 2 || s.Undoable != 1 || s.Undone {
		t.Fatalf("before: %+v", s)
	}
	preview := f.undo(ada, ops.UndoArgs{Request: "round-7", DryRun: true}, false)
	if preview.Status != "preview" || len(preview.Skipped) != 1 || status().Undone || len(preview.Changes) != 1 || preview.Changes[0] != "Describe a prefab (myTasks description)" {
		t.Fatalf("the preview: %s", mustJSON(preview))
	}
	u := f.undo(ada, ops.UndoArgs{Request: "round-7"}, false)
	if s := status(); !s.Undone || s.UndoneBy != u.Sent {
		t.Fatalf("after the undo: %+v", s)
	}
	if r := f.undo(ada, ops.UndoArgs{Request: "round-7"}, true); r.Status != "done" || r.Request != u.Sent {
		t.Fatalf("redoing the round: %s", mustJSON(r))
	}
	if s := status(); s.Undone {
		t.Fatalf("after the redo: %+v", s)
	}
	if f.prefab("myTasks").Prefab.Description != "Mine" {
		t.Fatal("the redo did not bring the change back")
	}
}
