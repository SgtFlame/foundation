package jobs_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/jobs"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
)

// notes makes the Notes app: its own note type and a trigger graph per
// event node given, published and installed.
func (f *fixture) notes(triggers map[string]map[string]any) {
	f.t.Helper()
	own := "app:notes"
	f.ok("entity.create", "note", map[string]any{"name": "Note", "owner": own, "fields": []map[string]any{{"name": "text", "type": "text"}}})
	res := f.ok("entity.publish", "note", map[string]any{"rev": 1})
	f.wait(entities.PublishedProjector, "entity.note", res.Seq)
	f.ok("app.create", "", map[string]any{"name": "Notes"})
	f.ok("app.addEntity", "notes", map[string]any{"entity": "note"})
	f.ok("app.addRole", "notes", map[string]any{"id": "member", "actions": []string{"entity:note.*"}})
	for id, event := range triggers {
		g := map[string]any{"name": "On " + id, "kind": livenodes.KindTrigger, "owner": own,
			"nodes": []any{event, node("hit", "test.hit", map[string]any{"name": id})},
			"edges": edges("event.then -> hit.then", "event."+map[bool]string{true: "time", false: "record"}[event["type"] == livenodes.OnSchedule]+" -> hit.value"),
		}
		f.publishGraph("notes-"+id, g)
		f.ok("app.addTrigger", "notes", map[string]any{"id": id, "graph": "notes-" + id})
	}
	var d manifests.Draft
	seq := f.ok("app.setInfo", "notes", map[string]any{"description": "Notes"}).Seq
	_ = json.Unmarshal(f.wait(manifests.DraftProjector, "app.notes", seq).Value, &d)
	res = f.ok("app.publish", "notes", map[string]any{"rev": d.Rev})
	f.wait(manifests.VersionProjector, "appVersion."+apps.VersionID("notes", 1), res.Seq)
	f.lifecycle("app.install", map[string]any{})
}

func (f *fixture) lifecycle(typ string, a any) {
	f.t.Helper()
	res := f.ok(typ, "notes", a)
	f.wait(installs.Projector, "appInstall.notes", res.Seq)
}

func (f *fixture) addNote(id, text string) {
	f.t.Helper()
	f.ok("record.create", id, map[string]any{"entity": "note", "values": map[string]any{"text": text}})
}

// A trigger on record changes runs its graph as the app's principal for
// each change, once, across a restart; an uninstall stops it.
func TestTriggerOnRecordChanged(t *testing.T) {
	f := setup(t, fast)
	f.notes(map[string]map[string]any{"noted": node("event", "records.onChanged", map[string]any{"entity": "note", "events": []string{"created"}})})
	a := f.start("a", time.Second)
	// Made before the consumer existed, after the install: still seen
	f.addNote("n1", "first")
	got := f.hits.wait(t, "noted", 1, 5*time.Second)
	if got[0].Principal != features.AppPrincipal(features.LocalTenantID, "notes") {
		t.Errorf("ran as %+v", got[0].Principal)
	}
	if rec, _ := got[0].Value.(map[string]any); rec["text"] != "first" {
		t.Errorf("record: %+v", got[0].Value)
	}
	j := f.job(got[0].Job)
	if j.App != "notes" || j.Trigger != "noted" || j.Event != "records.onChanged" || j.Status == "" {
		t.Errorf("the trigger's job: %+v", j)
	}
	// An update is not a create
	f.ok("record.update", "n1", map[string]any{"entity": "note", "set": map[string]any{"text": "changed"}})

	// The runner stops; a note made meanwhile runs once another starts
	a.Close()
	f.addNote("n2", "second")
	f.start("b", time.Second)
	f.start("c", time.Second)
	got = f.hits.wait(t, "noted", 2, 5*time.Second)
	time.Sleep(800 * time.Millisecond)
	if got = f.hits.named("noted"); len(got) != 2 {
		t.Fatalf("%d runs, want 2", len(got))
	}
	if rec, _ := got[1].Value.(map[string]any); rec["text"] != "second" {
		t.Errorf("the second run: %+v", got[1].Value)
	}

	// Uninstalled, it stops
	f.lifecycle("app.uninstall", map[string]any{})
	time.Sleep(600 * time.Millisecond) // the runners see the uninstall
	f.addNote("n3", "third")
	time.Sleep(1200 * time.Millisecond)
	if n := len(f.hits.named("noted")); n != 2 {
		t.Errorf("%d runs after the uninstall", n)
	}
	for _, area := range []string{"rec"} {
		s, _ := f.p.JetStream().Stream(context.Background(), fmt.Sprintf("EVT_1_%s", area))
		names := s.ConsumerNames(context.Background())
		for n := range names.Name() {
			if strings.HasPrefix(n, "trg_") {
				t.Errorf("consumer %s is left after the uninstall", n)
			}
		}
	}
}

// On schedule ticks once per interval however many runners there are,
// and stops when the app is uninstalled.
func TestScheduleTicksOnce(t *testing.T) {
	f := setup(t, fast)
	f.start("a", time.Second)
	f.start("b", time.Second)
	f.notes(map[string]map[string]any{"tick": node("event", livenodes.OnSchedule, map[string]any{"every": "1s"})})
	got := f.hits.wait(t, "tick", 3, 8*time.Second)
	seen := map[string]bool{}
	for _, h := range got {
		if seen[h.Job] {
			t.Errorf("tick %s ran twice", h.Job)
		}
		seen[h.Job] = true
		if h.Principal.Kind != features.PrincipalApp {
			t.Errorf("a tick ran as %+v", h.Principal)
		}
	}
	if span := got[len(got)-1].Start.Sub(got[0].Start); span < 1500*time.Millisecond {
		t.Errorf("three ticks within %v", span)
	}
	f.lifecycle("app.uninstall", map[string]any{})
	time.Sleep(1500 * time.Millisecond)
	n := len(f.hits.named("tick"))
	time.Sleep(2 * time.Second)
	if m := len(f.hits.named("tick")); m > n {
		t.Errorf("%d ticks after the uninstall", m-n)
	}
}

// A command graph's job is enqueued if and only if its command commits.
func TestCommandOutbox(t *testing.T) {
	f := setup(t, fast)
	f.ok("entity.create", "memo", map[string]any{"name": "Memo", "fields": []map[string]any{{"name": "text", "type": "text"}}})
	res := f.ok("entity.publish", "memo", map[string]any{"rev": 1})
	f.wait(entities.PublishedProjector, "entity.memo", res.Seq)
	f.publishGraph("work", action("after", nil))
	f.publishGraph("cmd-memo-make", map[string]any{"name": "Make a memo", "kind": livenodes.KindCommand,
		"nodes": []any{
			node("event", livenodes.OnCommand, map[string]any{"outputs": []any{map[string]any{"id": "reject", "type": "bool"}}}),
			node("branch", "flow.branch", nil),
			node("no", "live.reject", nil),
			node("why", "data.expression", map[string]any{"expr": `"refused"`, "type": "string"}),
			node("make", "records.create", map[string]any{"entity": "memo"}),
			node("inputs", "data.expression", map[string]any{"expr": `{value: target}`, "type": "json"}),
			node("enqueue", "jobs.enqueue", map[string]any{"graph": "work"}),
		},
		"edges": edges("event.then -> branch.then", "event.reject -> branch.condition", "branch.true -> no.then", "why.value -> no.message",
			"branch.false -> enqueue.then", "event.target -> inputs.target", "inputs.value -> enqueue.inputs", "enqueue.then -> make.then", "event.target -> make.id"),
		"spec": livenodes.CommandSpec{Command: "memo.make", Title: "Make a memo", Description: "Makes a memo, then works on it",
			Args: json.RawMessage(`{"type":"object","properties":{"reject":{"type":"boolean"}}}`),
			Area: "rec", TargetType: "memo", Projection: data.Projector, Action: "record.create", Events: []string{"created"}},
	})
	f.start("a", time.Second)
	f.ok("memo.make", "m1", map[string]any{"reject": false})
	if res := f.do("memo.make", "m2", map[string]any{"reject": true}); res.Common.Code != 422 {
		t.Fatalf("the refused command: %d", res.Common.Code)
	}
	got := f.hits.wait(t, "after", 1, 5*time.Second)
	if got[0].Value != "m1" || got[0].Principal != features.LocalPrincipal() {
		t.Errorf("the job: %+v", got[0])
	}
	time.Sleep(800 * time.Millisecond)
	if n := len(f.hits.named("after")); n != 1 {
		t.Errorf("%d jobs ran, want 1", n)
	}
	// A second process finds the area's outbox and takes over
	f.start("b", time.Second)
	time.Sleep(500 * time.Millisecond)
	f.ok("memo.make", "m3", map[string]any{"reject": false})
	got = f.hits.wait(t, "after", 2, 5*time.Second)
	if got[1].Value != "m3" {
		t.Errorf("the second job: %+v", got[1])
	}
}

// jobs.list shows the jobs a user may see, live.
func TestJobsList(t *testing.T) {
	f := setup(t, fast)
	f.publishGraph("work", action("listed", nil))
	f.enqueue(jobs.Request{ID: "mine", Graph: "work"})
	other := features.Principal{Kind: features.PrincipalUser, UserID: 7, TenantID: features.LocalTenantID}
	if _, err := f.runner.Enqueue(features.ServiceArgs{Ctx: context.Background(), Principal: other}, jobs.Request{ID: "theirs", Graph: "work"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"status": "queued"})
	v, _, fail := f.subs.Once(args(), "jobs.list", raw)
	if fail != nil {
		t.Fatal(fail.Message)
	}
	b, _ := json.Marshal(v)
	var list map[string]jobs.Job
	_ = json.Unmarshal(b, &list)
	// No policy is installed here, so the local user holds job.manage
	if len(list) != 2 || list["job.mine"].Graph != "work" {
		t.Errorf("list: %+v", list)
	}
}

// A trigger names its app's own app.trigger graph.
func TestTriggerGraphChecked(t *testing.T) {
	f := setup(t, fast)
	f.publishGraph("work", action("x", nil))
	f.publishGraph("other-trg", map[string]any{"name": "Theirs", "kind": livenodes.KindTrigger, "owner": "app:other",
		"nodes": []any{node("event", livenodes.OnSchedule, map[string]any{"every": "1m"})}, "edges": []any{}})
	f.ok("app.create", "", map[string]any{"name": "Notes"})
	for graph, want := range map[string]string{"work": "not an app.trigger graph", "other-trg": "not this app's own", "nope": "there is no graph"} {
		res := f.do("app.addTrigger", "notes", map[string]any{"id": "t", "graph": graph})
		if res.Common.Code != 422 || !strings.Contains(res.Problems[0].Message, want) {
			t.Errorf("trigger on %s: %d %+v", graph, res.Common.Code, res.Problems)
		}
	}
	f.ok("app.addTrigger", "notes", map[string]any{"id": "declared"}) // declared only
}

// With access control installed, an app's roles are synced by a follower
// after the install commits, perhaps in another process; a trigger that
// cannot read its records until then starts once it can.
func TestTriggerStartsOnceRolesSync(t *testing.T) {
	f := setup(t, fast)
	appDB, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "app.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(appDB, authz.Models()...); err != nil {
		t.Fatal(err)
	}
	authz.Contribute(f.reg)
	identity.ContributeAccess(f.reg)
	records.ContributeAccess(f.reg)
	cognition.ContributeAccess(f.reg)
	apps.ContributeAccess(f.reg)
	jobs.ContributeAccess(f.reg)
	pol := authz.NewPolicy(appDB, f.reg)
	if _, err := pol.Grant(context.Background(), nil, authz.GrantArgs{UserID: features.LocalUserID, RoleID: authz.RoleOwner, Resource: "tenant:1"}); err != nil {
		t.Fatal(err)
	}
	authz.Install(pol)
	t.Cleanup(func() { authz.Install(nil) })
	f.install.Roles = pol
	records.SetPlacement(installs.Placement(func() installs.Deps { return *f.install }))
	t.Cleanup(func() { records.SetPlacement(nil) })

	f.start("a", time.Second)
	f.notes(map[string]map[string]any{"noted": node("event", "records.onChanged", map[string]any{"entity": "note", "events": []string{"created"}})})
	time.Sleep(time.Second)
	f.addNote("n1", "after the roles")
	got := f.hits.wait(t, "noted", 1, 8*time.Second)
	if got[0].Principal.Kind != features.PrincipalApp {
		t.Errorf("ran as %+v", got[0].Principal)
	}
	if rec, _ := got[0].Value.(map[string]any); rec["text"] != "after the roles" {
		t.Errorf("the app principal read %+v", got[0].Value)
	}
}
