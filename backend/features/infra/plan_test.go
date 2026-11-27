package infra_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/infra"
	"github.com/SgtFlame/foundation/backend/features/infra/infratest"
)

func kindsReg() *extensions.Registry {
	reg := extensions.NewRegistry()
	infra.Contribute(reg, func() infra.Deps { return infra.Deps{} })
	return reg
}

func sample() infra.Document {
	return infra.Document{
		Accounts:   []infra.Account{{ID: "test", Name: "Test", Provider: "infratest", Connection: "c1"}},
		Sites:      []infra.Site{{ID: "west", Name: "West", Account: "test", Region: "r1"}},
		Clusters:   []infra.Cluster{{ID: "west", Site: "west"}},
		HostPools:  []infra.HostPool{{ID: "general", Site: "west", Size: "m", Count: 3}},
		Profiles:   []infra.Profile{{ID: "web", Name: "Web", Kind: "container"}},
		Placements: []infra.Placement{{ID: "web", Profile: "web", Sites: []infra.SiteCount{{Site: "west", Count: 2}}}},
	}
}

func stepList(p infra.PlanResult) string {
	var out []string
	for _, s := range p.Steps {
		out = append(out, s.Op+" "+s.ID)
	}
	return strings.Join(out, "\n")
}

func TestPlanFromNothing(t *testing.T) {
	reg := kindsReg()
	desired, kinds := infra.Expand(reg, sample())
	p := infra.Plan(desired, kinds, infra.Actual{})
	want := `create host:west/general/1
create host:west/general/2
create host:west/general/3
create cluster:west
create instance:west/web/web/1
create instance:west/web/web/2`
	if got := stepList(p); got != want || len(p.Refusals) != 0 {
		t.Fatalf("plan =\n%s\nrefusals %+v", got, p.Refusals)
	}
	if c := p.Steps[3]; len(c.After) != 3 {
		t.Fatalf("the cluster waits for %v", c.After)
	}
	// Against itself: nothing.
	if p := infra.Plan(desired, kinds, infra.Actual{Resources: desired}); len(p.Steps) != 0 {
		t.Fatalf("plan against itself =\n%s", stepList(p))
	}
}

func TestPlanRemovesASiteInReverse(t *testing.T) {
	reg := kindsReg()
	before, kinds := infra.Expand(reg, sample())
	p := infra.Plan(nil, kinds, infra.Actual{Resources: before})
	first := map[string]int{}
	for i, s := range p.Steps {
		if s.Op != infra.OpRemove {
			t.Fatalf("step %d is %s", i, s.Op)
		}
		if _, ok := first[s.Kind]; !ok {
			first[s.Kind] = i
		}
	}
	if !(first["instance"] < first["cluster"] && first["cluster"] < first["host"]) {
		t.Fatalf("removal order =\n%s", stepList(p))
	}
	if !p.Steps[0].HoldsData {
		t.Error("removing an instance does not say it holds data")
	}
}

func TestPlanRefusesVMsWithoutAHostForThem(t *testing.T) {
	reg := kindsReg()
	doc := sample()
	doc.Profiles[0].Kind = "vm"
	desired, kinds := infra.Expand(reg, doc)
	p := infra.Plan(desired, kinds, infra.Actual{Sizes: map[string][]infra.ProviderSize{"test": infratest.Sizes}})
	if len(p.Refusals) != 2 || !strings.Contains(p.Refusals[0].Reason, "no host on west can run VMs") {
		t.Fatalf("refusals = %+v", p.Refusals)
	}
	doc.HostPools[0].Size = "l" // l runs VMs
	desired, _ = infra.Expand(reg, doc)
	if p := infra.Plan(desired, kinds, infra.Actual{Sizes: map[string][]infra.ProviderSize{"test": infratest.Sizes}}); len(p.Refusals) != 0 {
		t.Fatalf("refusals with l = %+v", p.Refusals)
	}
}

func TestPlanNotesASmallCluster(t *testing.T) {
	reg := kindsReg()
	doc := sample()
	doc.HostPools[0].Count = 2
	desired, kinds := infra.Expand(reg, doc)
	p := infra.Plan(desired, kinds, infra.Actual{})
	if len(p.Notes) != 1 || !strings.Contains(p.Notes[0], "2 replica") {
		t.Fatalf("notes = %v", p.Notes)
	}
}

// testCompute calls the Infrastructure test server's session directly.
type testCompute struct{ s adapter.Session }

func (c testCompute) Call(args features.ServiceArgs, account infra.Account, op string, in any) (json.RawMessage, error) {
	raw, _ := json.Marshal(in)
	ctx := adapter.Context{Context: context.Background(), Args: args, Contract: infra.ComputeRef, Operation: op}
	out, err := c.s.Call(ctx, op, raw)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func openTestServer(t *testing.T) adapter.Session {
	t.Helper()
	ctx := adapter.Context{Context: context.Background(), Args: operator(7)}
	s, err := infratest.New().Impl().Open(ctx, adapter.Connection{ID: "c1", Type: infratest.TypeID, Tenant: features.RootTenantID})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMakePlanOnTheTestServer(t *testing.T) {
	st := newStack(t)
	seedDraft(st)
	sess := openTestServer(t)
	deps := infra.Deps{Bus: st.bus, Store: st.p, Compute: testCompute{sess}}
	hostCreates := func(p infra.PlanRecord) int {
		n := 0
		for _, s := range p.Steps {
			if s.Kind == infra.KindHost && s.Op == infra.OpCreate {
				n++
			}
		}
		return n
	}
	p, err := infra.MakePlan(operator(7), st.reg, deps, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if hostCreates(p) != 3 || len(p.Refusals) != 0 {
		t.Fatalf("plan = %+v", p)
	}
	var stored infra.PlanRecord
	if !st.get(infra.PlanProjector, infra.PlanType+"."+p.ID, &stored) || stored.Status != infra.PlanReady || len(stored.Steps) != len(p.Steps) || stored.CreatedBy != "7" {
		t.Fatalf("stored = %+v", stored)
	}
	// Make the three hosts, then vanish one: the plan creates only it.
	call := testCompute{sess}
	for i := 1; i <= 3; i++ {
		if _, err := call.Call(operator(7), infra.Account{}, "host.create", map[string]any{"id": infra.ProviderName("west", "general", i), "size": "m", "region": "r1"}); err != nil {
			t.Fatal(err)
		}
	}
	if p, _ = infra.MakePlan(operator(7), st.reg, deps, "", 0); hostCreates(p) != 0 {
		t.Fatalf("plan after making the hosts =\n%s", stepList(p.PlanResult))
	}
	raw, _ := json.Marshal(map[string]any{"host": infra.ProviderName("west", "general", 2)})
	if _, err := sess.Call(adapter.Context{Context: context.Background(), Args: operator(7), Contract: infratest.ControlRef}, "vanish", raw); err != nil {
		t.Fatal(err)
	}
	if p, _ = infra.MakePlan(operator(7), st.reg, deps, "", 0); hostCreates(p) != 1 {
		t.Fatalf("plan after a host vanished =\n%s", stepList(p.PlanResult))
	}
}
