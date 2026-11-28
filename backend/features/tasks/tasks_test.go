package tasks_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	appimpl "github.com/SgtFlame/foundation/backend/features/apps/services/appservice/impl"
	"github.com/SgtFlame/foundation/backend/features/authz"
	authznodes "github.com/SgtFlame/foundation/backend/features/authz/nodes"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	cognitionnodes "github.com/SgtFlame/foundation/backend/features/cognition/nodes"
	"github.com/SgtFlame/foundation/backend/features/cognition/traces"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/jobs"
	jobsnodes "github.com/SgtFlame/foundation/backend/features/jobs/nodes"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	recordsnodes "github.com/SgtFlame/foundation/backend/features/records/nodes"
	"github.com/SgtFlame/foundation/backend/features/tasks"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/SgtFlame/foundation/backend/features/ui"
	uicomponents "github.com/SgtFlame/foundation/backend/features/ui/components"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
)

const (
	owner uint64 = 1
	alice uint64 = 2 // a member of Website relaunch
	gus   uint64 = 3 // a guest of Billing
	bob   uint64 = 4 // admin of Billing
)

type fixture struct {
	t       *testing.T
	reg     *extensions.Registry
	p       *project.Projections
	bus     *bus.Bus
	eng     *engine.Engine
	subs    *subscribe.Subscriptions
	data    *data.Deps
	pol     *authz.Policy
	runner  *jobs.Runner
	n       atomic.Int64
	botOnce sync.Once
}

type jobSettings struct{}

func (jobSettings) Resolved(features.ServiceArgs, string) (json.RawMessage, error) {
	return json.Marshal(jobs.Settings{MaxConcurrent: 4, MaxAttempts: 5, BackoffSeconds: 1, MaxBackoffSeconds: 1, DoneHours: 24})
}

// setup is the stack the Tasks app runs on in a server process: the live
// core, records with access (app roles, row rules, record grants, agent
// principals) and large tables, apps with their tools, the job runner and
// triggers; and the Tasks app seeded into tenant 1, twice.
func setup(t *testing.T) *fixture {
	reg := extensions.NewRegistry()
	reg.Declare(livenodes.TriggerEvents, tools.Tools, tools.Sources)
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
		jobDeps    jobs.Deps
		jobNodes   jobsnodes.Deps
	)
	livenodes.Contribute(reg, func() livenodes.Deps { return nodeDeps })
	drafts.Contribute(reg, func() drafts.Deps { return draftDeps })
	entities.Contribute(reg, func() entities.Deps { return entityDeps })
	data.Contribute(reg, func() data.Deps { return dataDeps })
	recordsnodes.Contribute(reg, func() data.Deps { return dataDeps })
	prefabs.Contribute(reg, func() prefabs.Deps { return prefabDeps })
	manifests.Contribute(reg, func() manifests.Deps { return appDeps })
	installs.Contribute(reg, func() installs.Deps { return installDep })
	installs.ContributeTools(reg, func() installs.Deps { return installDep })
	jobs.Contribute(reg, func() jobs.Deps { return jobDeps })
	jobsnodes.Contribute(reg, func() jobsnodes.Deps { return jobNodes })
	authznodes.Contribute(reg)
	authz.ContributeAgents(reg)

	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, slices.Concat(cognition.Models(), records.Models(), ui.Models(), apps.Models(), tools.Models(), config.Models())...); err != nil {
		t.Fatal(err)
	}
	srv := natsd.StartTest(t)
	p := project.New(srv.JS(), reg)
	t.Cleanup(p.Close)
	eng := engine.New(traces.In(gdb), reg, nil)
	eng.SetSource(drafts.Source{Store: p})
	f := &fixture{t: t, reg: reg, p: p, bus: bus.New(p), eng: eng, data: &dataDeps}
	f.subs = subscribe.New(p, reg)
	nodeDeps = livenodes.Deps{Engine: eng, Bus: f.bus, Subscriptions: f.subs, Store: p}
	draftDeps = drafts.Deps{Engine: eng, Projections: p}
	tenantDB, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "tenant.db")))
	if err != nil {
		t.Fatal(err)
	}
	tables, err := data.NewTables(tenantDB, p)
	if err != nil {
		t.Fatal(err)
	}
	dataDeps = data.Deps{Projections: p, Bus: f.bus, Tables: tables}
	entityDeps = entities.Deps{Projections: p, DB: gdb, OnPublish: func(args features.ServiceArgs) error { return data.Follow(args, dataDeps) }}
	prefabDeps = prefabs.Deps{Projections: p, DB: gdb}
	appDeps = manifests.Deps{Projections: p, DB: gdb}
	installDep = installs.Deps{Projections: p, DB: gdb, Nodes: nodeDeps}
	f.bus.Resolve = installs.CommandFor(func() installs.Deps { return installDep }, livenodes.CommandFor(func() livenodes.Deps { return nodeDeps }))
	f.subs.Resolve = installs.QueryFor(func() installs.Deps { return installDep }, livenodes.QueryFor(func() livenodes.Deps { return nodeDeps }))
	jobDeps = jobs.Deps{Projections: p, Bus: f.bus, Engine: eng, Config: jobSettings{}}

	// Access, before the app installs, so its roles are made
	appDB, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "app.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(appDB, authz.Models()...); err != nil {
		t.Fatal(err)
	}
	authz.Contribute(reg)
	records.ContributeAccess(reg)
	apps.ContributeAccess(reg)
	cognition.ContributeAccess(reg)
	ui.ContributeAccess(reg)
	ui.ContributePrefabAccess(reg)
	jobs.ContributeAccess(reg)
	pol := authz.NewPolicy(appDB, reg)
	authz.Install(pol)
	t.Cleanup(func() { authz.Install(nil) })
	if _, err := pol.Grant(context.Background(), nil, authz.GrantArgs{UserID: owner, RoleID: authz.RoleOwner, Resource: "tenant:1", By: owner}); err != nil {
		t.Fatal(err)
	}
	f.pol = pol
	installDep.Roles = pol
	records.SetPlacement(installs.Placement(func() installs.Deps { return installDep }))
	t.Cleanup(func() { records.SetPlacement(nil) })
	authz.SetAgents(data.Agents{Deps: func() data.Deps { return dataDeps }})
	t.Cleanup(func() { authz.SetAgents(nil) })

	f.runner = jobs.New(jobDeps, jobs.Options{Name: "runner", AckWait: 5 * time.Second, Discover: 200 * time.Millisecond})
	t.Cleanup(f.runner.Close)
	jobNodes = jobsnodes.Deps{Runner: f.runner, Store: p}
	if err := f.runner.Serve(features.LocalTenantID); err != nil {
		t.Fatal(err)
	}

	svc := appimpl.New(appimpl.WithDeps(func() appimpl.Deps {
		return appimpl.Deps{Projections: p, DB: gdb, Bus: f.bus, Subscriptions: f.subs, Registry: reg}
	}))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	for i := range 2 {
		if err := tasks.SeedApp(features.ServiceArgs{Ctx: ctx}, features.LocalTenantID, p, svc, f.bus); err != nil {
			t.Fatalf("seed %d: %v", i+1, err)
		}
	}
	return f
}

func user(id uint64) features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, UserID: id, TenantID: features.LocalTenantID}}
}

func (f *fixture) doAs(as features.ServiceArgs, typ, target string, a any) bus.Result {
	raw, _ := json.Marshal(a)
	return f.bus.Execute(bus.Request{ServiceArgs: as, ID: fmt.Sprintf("c%d-%d", time.Now().UnixNano(), f.n.Add(1)), Type: typ, Target: target, Args: raw})
}

// okOn sends a command as a person and waits until its target is projected.
func (f *fixture) okOn(as uint64, typ, entity, target string, a any) bus.Result {
	f.t.Helper()
	res := f.doAs(user(as), typ, target, a)
	if res.Common.Code >= 300 {
		f.t.Fatalf("%s as %d: %s %+v", typ, as, res.Common.Message, res.Problems)
	}
	f.settle()
	return res
}

// settle waits until the records projection has folded every event, so
// every record a command's batch wrote reads as written.
func (f *fixture) settle() {
	f.t.Helper()
	if err := f.p.Settle(context.Background(), features.LocalTenantID, data.Projector); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) wait(key string, seq uint64) {
	f.t.Helper()
	v, err := f.p.WaitFor(context.Background(), features.LocalTenantID, data.Projector, key, seq, 5*time.Second)
	if err != nil || v.Seq < seq {
		f.t.Fatalf("%s did not reach %d: %v", key, seq, err)
	}
}

// find reads an entity type's records with where, as someone, checked as
// the records.find query checks a reader (record.read on the type).
func (f *fixture) find(as features.ServiceArgs, entity string, where map[string]any) []map[string]any {
	f.t.Helper()
	if !authz.Check(as.Context(), as.Principal, "record.read", records.Resource(as.Principal, entity)).Allowed {
		return nil
	}
	w := map[string]json.RawMessage{}
	for k, v := range where {
		w[k], _ = json.Marshal(v)
	}
	res, err := data.Find(as, *f.data, data.Query{Entity: entity, Where: w, Limit: 20000})
	if err != nil {
		f.t.Fatal(err)
	}
	raw, _ := json.Marshal(res.Records)
	var out []map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func (f *fixture) get(entity, id string) map[string]any {
	f.t.Helper()
	rows := f.find(user(owner), entity, map[string]any{"_id": id})
	for _, r := range rows {
		if fmt.Sprint(r["_id"]) == id {
			return r
		}
	}
	v, _, _ := f.p.Get(context.Background(), features.LocalTenantID, data.Projector, data.Key(entity, id))
	var r map[string]any
	_ = json.Unmarshal(v.Value, &r)
	return r
}

func (f *fixture) appRole(role string) uint64 {
	f.t.Helper()
	var id uint64
	eventually(f.t, "the app role "+role, 5*time.Second, func() bool {
		all, _ := f.pol.Roles()
		for _, r := range all {
			if r.App == tasks.AppID && r.AppRole == role {
				id = r.ID
				return true
			}
		}
		return false
	})
	return id
}

// grantApp gives a person an app role in the tenant (Tasks user).
func (f *fixture) grantApp(who uint64, role string) {
	f.t.Helper()
	if _, err := f.pol.Grant(context.Background(), nil, authz.GrantArgs{UserID: who, RoleID: f.appRole(role), Resource: "tenant:1", By: owner}); err != nil {
		f.t.Fatal(err)
	}
	f.pol.Changed()
}

func has(who uint64, role, record string) bool {
	return authz.HasRole(context.Background(), user(who).Principal, role, record)
}

func eventually(t *testing.T, what string, within time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen in %s", what, within)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// newProject makes a project as a person and waits until the app has set
// it up and kept its people as grants.
func (f *fixture) newProject(as uint64, name string, members, guests []string) string {
	f.t.Helper()
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	f.okOn(as, "tasks.project.create", tasks.EntityProject, id, map[string]any{"name": name, "members": members, "guests": guests})
	eventually(f.t, "the project's statuses", 10*time.Second, func() bool {
		return len(f.find(user(owner), tasks.EntityStatus, map[string]any{"project": id})) == 4
	})
	eventually(f.t, "its maker's grant", 10*time.Second, func() bool { return has(as, "projectAdmin", id) })
	return id
}

func (f *fixture) statuses(project string) map[string]string {
	out := map[string]string{}
	for _, s := range f.find(user(owner), tasks.EntityStatus, map[string]any{"project": project}) {
		out[fmt.Sprint(s["name"])] = fmt.Sprint(s["_id"])
	}
	return out
}

func ids(rows []map[string]any) []string {
	out := []string{}
	for _, r := range rows {
		out = append(out, fmt.Sprint(r["_id"]))
	}
	slices.Sort(out)
	return out
}

func TestDocumentChecks(t *testing.T) {
	d, err := tasks.Document()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Parts.Entities) != 6 || len(d.Parts.Graphs) < 15 {
		t.Fatalf("%d entity types and %d graphs", len(d.Parts.Entities), len(d.Parts.Graphs))
	}
}

// The task model end to end: seeding installs the library app; projects
// get their statuses and keep their people as grants; Alice (a member of
// Website relaunch) and Gus (a guest of Billing) see only their projects'
// tasks; a move to Done sets completedAt and a move back clears it; and
// an agent with autonomy suggest makes a proposal, one with draft a task.
func TestTaskModel(t *testing.T) {
	f := setup(t)
	if _, ok := installs.Get(context.Background(), f.p, features.LocalTenantID, tasks.AppID); !ok {
		t.Fatal("the Tasks app is not installed")
	}
	for _, who := range []uint64{alice, gus, bob} {
		f.grantApp(who, "user")
	}

	web := f.newProject(owner, "Website relaunch", []string{strconv.FormatUint(alice, 10)}, nil)
	bill := f.newProject(bob, "Billing", nil, []string{strconv.FormatUint(gus, 10)})
	if p := f.get(tasks.EntityProject, web); p["key"] != "WEB" {
		t.Errorf("the key: %v", p["key"])
	}
	eventually(t, "Alice a member of Website relaunch, Gus a guest of Billing", 10*time.Second, func() bool {
		return has(alice, "member", web) && has(gus, "guest", bill)
	})
	if has(alice, "member", bill) || has(gus, "guest", web) {
		t.Error("a grant on the wrong project")
	}
	ws, bs := f.statuses(web), f.statuses(bill)
	if len(ws) != 4 || ws["To do"] == "" || ws["Done"] == "" {
		t.Fatalf("Website relaunch's statuses: %v", ws)
	}

	// Tasks, made by people with a role on the project
	mk := func(as uint64, project, title string, extra map[string]any) string {
		t.Helper()
		a := map[string]any{"project": project, "title": title}
		for k, v := range extra {
			a[k] = v
		}
		id := fmt.Sprintf("%d", time.Now().UnixNano())
		f.okOn(as, "tasks.task.create", tasks.EntityTask, id, a)
		return id
	}
	hero := mk(alice, web, "Hero image", map[string]any{"assignees": []string{"2"}, "due": "2026-11-27", "description": "Wide.\n\nDark."})
	copyT := mk(alice, web, "Copy", nil)
	invoice := mk(bob, bill, "Invoice run", nil)
	if res := f.doAs(user(alice), "tasks.task.create", fmt.Sprint(time.Now().UnixNano()), map[string]any{"project": bill, "title": "Sneak"}); res.Common.Code < 300 {
		t.Error("Alice made a task in a project she is not in")
	}
	h := f.get(tasks.EntityTask, hero)
	if h["status"] != ws["To do"] || h["state"] != "active" || h["rank"] == nil || h["number"] == nil {
		t.Errorf("the new task: %v", h)
	}
	if c := f.get(tasks.EntityTask, copyT); fmt.Sprint(c["rank"]) <= fmt.Sprint(h["rank"]) {
		t.Errorf("the second task is not after the first: %v, %v", c["rank"], h["rank"])
	}

	// Alice's day, read as the browser reads it (members are Editors of the
	// tenant, which an app's queries need)
	if _, err := f.pol.Grant(context.Background(), nil, authz.GrantArgs{UserID: alice, RoleID: authz.RoleEditor, Resource: "tenant:1", By: owner}); err != nil {
		t.Fatal(err)
	}
	f.pol.Changed()
	day := f.query(alice, "tasks.myDay", map[string]any{"today": "2026-11-28"})
	if due, _ := day["due"].([]any); len(due) != 1 || due[0].(map[string]any)["id"] != hero {
		t.Errorf("Alice's day: %v", day)
	}
	if mine := f.tool(user(alice), "tasks.list", map[string]any{"assignee": "me"}); len(mine["tasks"].([]any)) != 1 {
		t.Errorf("tasks.list for me: %v", mine)
	}

	// Each sees only their projects
	if got := ids(f.find(user(alice), tasks.EntityTask, nil)); !slices.Equal(got, ids([]map[string]any{{"_id": hero}, {"_id": copyT}})) {
		t.Errorf("Alice sees %v", got)
	}
	if got := ids(f.find(user(gus), tasks.EntityTask, nil)); !slices.Equal(got, []string{invoice}) {
		t.Errorf("Gus sees %v", got)
	}
	if got := ids(f.find(user(gus), tasks.EntityProject, nil)); !slices.Equal(got, []string{bill}) {
		t.Errorf("Gus sees projects %v", got)
	}
	if got := ids(f.find(user(alice), tasks.EntityStatus, nil)); len(got) != 4 {
		t.Errorf("Alice sees statuses %v", got)
	}

	// A move to Done sets completedAt; back to Doing clears it
	f.okOn(alice, "tasks.task.move", tasks.EntityTask, hero, map[string]any{"status": ws["Done"]})
	if h := f.get(tasks.EntityTask, hero); h["status"] != ws["Done"] || h["completedAt"] == nil {
		t.Errorf("moved to Done: %v", h)
	}
	f.okOn(alice, "tasks.task.move", tasks.EntityTask, hero, map[string]any{"status": ws["Doing"]})
	if h := f.get(tasks.EntityTask, hero); h["status"] != ws["Doing"] || h["completedAt"] != nil {
		t.Errorf("moved back to Doing: %v", h)
	}
	// Between two tasks of the column
	f.okOn(alice, "tasks.task.move", tasks.EntityTask, copyT, map[string]any{"status": ws["Doing"], "before": hero})
	if c, h := f.get(tasks.EntityTask, copyT), f.get(tasks.EntityTask, hero); fmt.Sprint(c["rank"]) >= fmt.Sprint(h["rank"]) {
		t.Errorf("Copy is not above Hero image: %v, %v", c["rank"], h["rank"])
	}
	if res := f.doAs(user(alice), "tasks.task.move", hero, map[string]any{"status": bs["Done"]}); res.Common.Code < 300 {
		t.Error("a task moved to another project's status")
	}
	// Complete, and the guest changes a status
	f.okOn(alice, "tasks.task.complete", tasks.EntityTask, copyT, map[string]any{})
	if c := f.get(tasks.EntityTask, copyT); c["status"] != ws["Done"] || c["completedAt"] == nil {
		t.Errorf("completed: %v", c)
	}
	f.okOn(gus, "tasks.task.move", tasks.EntityTask, invoice, map[string]any{"status": bs["Review"]})
	f.okOn(alice, "tasks.task.update", tasks.EntityTask, hero, map[string]any{"title": "Hero image, dark", "priority": "high"})
	if h := f.get(tasks.EntityTask, hero); h["title"] != "Hero image, dark" || h["priority"] != "high" || h["status"] != ws["Doing"] {
		t.Errorf("updated: %v", h)
	}
	f.okOn(alice, "tasks.task.update", tasks.EntityTask, hero, map[string]any{"status": ws["Done"]})
	if h := f.get(tasks.EntityTask, hero); h["status"] != ws["Done"] || h["completedAt"] == nil {
		t.Errorf("updated to Done: %v", h)
	}

	// Removing Alice from the project takes her grant away
	f.okOn(owner, "record.update", tasks.EntityProject, web, map[string]any{"entity": tasks.EntityProject, "set": map[string]any{"members": []string{}}})
	eventually(t, "Alice's grant revoked", 10*time.Second, func() bool { return !has(alice, "member", web) })
	if got := f.find(user(alice), tasks.EntityTask, nil); len(got) != 0 {
		t.Errorf("Alice still sees %v", ids(got))
	}
	f.okOn(owner, "record.update", tasks.EntityProject, web, map[string]any{"entity": tasks.EntityProject, "set": map[string]any{"members": []string{"2"}}})
	eventually(t, "Alice's grant back", 10*time.Second, func() bool { return has(alice, "member", web) })

	// Queries
	board := f.query(owner, "tasks.projectTasks", map[string]any{"project": web})
	if sts, _ := board["statuses"].([]any); len(sts) != 4 {
		t.Errorf("the board's statuses: %v", board["statuses"])
	}
	if ts, _ := board["tasks"].([]any); len(ts) != 2 {
		t.Errorf("the board's tasks: %v", board["tasks"])
	}

	// Agents: suggest proposes, draft makes a task, each through the app's
	// tool as itself, held to a Member grant on the project
	triage, planner := f.agent("Triage", "suggest"), f.agent("Planner", "draft")
	for _, a := range []uint64{triage, planner} {
		f.grantAgent(a, "member", tasks.EntityProject, web)
	}
	out := f.tool(f.asAgent(triage, ""), "tasks.create", map[string]any{"project": web, "title": "Reply to Dana", "source": map[string]any{"link": "workbench:mailThread/7", "title": "Dana: invoice", "kind": "mail"}})
	proposal, _ := out["task"].(string)
	eventually(t, "the proposal", 5*time.Second, func() bool { return f.get(tasks.EntityTask, proposal)["_id"] != nil })
	if p := f.get(tasks.EntityTask, proposal); p["state"] != "proposed" || p["proposedBy"] != fmt.Sprintf("agent:%d", triage) {
		t.Errorf("the suggest agent's task: %v", p)
	}
	// Forge 30f: an agent proposes one task per source
	again := f.tool(f.asAgent(triage, ""), "tasks.create", map[string]any{"project": web, "title": "Reply to Dana again", "source": map[string]any{"link": "workbench:mailThread/7"}})
	if again["made"] == true || !strings.Contains(fmt.Sprint(again["error"]), "already proposed") {
		t.Errorf("a second proposal from one source: %v", again)
	}
	out = f.tool(f.asAgent(planner, ""), "tasks.create", map[string]any{"project": web, "title": "Plan the launch"})
	drafted, _ := out["task"].(string)
	eventually(t, "the drafted task", 5*time.Second, func() bool { return f.get(tasks.EntityTask, drafted)["_id"] != nil })
	if p := f.get(tasks.EntityTask, drafted); p["state"] != "active" {
		t.Errorf("the draft agent's task: %v", p)
	}
	if day := f.query(owner, "tasks.projectTasks", map[string]any{"project": web}); len(day["proposals"].([]any)) != 1 {
		t.Errorf("the board's proposals: %v", day["proposals"])
	}
	f.okOn(alice, "tasks.task.accept", tasks.EntityTask, proposal, map[string]any{})
	if p := f.get(tasks.EntityTask, proposal); p["state"] != "active" {
		t.Errorf("accepted: %v", p)
	}
	// A run confined to one task changes only that one
	confined := f.asAgent(planner, "taskItem:"+drafted)
	if out := f.tool(confined, "tasks.update", map[string]any{"task": proposal, "priority": "low"}); out["changed"] == true {
		t.Error("a confined run changed another task")
	}
	if out := f.tool(confined, "tasks.update", map[string]any{"task": drafted, "priority": "low"}); out["changed"] != true {
		t.Errorf("a confined run could not change its own task: %v", out)
	}
	if out := f.tool(confined, "tasks.comment", map[string]any{"task": drafted, "body": "On it."}); out["made"] != true {
		t.Errorf("the comment: %v", out)
	}
	eventually(t, "the comment", 5*time.Second, func() bool {
		return len(f.find(user(alice), tasks.EntityComment, map[string]any{"task": drafted})) == 1
	})
	listed := f.tool(user(alice), "tasks.list", map[string]any{"project": web, "state": "all", "includeDone": true})
	if ts, _ := listed["tasks"].([]any); len(ts) != 4 {
		t.Errorf("tasks.list: %v", listed)
	}
}

// agent makes an agent record of the bot type with an autonomy and gives
// its id.
func (f *fixture) agent(name, autonomy string) uint64 {
	f.t.Helper()
	f.botOnce.Do(func() {
		res := f.doAs(user(owner), "entity.create", "bot", map[string]any{"name": "Bot", "titleField": "name", "fields": []map[string]any{
			{"name": "name", "type": "text"}, {"name": "autonomy", "type": "text"}, {"name": "principal", "type": "principal", "kind": "agent"}}})
		if res.Common.Code >= 300 {
			f.t.Fatalf("the agent type: %s %+v", res.Common.Message, res.Problems)
		}
		res = f.doAs(user(owner), "entity.publish", "bot", map[string]any{"rev": 1})
		if _, err := f.p.WaitFor(context.Background(), features.LocalTenantID, entities.PublishedProjector, "entity.bot", res.Seq, 5*time.Second); err != nil {
			f.t.Fatal(err)
		}
	})
	res := f.doAs(user(owner), "record.create", "", map[string]any{"entity": "bot", "values": map[string]any{"name": name, "autonomy": autonomy}})
	if res.Common.Code >= 300 {
		f.t.Fatalf("the agent: %s %+v", res.Common.Message, res.Problems)
	}
	f.wait(data.Key("bot", res.Target), res.Seq)
	id, _ := strconv.ParseUint(res.Target, 10, 64)
	return id
}

// asAgent is a run as an agent its maker (the owner) starts, confined to
// reach when given.
func (f *fixture) asAgent(agent uint64, reach string) features.ServiceArgs {
	f.t.Helper()
	p, err := authz.AsAgent(context.Background(), user(owner).Principal, agent)
	if err != nil {
		f.t.Fatal(err)
	}
	p.Reach = reach
	return features.ServiceArgs{Ctx: context.Background(), Principal: p}
}

// grantAgent gives an agent an app role on one record.
func (f *fixture) grantAgent(agent uint64, role, entity, record string) {
	f.t.Helper()
	resource := authz.RecordResource(records.Resource(user(owner).Principal, entity), record)
	if _, err := f.pol.Grant(context.Background(), nil, authz.GrantArgs{AgentID: agent, RoleID: f.appRole(role), Resource: resource, By: owner}); err != nil {
		f.t.Fatal(err)
	}
	f.pol.Changed()
}

// tool calls one of the installed app's tools as someone and gives its
// content.
func (f *fixture) tool(as features.ServiceArgs, name string, args map[string]any) map[string]any {
	f.t.Helper()
	for _, tl := range tools.Available(f.reg, as) {
		if tl.ID != name {
			continue
		}
		raw, _ := json.Marshal(args)
		res, err := tl.Run(tools.Call{ServiceArgs: as, ID: fmt.Sprintf("call-%d", f.n.Add(1)), Name: name, Args: raw})
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		return res.Content
	}
	f.t.Fatalf("no tool %s", name)
	return nil
}

// query reads an app query once as a person.
func (f *fixture) query(as uint64, name string, params map[string]any) map[string]any {
	f.t.Helper()
	raw, _ := json.Marshal(params)
	v, _, res := f.subs.Once(user(as), name, raw)
	if res != nil && res.Code >= 300 {
		f.t.Fatalf("%s: %s", name, res.Message)
	}
	b, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

// Ten thousand tasks (TASKS_N), kept in a large table, filter by assignee in SQL
// within the budget Forge's large tables keep (half a second).
func TestTenThousandTasks(t *testing.T) {
	if testing.Short() {
		t.Skip("makes 10 000 tasks")
	}
	f := setup(t)
	web := f.newProject(owner, "Website relaunch", nil, nil)
	todo := f.statuses(web)["To do"]
	// 10 000 take about five minutes to make (each create takes the task
	// number's counter), so the default run makes 1 000: TASKS_N=10000 for
	// the part's acceptance (20 ms by assignee, 2026-11-27)
	n := 1000
	if v, err := strconv.Atoi(os.Getenv("TASKS_N")); err == nil && v > 0 {
		n = v
	}
	ranks := records.EvenRanks(n)
	var wg sync.WaitGroup
	var failed atomic.Int64
	work := make(chan int)
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				res := f.doAs(user(owner), "record.create", "", map[string]any{"entity": tasks.EntityTask, "values": map[string]any{
					"title": fmt.Sprintf("Task %d", i), "project": web, "status": todo, "rank": ranks[i], "assignees": []string{strconv.Itoa(10 + i%20)}}})
				if res.Common.Code >= 300 {
					failed.Add(1)
				}
			}
		}()
	}
	for i := range n {
		work <- i
	}
	close(work)
	wg.Wait()
	if failed.Load() > 0 {
		t.Fatalf("%d creates failed", failed.Load())
	}
	f.settle()
	q := data.Query{Entity: tasks.EntityTask, Filter: &data.Filter{Field: "assignees", Op: "contains", Value: json.RawMessage(`"13"`)}, Sort: []data.Sort{{Field: "rank"}}, Limit: 100}
	var res data.Result
	eventually(t, "the large table", 60*time.Second, func() bool {
		var err error
		res, err = data.Find(user(owner), *f.data, q)
		if err != nil || len(res.Records) != min(100, n/20) {
			t.Logf("%d %v %+v", len(res.Records), err, res.Plan)
		}
		return err == nil && len(res.Records) == min(100, n/20)
	})
	start := time.Now()
	res, err := data.Find(user(owner), *f.data, q)
	took := time.Since(start)
	if err != nil || len(res.Records) != min(100, n/20) {
		t.Fatalf("%d records: %v", len(res.Records), err)
	}
	if res.Plan.Scan {
		t.Errorf("the filter scanned: %+v", res.Plan)
	}
	if took > 500*time.Millisecond {
		t.Errorf("filtering 10 000 tasks by assignee took %s", took)
	}
	t.Logf("10 000 tasks, by assignee: %s, plan %+v", took, res.Plan)
}
