package controlplane_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/branches"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	appimpl "github.com/SgtFlame/foundation/backend/features/apps/services/appservice/impl"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	cognitionnodes "github.com/SgtFlame/foundation/backend/features/cognition/nodes"
	"github.com/SgtFlame/foundation/backend/features/controlplane"
	"github.com/SgtFlame/foundation/backend/features/controlplane/controlplanetest"
	"github.com/SgtFlame/foundation/backend/features/controlplane/ctl"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
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
)

func init() { ctl.HealthTTL = 2 * time.Second }

type stack struct {
	t    *testing.T
	bus  *bus.Bus
	p    *project.Projections
	data data.Deps
	ctl  *ctl.Control
	n    atomic.Int64
}

// newStack is the live core with records and apps, as serve wires them,
// the infrastructure app seeded into the root tenant and the control
// plane's followers running.
func newStack(t *testing.T) *stack {
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
		branchDeps branches.Deps
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
	b := bus.New(p)
	subs := subscribe.New(p, reg)
	nodeDeps = livenodes.Deps{Engine: eng, Bus: b, Subscriptions: subs, Store: p}
	draftDeps = drafts.Deps{Engine: eng, Projections: p}
	dataDeps = data.Deps{Projections: p, Bus: b}
	entityDeps = entities.Deps{Projections: p, DB: gdb, OnPublish: func(args features.ServiceArgs) error { return data.Follow(args, dataDeps) }}
	prefabDeps = prefabs.Deps{Projections: p, DB: gdb}
	appDeps = manifests.Deps{Projections: p, DB: gdb}
	installDep = installs.Deps{Projections: p, DB: gdb, Nodes: nodeDeps}
	branchDeps = branches.Deps{Projections: p}
	b.Resolve = installs.CommandFor(func() installs.Deps { return installDep }, livenodes.CommandFor(func() livenodes.Deps { return nodeDeps }))
	subs.Resolve = installs.QueryFor(func() installs.Deps { return installDep }, livenodes.QueryFor(func() livenodes.Deps { return nodeDeps }))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc := appimpl.New(appimpl.WithDeps(func() appimpl.Deps {
		return appimpl.Deps{Projections: p, DB: gdb, Bus: b, Subscriptions: subs, Registry: reg}
	}))
	args := features.ServiceArgs{Ctx: ctx}
	if err := controlplane.SeedApp(args, p, svc, b); err != nil {
		t.Fatal(err)
	}
	if err := controlplane.SeedApp(args, p, svc, b); err != nil {
		t.Fatalf("a second seed: %v", err)
	}
	s := &stack{t: t, bus: b, p: p, data: dataDeps, ctl: &ctl.Control{NC: srv.Conn(), JS: srv.JS()}}
	deps := controlplane.Deps{Projections: p, Data: dataDeps, Bus: b, Control: s.ctl, Sweep: 200 * time.Millisecond}
	if err := controlplane.Start(ctx, func() controlplane.Deps { return deps }); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *stack) create(entity string, values map[string]any) string {
	s.t.Helper()
	raw, _ := json.Marshal(map[string]any{"entity": entity, "values": values})
	res := s.bus.Execute(bus.Request{ServiceArgs: controlplane.RootArgs(features.ServiceArgs{Ctx: context.Background()}), ID: fmt.Sprintf("c%d", s.n.Add(1)), Type: "record.create", Args: raw})
	if res.Common.Code >= 300 {
		s.t.Fatalf("create %s: %s %+v", entity, res.Common.Message, res.Problems)
	}
	return res.Target
}

func (s *stack) set(entity, id string, set map[string]any) {
	s.t.Helper()
	raw, _ := json.Marshal(map[string]any{"entity": entity, "set": set})
	res := s.bus.Execute(bus.Request{ServiceArgs: controlplane.RootArgs(features.ServiceArgs{Ctx: context.Background()}), ID: fmt.Sprintf("c%d", s.n.Add(1)), Type: "record.update", Target: id, Args: raw})
	if res.Common.Code >= 300 {
		s.t.Fatalf("update %s: %s %+v", entity, res.Common.Message, res.Problems)
	}
}

func (s *stack) field(entity, id, field string) string {
	r, _, err := data.Get(controlplane.RootArgs(features.ServiceArgs{Ctx: context.Background()}), s.data, entity, id)
	if err != nil || r == nil {
		return ""
	}
	return string(r[field])
}

func until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting until %s", what)
}

func TestDesiredAndActualState(t *testing.T) {
	ctx := context.Background()
	s := newStack(t)
	host := s.create(controlplane.EntityHost, map[string]any{"name": "lxd1", "site": "east", "provider": "lxd"})
	prof := s.create(controlplane.EntityProfile, map[string]any{"name": "serve", "kind": "container", "image": "ubuntu/24.04"})
	inst := s.create(controlplane.EntityInstance, map[string]any{"name": "serve1", "host": host, "profile": prof})
	s.create(controlplane.EntityUnit, map[string]any{"instance": inst, "name": "serve", "kind": "serve", "channel": "stable"})
	s.create(controlplane.EntityPackagePin, map[string]any{"target": prof, "name": "foundation-serve", "source": "firstParty", "channel": "stable"})

	it := ctl.Target{Kind: ctl.KindInstance, ID: inst}
	var doc struct {
		Instance map[string]any   `json:"instance"`
		Profile  map[string]any   `json:"profile"`
		Units    []map[string]any `json:"units"`
		Pins     []map[string]any `json:"pins"`
	}
	until(t, "the instance's desired state is resolved", func() bool {
		raw, ok, _ := s.ctl.Desired(ctx, it)
		return ok && json.Unmarshal(raw, &doc) == nil && len(doc.Units) == 1 && len(doc.Pins) == 1
	})
	if doc.Instance["kind"] != "container" || doc.Profile["image"] != "ubuntu/24.04" || doc.Instance["status"] != "running" {
		t.Fatalf("resolved %+v", doc)
	}
	if _, has := doc.Instance["health"]; has {
		t.Fatal("a reported field is in desired state")
	}
	ht := ctl.Target{Kind: ctl.KindHost, ID: host}
	until(t, "the host's desired state lists the instance", func() bool {
		raw, ok, _ := s.ctl.Desired(ctx, ht)
		var h struct{ Instances []map[string]any }
		return ok && json.Unmarshal(raw, &h) == nil && len(h.Instances) == 1
	})

	// A daemon: a change nudges it, its events reach the record
	fake := &controlplanetest.FakeDaemon{C: s.ctl, Target: it}
	if err := fake.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer fake.Stop()
	s.set(controlplane.EntityInstance, inst, map[string]any{"status": "stopped"})
	until(t, "the daemon is nudged and its outcome reported", func() bool {
		return len(fake.Requests()) > 0 && json.Valid([]byte(s.field(controlplane.EntityInstance, inst, "lastEvent"))) &&
			s.field(controlplane.EntityInstance, inst, "lastEvent") != "" && s.field(controlplane.EntityInstance, inst, "lastEvent") != "null"
	})
	if r := fake.Requests()[0]; r.Verb != "reconcile" {
		t.Fatalf("request %+v", r)
	}
	// A reported field changing nudges nobody
	before := len(fake.Requests())
	if err := fake.SetFacts(ctx, map[string]any{"os": "ubuntu 24.04"}); err != nil {
		t.Fatal(err)
	}
	until(t, "facts reach the record", func() bool {
		return s.field(controlplane.EntityInstance, inst, "facts") == `{"os":"ubuntu 24.04"}`
	})
	time.Sleep(300 * time.Millisecond)
	if n := len(fake.Requests()); n != before {
		t.Fatalf("reported facts nudged the daemon (%d requests, then %d)", before, n)
	}

	// Heartbeats: up, then down once they stop
	fake.Beat(ctx, 300*time.Millisecond)
	until(t, "the instance is up", func() bool { return s.field(controlplane.EntityInstance, inst, "health") == `"up"` })
	fake.StopBeating()
	until(t, "the instance is down", func() bool { return s.field(controlplane.EntityInstance, inst, "health") == `"down"` })

	// One request reported ten times is one done
	for i := 0; i < 10; i++ {
		if err := s.ctl.Report(ctx, ctl.Event{Request: "r-once", Kind: ctl.EventDone, Target: it}); err != nil {
			t.Fatal(err)
		}
	}
	st, err := s.ctl.JS.Stream(ctx, ctl.StreamName)
	if err != nil {
		t.Fatal(err)
	}
	msgs := 0
	for seq := uint64(1); ; seq++ {
		m, err := st.GetMsg(ctx, seq)
		if err != nil {
			break
		}
		var e ctl.Event
		_ = json.Unmarshal(m.Data, &e)
		if e.Request == "r-once" {
			msgs++
		}
	}
	if msgs != 1 {
		t.Fatalf("r-once is on CTL %d times", msgs)
	}

	// Deleting the instance removes its desired state
	raw, _ := json.Marshal(map[string]any{"entity": controlplane.EntityInstance})
	if res := s.bus.Execute(bus.Request{ServiceArgs: controlplane.RootArgs(features.ServiceArgs{Ctx: ctx}), ID: "del", Type: "record.delete", Target: inst, Args: raw}); res.Common.Code >= 300 {
		t.Fatalf("delete: %s", res.Common.Message)
	}
	until(t, "the instance's desired state is gone", func() bool {
		_, ok, _ := s.ctl.Desired(ctx, it)
		return !ok
	})
}
