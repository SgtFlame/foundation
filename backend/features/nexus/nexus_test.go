package nexus_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/branches"
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
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/identity"
	"github.com/SgtFlame/foundation/backend/features/jobs"
	jobsnodes "github.com/SgtFlame/foundation/backend/features/jobs/nodes"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/nexus"
	"github.com/SgtFlame/foundation/backend/features/notifications"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/records/data"
	"github.com/SgtFlame/foundation/backend/features/records/entities"
	recordsnodes "github.com/SgtFlame/foundation/backend/features/records/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui"
	uicomponents "github.com/SgtFlame/foundation/backend/features/ui/components"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
	"github.com/SgtFlame/foundation/backend/features/ui/prefabs"
)

type settings struct{ s jobs.Settings }

func (s settings) Resolved(features.ServiceArgs, string) (json.RawMessage, error) {
	return json.Marshal(s.s)
}

type stack struct {
	t    *testing.T
	reg  *extensions.Registry
	bus  *bus.Bus
	subs *subscribe.Subscriptions
	p    *project.Projections
	data data.Deps
	n    atomic.Int64
}

const (
	ana = 7
	bo  = 8
	cy  = 9 // in no channel
)

func user(id uint64) features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.Principal{Kind: features.PrincipalUser, UserID: id, TenantID: features.LocalTenantID}}
}

// newStack is the live core with records, apps, jobs, access control and
// notifications as serve wires them, the Nexus app seeded into tenant 1 and
// its users given the Nexus user role.
func newStack(t *testing.T) *stack {
	reg := extensions.NewRegistry()
	cognitionnodes.Contribute(reg)
	uinodes.Contribute(reg)
	authznodes.Contribute(reg)
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
		branchDeps branches.Deps
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
	branches.Contribute(reg, func() branches.Deps { return branchDeps })
	jobs.Contribute(reg, func() jobs.Deps { return jobDeps })
	jobsnodes.Contribute(reg, func() jobsnodes.Deps { return jobNodes })
	notifications.ContributeAccess(reg)
	notifications.Contribute(reg, func() notifications.Deps { return dataDeps })
	authz.Contribute(reg)
	identity.ContributeAccess(reg)
	records.ContributeAccess(reg)
	cognition.ContributeAccess(reg)
	apps.ContributeAccess(reg)
	jobs.ContributeAccess(reg)

	dir := t.TempDir()
	gdb, err := db.Open(db.WithPath(filepath.Join(dir, "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, slices.Concat(cognition.Models(), records.Models(), ui.Models(), apps.Models())...); err != nil {
		t.Fatal(err)
	}
	appDB, err := db.Open(db.WithPath(filepath.Join(dir, "app.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(appDB, authz.Models()...); err != nil {
		t.Fatal(err)
	}
	pol := authz.NewPolicy(appDB, reg)
	if _, err := pol.Grant(context.Background(), nil, authz.GrantArgs{UserID: features.LocalUserID, RoleID: authz.RoleOwner, Resource: "tenant:1"}); err != nil {
		t.Fatal(err)
	}
	authz.Install(pol)
	t.Cleanup(func() { authz.Install(nil) })

	srv := natsd.StartTest(t)
	p := project.New(srv.JS(), reg)
	t.Cleanup(p.Close)
	eng := engine.New(traces.In(gdb), reg, nil)
	eng.SetSource(drafts.Source{Store: p})
	b := bus.New(p)
	subs := subscribe.New(p, reg)
	nodeDeps = livenodes.Deps{Engine: eng, Bus: b, Subscriptions: subs, Store: p}
	draftDeps = drafts.Deps{Engine: eng, Projections: p}
	dataDeps = data.Deps{Projections: p, Bus: b}
	entityDeps = entities.Deps{Projections: p, DB: gdb, OnPublish: func(args features.ServiceArgs) error { return data.Follow(args, dataDeps) }}
	prefabDeps = prefabs.Deps{Projections: p, DB: gdb}
	appDeps = manifests.Deps{Projections: p, DB: gdb}
	installDep = installs.Deps{Projections: p, DB: gdb, Nodes: nodeDeps, Roles: pol}
	branchDeps = branches.Deps{Projections: p}
	b.Resolve = installs.CommandFor(func() installs.Deps { return installDep }, livenodes.CommandFor(func() livenodes.Deps { return nodeDeps }))
	subs.Resolve = installs.QueryFor(func() installs.Deps { return installDep }, livenodes.QueryFor(func() livenodes.Deps { return nodeDeps }))
	p.SetResolver(installs.ProjectorResolver(func() installs.Deps { return installDep }))
	records.SetPlacement(installs.Placement(func() installs.Deps { return installDep }))
	t.Cleanup(func() { records.SetPlacement(nil) })
	jobDeps = jobs.Deps{Projections: p, Bus: b, Engine: eng, Config: settings{jobs.Settings{MaxConcurrent: 4, MaxAttempts: 3, BackoffSeconds: 1, MaxBackoffSeconds: 1, DoneHours: 24}}}
	nodesRunner := jobs.New(jobDeps, jobs.Options{Name: "nodes"})
	t.Cleanup(nodesRunner.Close)
	jobNodes = jobsnodes.Deps{Runner: nodesRunner, Store: p}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc := appimpl.New(appimpl.WithDeps(func() appimpl.Deps {
		return appimpl.Deps{Projections: p, DB: gdb, Bus: b, Subscriptions: subs, Registry: reg}
	}))
	tenant := features.ServiceArgs{Ctx: ctx, Principal: features.ServicePrincipal(features.LocalTenantID)}
	if err := nexus.Seed(tenant, p, svc, b); err != nil {
		t.Fatal(err)
	}
	if err := nexus.Seed(tenant, p, svc, b); err != nil {
		t.Fatalf("a second seed: %v", err)
	}
	// The app's roles are synced by a follower; then everyone is a Nexus user
	var userRole uint64
	until(t, "the app's roles", func() bool {
		roles, _ := pol.Roles()
		for id, r := range roles {
			if r.App == nexus.AppID && r.AppRole == "user" {
				userRole = id
			}
		}
		return userRole != 0
	})
	for _, u := range []uint64{ana, bo, cy} {
		if _, err := pol.Grant(context.Background(), nil, authz.GrantArgs{UserID: u, RoleID: userRole, Resource: "tenant:1"}); err != nil {
			t.Fatal(err)
		}
	}
	runner := jobs.New(jobDeps, jobs.Options{Name: "a", Discover: 200 * time.Millisecond})
	t.Cleanup(runner.Close)
	if err := runner.Serve(features.LocalTenantID); err != nil {
		t.Fatal(err)
	}
	return &stack{t: t, reg: reg, bus: b, subs: subs, p: p, data: dataDeps}
}

func until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// do sends a Nexus command as a user.
func (s *stack) do(as uint64, command, target string, args any) bus.Result {
	if args == nil {
		args = map[string]any{}
	}
	raw, _ := json.Marshal(args)
	return s.bus.Execute(bus.Request{ServiceArgs: user(as), ID: fmt.Sprintf("c%d", s.n.Add(1)), Type: "nexus." + command, Target: target, Args: raw})
}

func (s *stack) ok(as uint64, command, target string, args any) {
	s.t.Helper()
	if res := s.do(as, command, target, args); res.Common.Code >= 300 {
		s.t.Fatalf("%d %s %s: %s %+v", as, command, target, res.Common.Message, res.Problems)
	}
}

func (s *stack) refused(as uint64, command, target string, args any, want string) {
	s.t.Helper()
	res := s.do(as, command, target, args)
	if res.Common.Code < 300 {
		s.t.Fatalf("%d %s %s was not refused", as, command, target)
	}
	got := res.Common.Message
	for _, p := range res.Problems {
		got += " " + p.Message
	}
	if !strings.Contains(got, want) {
		s.t.Fatalf("%d %s %s: refused with %q, not %q", as, command, target, got, want)
	}
}

// query runs a Nexus query once as a user.
func (s *stack) query(as uint64, name string, params any) map[string]any {
	s.t.Helper()
	raw, _ := json.Marshal(params)
	v, _, fail := s.subs.Once(user(as), "nexus."+name, raw)
	if fail != nil {
		s.t.Fatalf("%s: %s", name, fail.Message)
	}
	b, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func (s *stack) settle() {
	_ = s.p.Settle(context.Background(), features.LocalTenantID, "records.record")
}

func (s *stack) get(as uint64, entity, id string) data.Record {
	s.settle()
	r, _, err := data.Get(user(as), s.data, entity, id)
	if err != nil || len(r) == 0 {
		return nil
	}
	return r
}

func (s *stack) find(as uint64, entity string, where map[string]any) []data.Record {
	s.settle()
	w := map[string]json.RawMessage{}
	for k, v := range where {
		w[k], _ = json.Marshal(v)
	}
	res, err := data.Find(user(as), s.data, data.Query{Entity: entity, Where: w, Limit: 100})
	if err != nil {
		s.t.Fatal(err)
	}
	return res.Records
}

func str(r data.Record, field string) string {
	var v any
	_ = json.Unmarshal(r[field], &v)
	return fmt.Sprint(v)
}

func TestChannels(t *testing.T) {
	s := newStack(t)

	// Ana makes #design; Bo must join before posting
	s.ok(ana, "channel.create", "c1", map[string]any{"name": " Design ", "topic": "Mockups"})
	if r := s.get(bo, nexus.EntityChannel, "c1"); str(r, "name") != "design" {
		t.Fatalf("the channel as Bo sees it: %v", r)
	}
	s.refused(bo, "message.post", "m1", map[string]any{"channel": "c1", "text": "hi"}, "join the channel")
	s.ok(bo, "channel.join", "c1", nil)
	s.ok(bo, "channel.join", "c1", nil) // again: nothing changes
	s.ok(bo, "message.post", "m1", map[string]any{"channel": "c1", "text": "hi @ana", "mentions": []string{"7"}})
	s.ok(bo, "message.post", "m1", map[string]any{"channel": "c1", "text": "hi @ana", "mentions": []string{"7"}}) // a retry with a new command id
	if got := s.find(ana, nexus.EntityMessage, map[string]any{"channel": "c1"}); len(got) != 1 {
		t.Fatalf("one message posted twice made %d", len(got))
	}
	s.ok(ana, "message.post", "m2", map[string]any{"channel": "c1", "text": "welcome"})

	// The unread projector counts them; marking read follows
	until(t, "the channel's count", func() bool {
		c, _ := s.query(ana, "unread", map[string]any{"channel": "c1"})["count"].(float64)
		return c == 2
	})
	chans, _ := s.query(ana, "channels", map[string]any{})["channels"].([]any)
	if len(chans) != 1 || chans[0].(map[string]any)["name"] != "design" || chans[0].(map[string]any)["role"] != "admin" {
		t.Fatalf("Ana's channels: %v", chans)
	}
	s.ok(ana, "channel.markRead", "c1", map[string]any{"count": 2})
	if r := s.get(ana, nexus.EntityMember, "c1-7"); str(r, "seen") != "2" {
		t.Fatalf("Ana's membership after mark read: %v", r)
	}

	// Only the author edits or deletes
	s.refused(ana, "message.edit", "m1", map[string]any{"text": "changed"}, "only its author")
	s.ok(bo, "message.edit", "m1", map[string]any{"text": "hello @ana"})
	if r := s.get(ana, nexus.EntityMessage, "m1"); str(r, "text") != "hello @ana" || str(r, "edited") != "true" {
		t.Fatalf("edited: %v", r)
	}
	s.ok(ana, "message.delete", "m2", nil)
	if r := s.get(bo, nexus.EntityMessage, "m2"); str(r, "deleted") != "true" || str(r, "text") != "" {
		t.Fatalf("deleted: %v", r)
	}

	// Reactions toggle
	s.ok(ana, "reaction.toggle", "m1", map[string]any{"emoji": "👍"})
	s.ok(ana, "reaction.toggle", "m1", map[string]any{"emoji": "🎉"})
	if r := s.get(bo, nexus.EntityReaction, "m1-7"); !strings.Contains(string(r["emoji"]), "🎉") || !strings.Contains(string(r["emoji"]), "👍") {
		t.Fatalf("reactions: %v", r)
	}
	s.ok(ana, "reaction.toggle", "m1", map[string]any{"emoji": "👍"})
	s.ok(ana, "reaction.toggle", "m1", map[string]any{"emoji": "🎉"})
	if r := s.get(bo, nexus.EntityReaction, "m1-7"); r != nil {
		t.Fatalf("reactions all taken back: %v", r)
	}

	// Ana was mentioned once
	until(t, "Ana's mention", func() bool {
		raw, _ := json.Marshal(map[string]any{})
		v, _, fail := s.subs.Once(user(ana), notifications.MineQuery, raw)
		if fail != nil {
			t.Fatal(fail.Message)
		}
		b, _ := json.Marshal(v)
		return strings.Contains(string(b), "You were mentioned in #design")
	})

	// Archived: no more posts or joins, and only the owner archives
	s.refused(bo, "channel.archive", "c1", nil, "owner")
	s.ok(ana, "channel.archive", "c1", nil)
	s.refused(bo, "message.post", "m3", map[string]any{"channel": "c1", "text": "late"}, "archived")
	s.refused(cy, "channel.join", "c1", nil, "archived")
}

func TestPrivateChannels(t *testing.T) {
	s := newStack(t)
	s.ok(ana, "channel.create", "p1", map[string]any{"name": "secret", "private": true})
	if r := s.get(ana, nexus.EntityChannel, "p1"); r == nil {
		t.Fatal("its owner cannot see the private channel")
	}
	if r := s.get(cy, nexus.EntityChannel, "p1"); r != nil {
		t.Fatalf("someone else sees a private channel: %v", r)
	}
	s.refused(cy, "channel.join", "p1", nil, "no channel")
	s.refused(cy, "channel.addMember", "p1", map[string]any{"user": "9"}, "no channel")

	// Ana adds Bo; the app gives Bo access
	s.ok(ana, "channel.addMember", "p1", map[string]any{"user": "8"})
	until(t, "Bo's access", func() bool { return s.get(bo, nexus.EntityChannel, "p1") != nil })
	s.ok(bo, "message.post", "pm1", map[string]any{"channel": "p1", "text": "only us, not @cy", "mentions": []string{"7", "9"}})
	if got := s.find(cy, nexus.EntityMessage, map[string]any{"channel": "p1"}); len(got) != 0 {
		t.Fatalf("an outsider reads a private channel: %v", got)
	}
	if c, _ := s.query(cy, "unread", map[string]any{"channel": "p1"})["count"].(float64); c != 0 {
		t.Fatalf("an outsider reads a private channel's count: %v", c)
	}
	until(t, "Ana's mention", func() bool {
		raw, _ := json.Marshal(map[string]any{})
		v, _, _ := s.subs.Once(user(ana), notifications.MineQuery, raw)
		b, _ := json.Marshal(v)
		return strings.Contains(string(b), "#secret")
	})
	time.Sleep(300 * time.Millisecond)
	raw, _ := json.Marshal(map[string]any{})
	v, _, _ := s.subs.Once(user(cy), notifications.MineQuery, raw)
	if b, _ := json.Marshal(v); strings.Contains(string(b), "secret") || strings.Contains(string(b), "only us") {
		t.Fatalf("an outsider was notified of a private message: %s", b)
	}

	// A membership an outsider makes for themselves gives no access
	raw, _ = json.Marshal(map[string]any{"entity": nexus.EntityMember, "values": map[string]any{"channel": "p1", "user": "9", "private": true}})
	_ = s.bus.Execute(bus.Request{ServiceArgs: user(cy), ID: "forged", Type: "record.create", Target: "p1-9", Args: raw})
	time.Sleep(500 * time.Millisecond)
	if r := s.get(cy, nexus.EntityChannel, "p1"); r != nil {
		t.Fatal("a forged membership gave access to a private channel")
	}

	// Leaving takes access away
	s.ok(bo, "channel.leave", "p1", nil)
	until(t, "Bo's access taken", func() bool { return s.get(bo, nexus.EntityChannel, "p1") == nil })

	// Direct conversations: one per pair, both members, private
	s.refused(ana, "dm.open", "dm-x", map[string]any{"user": "8"}, "dm-7-8")
	s.ok(ana, "dm.open", "dm-7-8", map[string]any{"user": "8"})
	s.ok(bo, "dm.open", "dm-7-8", map[string]any{"user": "7"})
	until(t, "Bo's direct conversation", func() bool { return s.get(bo, nexus.EntityChannel, "dm-7-8") != nil })
	s.ok(bo, "message.post", "d1", map[string]any{"channel": "dm-7-8", "text": "psst"})
	if r := s.get(cy, nexus.EntityMessage, "d1"); r != nil {
		t.Fatal("someone else reads a direct conversation")
	}
	s.refused(ana, "channel.addMember", "dm-7-8", map[string]any{"user": "9"}, "two people")
}

// The app as its document passes the blueprints-only check.
func TestDocument(t *testing.T) {
	s := newStack(t)
	d, err := nexus.Document()
	if err != nil {
		t.Fatal(err)
	}
	if problems := apps.CheckDocument(d, apps.DocumentEnvOf(s.reg)); len(problems) > 0 {
		t.Fatalf("%+v", problems)
	}
}
