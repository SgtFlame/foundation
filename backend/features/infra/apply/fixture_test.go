package infraapply_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

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
	"github.com/SgtFlame/foundation/backend/features/cognition/traces"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/controlplane"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/infra"
	infraapply "github.com/SgtFlame/foundation/backend/features/infra/apply"
	"github.com/SgtFlame/foundation/backend/features/infra/infratest"
	infranodes "github.com/SgtFlame/foundation/backend/features/infra/nodes"
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

type fixture struct {
	t     *testing.T
	reg   *extensions.Registry
	data  data.Deps
	deps  infranodes.Deps
	sess  adapter.Session
	types map[string]cognition.NodeType
	bus   *bus.Bus
	p     *project.Projections
	infra infra.Deps
	apply infraapply.Deps
	n     int
}

// newFixture is the infrastructure app installed in the root tenant, the
// nodes and the Infrastructure test server.
func newFixture(t *testing.T) *fixture {
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
		infraDeps  infranodes.Deps
		ideps      infra.Deps
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
	infranodes.Contribute(reg, func() infranodes.Deps { return infraDeps })
	infra.Contribute(reg, func() infra.Deps { return ideps })
	infraapply.Contribute(reg, func() infraapply.Deps { return infraapply.Deps{Infra: ideps, Nodes: infraDeps} })
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
	installDep = installs.Deps{Projections: p, DB: gdb, Nodes: nodeDeps}
	branchDeps = branches.Deps{Projections: p}
	b.Resolve = installs.CommandFor(func() installs.Deps { return installDep }, livenodes.CommandFor(func() livenodes.Deps { return nodeDeps }))
	subs.Resolve = installs.QueryFor(func() installs.Deps { return installDep }, livenodes.QueryFor(func() livenodes.Deps { return nodeDeps }))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc := appimpl.New(appimpl.WithDeps(func() appimpl.Deps {
		return appimpl.Deps{Projections: p, DB: gdb, Bus: b, Subscriptions: subs, Registry: reg}
	}))
	if err := controlplane.SeedApp(features.ServiceArgs{Ctx: ctx}, p, svc, b); err != nil {
		t.Fatal(err)
	}
	sess, err := infratest.New().Impl().Open(adapter.Context{Context: ctx, Args: root()}, adapter.Connection{ID: "c1", Type: infratest.TypeID, Tenant: features.RootTenantID})
	if err != nil {
		t.Fatal(err)
	}
	infraDeps = infranodes.Deps{Bus: b, Data: dataDeps, Compute: infratest.Compute{Session: sess}}
	ideps = infra.Deps{Bus: b, Store: p, Compute: infratest.Compute{Session: sess}, Actual: infraapply.RecordsActual(reg, func() infranodes.Deps { return infraDeps })}
	f := &fixture{t: t, reg: reg, data: dataDeps, deps: infraDeps, sess: sess, types: map[string]cognition.NodeType{},
		bus: b, p: p, infra: ideps, apply: infraapply.Deps{Infra: ideps, Nodes: infraDeps}}
	for _, nt := range cognition.Nodes.All(reg) {
		f.types[nt.ID] = nt
	}
	return f
}

func root() features.ServiceArgs {
	return controlplane.RootArgs(features.ServiceArgs{Ctx: context.Background()})
}

func (f *fixture) run(node string, config string, in cognition.Values) (cognition.Values, string) {
	f.t.Helper()
	nt, ok := f.types[node]
	if !ok {
		f.t.Fatalf("no node %s", node)
	}
	x := &cognition.Exec{ServiceArgs: root(), Node: cognition.Node{Type: node, Config: json.RawMessage(config)}}
	out, pin, err := nt.Run(x, in)
	if err != nil {
		f.t.Fatalf("%s: %v", node, err)
	}
	return out, pin
}

func (f *fixture) record(entity, id string) data.Record {
	f.t.Helper()
	if err := f.data.Projections.Settle(context.Background(), features.RootTenantID, data.Projector); err != nil {
		f.t.Fatal(err)
	}
	r, _, err := data.Get(root(), f.data, entity, id)
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

func (f *fixture) hosts() []infra.ProviderHost {
	raw, _ := infratest.Compute{Session: f.sess}.Call(root(), infra.Account{}, "host.list", map[string]any{})
	var l struct{ Hosts []infra.ProviderHost }
	_ = json.Unmarshal(raw, &l)
	return l.Hosts
}

func (f *fixture) send(typ, target string, a any) bus.Result {
	f.t.Helper()
	f.n++
	raw, _ := json.Marshal(a)
	return f.bus.Execute(bus.Request{ServiceArgs: root(), ID: fmt.Sprintf("t%d-%s", f.n, typ), Type: typ, Target: target, Args: raw})
}

func (f *fixture) must(typ, target string, a any) bus.Result {
	f.t.Helper()
	r := f.send(typ, target, a)
	if r.Common.Code >= 300 {
		f.t.Fatalf("%s: %d %s %+v", typ, r.Common.Code, r.Common.Message, r.Problems)
	}
	return r
}

func (f *fixture) get(projector, key string, into any) bool {
	f.t.Helper()
	ctx := context.Background()
	if err := f.p.Settle(ctx, features.RootTenantID, projector); err != nil {
		f.t.Fatal(err)
	}
	v, ok, err := f.p.Get(ctx, features.RootTenantID, projector, key)
	if err != nil {
		f.t.Fatal(err)
	}
	if ok && into != nil {
		_ = json.Unmarshal(v.Value, into)
	}
	return ok
}

func (f *fixture) config() infra.Config {
	var c infra.Config
	f.get(infra.ConfigProjector, infra.ConfigType+"."+infra.DefaultConfig, &c)
	return c
}

func (f *fixture) applyOf(id string) infraapply.Apply {
	var a infraapply.Apply
	f.get(infraapply.ApplyProjector, infraapply.ApplyType+"."+id, &a)
	return a
}

// drive runs the apply's ready steps, as their jobs would, until none is
// ready.
func (f *fixture) drive(id string) infraapply.Apply {
	f.t.Helper()
	for i := 0; i < 50; i++ {
		a := f.applyOf(id)
		ready := false
		for _, s := range a.Steps {
			if s.Status == infraapply.StepReady {
				ready = true
				if _, err := infraapply.RunStep(root(), f.apply, id, s.ID); err != nil {
					f.t.Logf("step %s: %v", s.ID, err)
				}
			}
		}
		if !ready {
			return a
		}
	}
	f.t.Fatal("the apply never settled")
	return infraapply.Apply{}
}
