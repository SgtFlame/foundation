package infranodes_test

import (
	"context"
	"encoding/json"
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
}

// compute calls the test server's session directly.
type compute struct{ s adapter.Session }

func (c compute) Call(args features.ServiceArgs, account infra.Account, op string, in any) (json.RawMessage, error) {
	raw, _ := json.Marshal(in)
	out, err := c.s.Call(adapter.Context{Context: context.Background(), Args: args, Contract: infra.ComputeRef, Operation: op}, op, raw)
	if err != nil {
		return nil, err
	}
	return out, nil
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
	infraDeps = infranodes.Deps{Bus: b, Data: dataDeps, Compute: compute{sess}}
	f := &fixture{t: t, reg: reg, data: dataDeps, deps: infraDeps, sess: sess, types: map[string]cognition.NodeType{}}
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
	raw, _ := compute{f.sess}.Call(root(), infra.Account{}, "host.list", map[string]any{})
	var l struct{ Hosts []infra.ProviderHost }
	_ = json.Unmarshal(raw, &l)
	return l.Hosts
}

func TestEnsureNodesAreIdempotent(t *testing.T) {
	f := newFixture(t)
	doc := infra.Document{
		Accounts:   []infra.Account{{ID: "test", Name: "Test", Provider: "infratest", Connection: "c1"}},
		Sites:      []infra.Site{{ID: "west", Name: "West", Account: "test", Region: "r1"}},
		HostPools:  []infra.HostPool{{ID: "general", Site: "west", Size: "m", Count: 1}},
		Profiles:   []infra.Profile{{ID: "web", Name: "Web", Kind: "container", Image: "ubuntu/24.04", Units: []infra.Unit{{Name: "web", Exec: "/usr/bin/web"}}}},
		Placements: []infra.Placement{{ID: "web", Profile: "web", Sites: []infra.SiteCount{{Site: "west", Count: 1}}}},
	}
	reg := extensions.NewRegistry()
	infra.Contribute(reg, func() infra.Deps { return infra.Deps{} })
	desired, kinds := infra.Expand(reg, doc)
	plan := infra.Plan(desired, kinds, infra.Actual{})
	var hostStep, instStep infra.Step
	for _, s := range plan.Steps {
		switch s.Kind {
		case infra.KindHost:
			hostStep = s
		case infra.KindInstance:
			instStep = s
		}
	}
	in := cognition.Values{"step": hostStep, "connection": "c1", "provider": "infratest"}
	var host string
	for i := 0; i < 2; i++ {
		out, pin := f.run("infra.ensureHost", "", in)
		if pin != cognition.Then {
			t.Fatalf("ensureHost %d: %s %v", i, pin, out)
		}
		host = out["host"].(string)
	}
	if hs := f.hosts(); len(hs) != 1 || hs[0].ID != "west-general-1" {
		t.Fatalf("hosts = %+v", hs)
	}
	if r := f.record(controlplane.EntityHost, host); r == nil || string(r["name"]) != `"west-general-1"` {
		t.Fatalf("host record = %v", r)
	}
	var inst string
	for i := 0; i < 2; i++ {
		out, _ := f.run("infra.ensureInstance", "", cognition.Values{"step": instStep, "host": host})
		inst = out["instance"].(string)
		if changed := out["changed"].(bool); changed != (i == 0) {
			t.Fatalf("ensureInstance %d changed = %v", i, changed)
		}
	}
	if r := f.record(controlplane.EntityInstance, inst); r == nil || string(r["host"]) != `"`+host+`"` {
		t.Fatalf("instance record = %v", r)
	}
	for i := 0; i < 2; i++ {
		out, _ := f.run("infra.ensureUnit", "", cognition.Values{"instance": inst, "unit": doc.Profiles[0].Units[0]})
		if changed := out["changed"].(bool); changed != (i == 0) {
			t.Fatalf("ensureUnit %d changed = %v", i, changed)
		}
	}
	out, _ := f.run("infra.ensurePackages", `{"source":"ubuntu"}`, cognition.Values{"target": "profile_web", "packages": []string{"nginx", "jq=1.7"}})
	if len(out["pins"].([]any)) != 2 {
		t.Fatalf("pins = %v", out)
	}
	if r := f.record(controlplane.EntityPackagePin, "profile_web__jq"); r == nil || string(r["version"]) != `"1.7"` {
		t.Fatalf("jq pin = %v", r)
	}
	// Wait healthy times out on a host no daemon reports.
	if _, pin := f.run("infra.waitHealthy", `{"timeoutSeconds":1}`, cognition.Values{"kind": "host", "id": host}); pin != "timedOut" {
		t.Fatalf("waitHealthy = %s", pin)
	}
	// Remove the instance, then the host, twice each.
	for i := 0; i < 2; i++ {
		out, pin := f.run("infra.remove", "", cognition.Values{"step": instStep})
		if pin != cognition.Then || out["removed"].(bool) != (i == 0) {
			t.Fatalf("remove instance %d = %s %v", i, pin, out)
		}
	}
	for i := 0; i < 2; i++ {
		out, pin := f.run("infra.remove", "", cognition.Values{"step": hostStep, "connection": "c1"})
		if pin != cognition.Then || out["removed"].(bool) != (i == 0) {
			t.Fatalf("remove host %d = %s %v", i, pin, out)
		}
	}
	if hs := f.hosts(); len(hs) != 0 {
		t.Fatalf("hosts after remove = %+v", hs)
	}
	if f.record(controlplane.EntityHost, host) != nil {
		t.Fatal("the host's record is still there")
	}
}

func TestNodesPassTheMetadataCheck(t *testing.T) {
	f := newFixture(t)
	for id, nt := range f.types {
		if len(id) > 6 && id[:6] == "infra." {
			if ps := cognition.MetaProblems(nt); len(ps) > 0 {
				t.Errorf("%s: %v", id, ps)
			}
		}
	}
}
