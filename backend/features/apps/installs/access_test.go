package installs_test

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
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/authz/services/authzservice"
	authzimpl "github.com/SgtFlame/foundation/backend/features/authz/services/authzservice/impl"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/config/services/configservice"
	configimpl "github.com/SgtFlame/foundation/backend/features/config/services/configservice/impl"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/services/recordservice"
	recordimpl "github.com/SgtFlame/foundation/backend/features/records/services/recordservice/impl"
	recordstools "github.com/SgtFlame/foundation/backend/features/records/tools"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/SgtFlame/foundation/backend/features/ui"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
)

// Users of the access test, all members of tenant 1: the owner installs
// the app; Alice and Bob are given Member, Mia Manager, and Eve nothing
// of the app's, only the Editor every member has.
const (
	owner uint64 = 1
	alice uint64 = 2
	bob   uint64 = 3
	eve   uint64 = 4
	mia   uint64 = 5
)

func as(user uint64) features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, UserID: user, TenantID: 1}}
}

func (f *fixture) doAs(user uint64, typ, target string, a any) bus.Result {
	raw, _ := json.Marshal(a)
	return f.bus.Execute(bus.Request{ServiceArgs: as(user), ID: fmt.Sprintf("c%d", f.n.Add(1)), Type: typ, Target: target, Args: raw})
}

// ids are the records a user finds in a records.find snapshot, or the
// failure's code.
func (f *fixture) ids(user uint64) ([]string, int) {
	f.t.Helper()
	raw, _ := json.Marshal(map[string]any{"entity": "task"})
	v, _, fail := f.subs.Once(as(user), "records.find", raw)
	if fail != nil {
		return nil, int(fail.Code)
	}
	b, _ := json.Marshal(v)
	var page struct{ Records []map[string]any }
	_ = json.Unmarshal(b, &page)
	var out []string
	for _, r := range page.Records {
		out = append(out, fmt.Sprint(r["_id"]))
	}
	slices.Sort(out)
	return out, 0
}

// eventually waits for what a follower or a watch does after a command.
func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// changes collects a subscription's list changes.
type changes struct {
	mu   sync.Mutex
	ops  []string
	fail string
}

func (c *changes) Snapshot(any, live.Position) {}
func (c *changes) Change(d live.Delta, _ live.Position) {
	if d.List == "" {
		return
	}
	var r map[string]any
	_ = json.Unmarshal(d.Value, &r)
	c.mu.Lock()
	c.ops = append(c.ops, d.Op+" "+fmt.Sprint(r["_id"]))
	c.mu.Unlock()
}
func (c *changes) Failed(resp features.ServiceResponse) {
	c.mu.Lock()
	c.fail = resp.Message
	c.mu.Unlock()
}
func (c *changes) seen() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.ops)
}

// access turns access control on over the fixture, as serve.go wires it:
// the resource types, a policy with the owner as Owner and every other
// user an Editor, the apps' roles, sections and record placement.
func (f *fixture) access() (*authz.Policy, configservice.Service) {
	t := f.t
	appDB, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "app.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(appDB, authz.Models()...); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(f.db, config.Models()...); err != nil {
		t.Fatal(err)
	}
	authz.Contribute(f.reg)
	records.ContributeAccess(f.reg)
	apps.ContributeAccess(f.reg)
	config.ContributeAccess(f.reg)
	cognition.ContributeAccess(f.reg)
	ui.ContributeAccess(f.reg)
	ui.ContributePrefabAccess(f.reg)
	pol := authz.NewPolicy(appDB, f.reg)
	authz.Install(pol)
	t.Cleanup(func() { authz.Install(nil) })
	grant := func(user, role uint64) {
		if _, err := pol.Grant(context.Background(), nil, authz.GrantArgs{UserID: user, RoleID: role, Resource: "tenant:1", By: owner}); err != nil {
			t.Fatal(err)
		}
	}
	grant(owner, authz.RoleOwner)
	for _, u := range []uint64{alice, bob, eve, mia} {
		grant(u, authz.RoleEditor)
	}
	cfg := configimpl.New(configimpl.WithDB(f.db), configimpl.WithRegistry(f.reg), configimpl.WithChanges(config.NewChanges()))
	f.install.Roles, f.install.Config = pol, cfg
	records.SetPlacement(installs.Placement(func() installs.Deps { return *f.install }))
	t.Cleanup(func() { records.SetPlacement(nil) })
	return pol, cfg
}

// appRoles are the tenant's roles made by the app, by their manifest id.
func appRoles(t *testing.T, pol *authz.Policy, app string) map[string]authz.Role {
	t.Helper()
	all, err := pol.Roles()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]authz.Role{}
	for _, r := range all {
		if r.App == app {
			out[r.AppRole] = r
		}
	}
	return out
}

func TestAppRolesAccessAndSettings(t *testing.T) {
	f := setup(t)
	f.tasks(true)
	// A page bound to a setting
	f.ok("prefab.create", "dueHint", map[string]any{"name": "Due hint", "owner": "app:tasks"})
	res := f.ok("prefab.insertNode", "dueHint", map[string]any{"node": map[string]any{"type": "mui.text", "key": "due",
		"bind": map[string]any{"text": `="Due in " + string(app.settings.defaultDueDays) + " days"`}}})
	f.wait(prefabs.DraftProjector, "prefab.dueHint", res.Seq)
	res = f.ok("prefab.publish", "dueHint", map[string]any{"rev": 2})
	f.wait(prefabs.PublishedProjector, "prefab.dueHint", res.Seq)
	f.ok("app.addPage", "tasks", map[string]any{"route": "/due", "prefab": "dueHint"})
	f.ok("app.addRole", "tasks", map[string]any{"id": "manager", "title": "Manager", "actions": []string{"entity:task.*"}})
	f.ok("app.addRole", "tasks", map[string]any{"id": "member", "title": "Member", "actions": []string{"entity:task.read", "entity:task.update"},
		"rowRules": map[string]any{"task": "record.assignee == principal.user"}})
	// Actions reach only the app's own entity types, one at a time for a command
	f.refused("app.addRole", "tasks", map[string]any{"id": "x", "actions": []string{"entity:task.purge"}}, "manifest.roles[x].actions[0]", "is not an action")
	f.refused("app.updateCommand", "tasks", map[string]any{"type": "task.reassign", "action": "entity:task.*"}, "manifest.commands[task.reassign].action", "needs one action")
	seq := f.ok("app.setSettingsSchema", "tasks", map[string]any{
		"schema":   map[string]any{"type": "object", "properties": map[string]any{"defaultDueDays": map[string]any{"type": "integer", "title": "Default due in N days"}}},
		"defaults": map[string]any{"defaultDueDays": 7}}).Seq
	f.publish(seq)

	pol, cfg := f.access()
	if r := records.Resource(as(alice).Principal, "task"); r != "tenant:1/records/entity:task" {
		t.Fatalf("before install the records are the tenant's: %s", r)
	}
	f.lifecycle("app.install", "tasks", map[string]any{})

	// The roles appear as custom roles, the installer holding the first
	eventually(t, "the app's roles", func() bool { return len(appRoles(t, pol, "tasks")) == 2 })
	roles := appRoles(t, pol, "tasks")
	if got := roles["member"].Permissions; !slices.Equal(got, []string{"app:tasks/entity:task/record.read", "app:tasks/entity:task/record.update"}) {
		t.Errorf("member's permissions: %v", got)
	}
	if roles["member"].Rules["task"] != "record.assignee == principal.user" || roles["manager"].Name != "Manager" {
		t.Errorf("roles: %+v", roles)
	}
	eventually(t, "the installer's grant", func() bool {
		gs, _ := pol.GrantsOf(owner)
		return slices.ContainsFunc(gs, func(g authz.RoleGrant) bool { return g.RoleID == roles["manager"].ID })
	})
	// In the Role Editor's list, read-only
	az := authzimpl.New(authzimpl.WithPolicy(pol))
	list := az.Roles(authzservice.RolesArgs{ServiceArgs: as(owner)})
	if !slices.ContainsFunc(list.Roles, func(r authz.Role) bool { return r.ID == roles["member"].ID && r.App == "tasks" }) {
		t.Errorf("the Role Editor does not list the app's roles: %+v", list.Roles)
	}
	saved := az.SaveRole(authzservice.SaveRoleArgs{ServiceArgs: as(owner), RoleID: roles["member"].ID, Name: "Mine", Revision: roles["member"].Revision})
	deleted := az.DeleteRole(authzservice.DeleteRoleArgs{ServiceArgs: as(owner), RoleID: roles["member"].ID})
	if saved.Common.ErrorCode != "app_role" || deleted.Common.ErrorCode != "app_role" {
		t.Errorf("an app's role was changed: %+v %+v", saved.Common, deleted.Common)
	}
	// Grants of them are any role's: the owner may give Member, Eve may not
	if d := pol.MayGrant(context.Background(), as(owner).Principal, roles["member"], "tenant:1"); !d.Allowed {
		t.Errorf("the owner may not grant Member: %s", d.Reason)
	}
	if d := pol.MayGrant(context.Background(), as(eve).Principal, roles["member"], "tenant:1"); d.Allowed {
		t.Error("an Editor may grant Member")
	}
	for user, role := range map[uint64]string{alice: "member", bob: "member", mia: "manager"} {
		if _, err := pol.Grant(context.Background(), nil, authz.GrantArgs{UserID: user, RoleID: roles[role].ID, Resource: "tenant:1", By: owner}); err != nil {
			t.Fatal(err)
		}
	}

	// The records moved under the app's path
	eventually(t, "the records under the app", func() bool {
		return records.Resource(as(alice).Principal, "task") == "tenant:1/app:tasks/entity:task"
	})
	mine := f.doAs(owner, "record.create", "", map[string]any{"entity": "task", "values": map[string]any{"title": "Alice's", "assignee": "2"}})
	theirs := f.doAs(owner, "record.create", "", map[string]any{"entity": "task", "values": map[string]any{"title": "Bob's", "assignee": "3"}})
	if mine.Common.Code >= 300 || theirs.Common.Code >= 300 {
		t.Fatalf("the owner cannot create tasks: %s %s", mine.Common.Message, theirs.Common.Message)
	}
	f.wait(data.Projector, "task."+theirs.Target, theirs.Seq)
	both := []string{mine.Target, theirs.Target}
	slices.Sort(both)

	// A query: a Member sees their own, a Manager and the Owner every task,
	// and an Editor of the tenant none
	if got, _ := f.ids(alice); !slices.Equal(got, []string{mine.Target}) {
		t.Errorf("Alice sees %v", got)
	}
	if got, _ := f.ids(bob); !slices.Equal(got, []string{theirs.Target}) {
		t.Errorf("Bob sees %v", got)
	}
	for _, u := range []uint64{mia, owner} {
		if got, _ := f.ids(u); !slices.Equal(got, both) {
			t.Errorf("user %d sees %v", u, got)
		}
	}
	if _, code := f.ids(eve); code != authz.Status {
		t.Errorf("Eve, an Editor, reads the app's tasks: %d", code)
	}

	// A command: Bob's task is not there for Alice, and her own cannot be
	// given away (the rule holds after the change too)
	if res := f.doAs(alice, "record.update", theirs.Target, map[string]any{"entity": "task", "set": map[string]any{"title": "Mine now"}}); res.Common.Code != 422 ||
		!strings.Contains(res.Problems[0].Message, "there is no task record") {
		t.Errorf("Alice changed Bob's task: %d %+v", res.Common.Code, res.Problems)
	}
	if res := f.doAs(alice, "tasks.task.reassign", theirs.Target, map[string]any{"assignee": "2"}); res.Common.Code != 422 {
		t.Errorf("Alice took Bob's task through the app's command: %d %+v", res.Common.Code, res.Problems)
	}
	if res := f.doAs(alice, "tasks.task.reassign", mine.Target, map[string]any{"assignee": "3"}); res.Common.Code != 422 || res.Problems[0].Code != "row_rule" {
		t.Errorf("Alice gave her task away: %d %+v", res.Common.Code, res.Problems)
	}
	if res := f.doAs(alice, "record.update", mine.Target, map[string]any{"entity": "task", "set": map[string]any{"title": "Alice's, renamed"}}); res.Common.Code >= 300 {
		t.Errorf("Alice cannot change her own task: %s %+v", res.Common.Message, res.Problems)
	}
	// The app's command checks the manifest's action, on the app's path
	if res := f.doAs(eve, "tasks.task.reassign", mine.Target, map[string]any{"assignee": "4"}); res.Common.Code != authz.Status {
		t.Errorf("Eve sent the app's command: %d %s", res.Common.Code, res.Common.Message)
	}

	// A subscription's changes: Alice's list gains Bob's task when it is
	// given to her and loses it when it goes back
	sub := &changes{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sa := as(alice)
	sa.Ctx = ctx
	if fail := f.subs.Open(subscribe.OpenArgs{ServiceArgs: sa, Query: "records.find", Params: json.RawMessage(`{"entity":"task"}`), Sink: sub}); fail != nil {
		t.Fatal(fail.Message)
	}
	time.Sleep(100 * time.Millisecond)
	f.doAs(mia, "record.update", theirs.Target, map[string]any{"entity": "task", "set": map[string]any{"title": "Bob's, renamed"}})
	f.doAs(mia, "tasks.task.reassign", theirs.Target, map[string]any{"assignee": "2"})
	eventually(t, "Bob's task reaching Alice", func() bool { return slices.Contains(sub.seen(), "insert "+theirs.Target) })
	f.doAs(mia, "tasks.task.reassign", theirs.Target, map[string]any{"assignee": "3"})
	eventually(t, "Bob's task leaving Alice", func() bool { return slices.Contains(sub.seen(), "remove ") || len(sub.seen()) >= 2 })
	if got := sub.seen(); len(got) != 2 || !strings.HasPrefix(got[1], "remove") {
		t.Errorf("Alice's list changed by %v", got)
	}

	// A chat tool reads through the same rules
	treg := extensions.NewRegistry()
	recordstools.ContributeRecords(treg, func() recordservice.Service { return recordimpl.New(recordimpl.WithRecords(f.bus, f.subs, *f.data)) })
	query := tools.Tools.All(treg)[slices.IndexFunc(tools.Tools.All(treg), func(tl tools.Tool) bool { return tl.ID == "records.query" })]
	out, err := query.Run(tools.Call{ServiceArgs: as(alice), Name: "records.query", Args: json.RawMessage(`{"entity":"task"}`)})
	if raw, _ := json.Marshal(out.Content); err != nil || strings.Contains(string(raw), "Bob's") || !strings.Contains(string(raw), "Alice's") {
		t.Errorf("the records.query tool gave Alice %s (%v)", raw, err)
	}

	// Settings: a section, its default, and a tenant override reaching a
	// page bound to it, the app.settings query and the App setting node
	sections := cfg.Sections(configservice.SectionsArgs{ServiceArgs: as(alice)})
	if !slices.ContainsFunc(sections.Sections, func(s config.Section) bool {
		return s.ID == "app.tasks" && s.Title == "Tasks" && slices.Equal(s.Category, []string{"Apps", "Tasks"})
	}) {
		t.Fatalf("no app.tasks section in %d sections", len(sections.Sections))
	}
	page := ui.PrefabPage("dueHint", "", nil)
	page.ID, page.App = "tasks/due", "tasks"
	frames := make(chan any, 64)
	sctx, scancel := context.WithCancel(context.Background())
	defer scancel()
	sa = as(alice)
	sa.Ctx = sctx
	s, fail := ui.Open(sa, "s1", page, nil, nil, ui.Options{Catalog: ui.Catalog(f.reg), Subs: f.subs, Send: func(fr any) { frames <- fr }})
	if fail != nil {
		t.Fatal(fail.Message)
	}
	defer s.Close(true)
	shows := func(want string) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case fr := <-frames:
				raw, _ := json.Marshal(fr)
				if strings.Contains(string(raw), want) {
					return
				}
			case <-deadline:
				t.Fatalf("the page never showed %s", want)
			}
		}
	}
	shows("Due in 7 days")
	set := cfg.Set(configservice.SetArgs{ServiceArgs: as(owner), Section: "app.tasks", Scope: configservice.ScopeTenant, Patch: json.RawMessage(`{"defaultDueDays":3}`)})
	if set.Common.Code >= 300 {
		t.Fatalf("the tenant override: %s", set.Common.Message)
	}
	shows("Due in 3 days")
	raw, _ := json.Marshal(map[string]any{"app": "tasks"})
	v, _, qfail := f.subs.Once(as(bob), installs.SettingsQuery, raw)
	if b, _ := json.Marshal(v); qfail != nil || string(b) != `{"defaultDueDays":3}` {
		t.Errorf("app.settings for Bob: %s", b)
	}
	node := cognition.Nodes.All(f.reg)[slices.IndexFunc(cognition.Nodes.All(f.reg), func(n cognition.NodeType) bool { return n.ID == "apps.setting" })]
	vals, _, err := node.Run(&cognition.Exec{ServiceArgs: as(bob), Node: cognition.Node{Config: json.RawMessage(`{"app":"tasks","key":"defaultDueDays"}`)}}, nil)
	if err != nil || fmt.Sprint(vals["value"]) != "3" {
		t.Errorf("App setting gave %v (%v)", vals, err)
	}

	// An upgrade changes the roles: Member no longer updates
	seq = f.ok("app.updateRole", "tasks", map[string]any{"id": "member", "actions": []string{"entity:task.read"}}).Seq
	f.publish(seq)
	f.lifecycle("app.upgrade", "tasks", map[string]any{})
	eventually(t, "the upgraded roles", func() bool {
		return slices.Equal(appRoles(t, pol, "tasks")["member"].Permissions, []string{"app:tasks/entity:task/record.read"})
	})
	if res := f.doAs(alice, "record.update", mine.Target, map[string]any{"entity": "task", "set": map[string]any{"title": "Again"}}); res.Common.Code != authz.Status {
		t.Errorf("Alice still updates after the upgrade: %d", res.Common.Code)
	}

	// Uninstall removes the roles and their grants, and the section; the
	// records stay, at the tenant's path again
	f.lifecycle("app.uninstall", "tasks", map[string]any{})
	eventually(t, "the roles removed", func() bool { return len(appRoles(t, pol, "tasks")) == 0 })
	for _, u := range []uint64{owner, alice, mia} {
		gs, _ := pol.GrantsOf(u)
		if slices.ContainsFunc(gs, func(g authz.RoleGrant) bool { return g.RoleID == roles["member"].ID || g.RoleID == roles["manager"].ID }) {
			t.Errorf("user %d keeps a grant of a removed role", u)
		}
	}
	eventually(t, "the section removed", func() bool {
		all := cfg.Sections(configservice.SectionsArgs{ServiceArgs: as(alice)}).Sections
		return !slices.ContainsFunc(all, func(s config.Section) bool { return s.ID == "app.tasks" })
	})
	eventually(t, "the records back at the tenant's path", func() bool {
		return records.Resource(as(alice).Principal, "task") == "tenant:1/records/entity:task"
	})
	if got, _ := f.ids(eve); !slices.Equal(got, both) {
		t.Errorf("after uninstall an Editor sees %v", got)
	}
}
