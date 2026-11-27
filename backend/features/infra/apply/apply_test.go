package infraapply_test

import (
	"encoding/json"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/controlplane"
	"github.com/SgtFlame/foundation/backend/features/infra"
	infraapply "github.com/SgtFlame/foundation/backend/features/infra/apply"
	"github.com/SgtFlame/foundation/backend/features/infra/infratest"
)

type m = map[string]any

func (f *fixture) control(op string, a any) {
	f.t.Helper()
	raw, _ := json.Marshal(a)
	if _, err := f.sess.Call(adapter.Context{Context: root().Ctx, Args: root(), Contract: infratest.ControlRef}, op, raw); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) commit(msg string) int {
	f.t.Helper()
	c := f.config()
	f.must("infra.config.commit", infra.DefaultConfig, m{"message": msg, "expect": c.Rev})
	return f.config().Head
}

func (f *fixture) planAndApply(version int) (infra.PlanRecord, infraapply.Apply) {
	f.t.Helper()
	p, err := infra.MakePlan(root(), f.reg, f.infra, "", version)
	if err != nil {
		f.t.Fatal(err)
	}
	if len(p.Steps) == 0 {
		return p, infraapply.Apply{}
	}
	f.must("infra.apply", "", m{"plan": p.ID})
	return p, f.drive(p.ID)
}

func TestApplyDriftAndRollback(t *testing.T) {
	f := newFixture(t)
	put := func(kind string, item m) {
		f.must("infra.config.put", infra.DefaultConfig, m{"kind": kind, "item": item})
	}
	put("accounts", m{"id": "test", "name": "Test", "provider": "infratest", "connection": "c1"})
	put("sites", m{"id": "west", "name": "West", "account": "test", "region": "r1"})
	put("clusters", m{"id": "west", "site": "west"})
	put("hostPools", m{"id": "general", "site": "west", "size": "m", "count": 2})
	put("profiles", m{"id": "web", "name": "Web", "kind": "container", "packages": []string{"nginx"}, "units": []m{{"name": "web", "exec": "/usr/bin/web"}}})
	put("placements", m{"id": "web", "profile": "web", "sites": []m{{"site": "west", "count": 1}}})
	v1 := f.commit("Two hosts and a web instance")

	// Apply version 1: every step done, the configuration applied.
	p, a := f.planAndApply(v1)
	if len(p.Steps) != 4 || a.Status != infraapply.ApplyDone {
		t.Fatalf("apply = %+v", a)
	}
	if c := f.config(); c.Applied != v1 {
		t.Fatalf("applied = %d", c.Applied)
	}
	if hs := f.hosts(); len(hs) != 2 {
		t.Fatalf("hosts = %+v", hs)
	}
	if r := f.record(controlplane.EntityUnit, "instance_west_web_web_1__web"); r == nil {
		t.Fatal("no unit record")
	}
	if r := f.record(controlplane.EntityPackagePin, "profile_web__nginx"); r == nil {
		t.Fatal("no package pin")
	}
	// Applying it again changes nothing.
	if p, _ := f.planAndApply(v1); len(p.Steps) != 0 {
		t.Fatalf("a second plan = %+v", p.Steps)
	}
	// A step already done does nothing when its job runs again.
	if st, err := infraapply.RunStep(root(), f.apply, a.ID, "host:west/general/1"); err != nil || st != infraapply.StepDone {
		t.Fatalf("rerun = %s %v", st, err)
	}
	if hs := f.hosts(); len(hs) != 2 {
		t.Fatalf("hosts after a rerun = %+v", hs)
	}

	// A host vanishes: drift finds it; correct applies the fix, whose
	// first attempt fails and is resumed.
	f.control("vanish", m{"host": "west-general-2"})
	dr, err := infraapply.CheckDrift(root(), f.reg, f.apply, "")
	if err != nil || len(dr.Plan.Steps) != 1 || dr.Applied != "" {
		t.Fatalf("drift = %+v %v", dr, err)
	}
	f.must("infra.config.settings", infra.DefaultConfig, m{"onDrift": "correct"})
	f.control("fail", m{"operation": "host.create", "kind": "invalid"})
	if dr, err = infraapply.CheckDrift(root(), f.reg, f.apply, ""); err != nil || dr.Applied == "" {
		t.Fatalf("correcting drift = %+v %v", dr, err)
	}
	if a := f.drive(dr.Applied); a.Status != infraapply.ApplyFailed {
		t.Fatalf("after a failing create = %+v", a)
	}
	f.must("infra.apply.resume", dr.Applied, m{})
	if a := f.drive(dr.Applied); a.Status != infraapply.ApplyDone || a.Steps[0].Attempt != 1 {
		t.Fatalf("after resume = %+v", a)
	}
	if hs := f.hosts(); len(hs) != 2 {
		t.Fatalf("hosts after correcting = %+v", hs)
	}
	// A retryable failure is left for the job to retry, recording nothing.
	f.control("vanish", m{"host": "west-general-1"})
	f.control("fail", m{"operation": "host.create", "kind": "unavailable"})
	p3, err := infra.MakePlan(root(), f.reg, f.infra, "", v1)
	if err != nil {
		t.Fatal(err)
	}
	f.must("infra.apply", "", m{"plan": p3.ID})
	if _, err := infraapply.RunStep(root(), f.apply, p3.ID, "host:west/general/1"); err == nil {
		t.Fatal("a retryable failure was not returned")
	}
	if a := f.drive(p3.ID); a.Status != infraapply.ApplyDone {
		t.Fatalf("after the retry = %+v", a)
	}

	// Roll back to no instance: its removal holds data and waits for an
	// approval.
	put("placements", m{"id": "web", "profile": "web", "sites": []m{{"site": "west", "count": 0}}})
	v2 := f.commit("No web instance")
	p, a = f.planAndApply(v2)
	if len(p.Steps) != 1 || a.Status != infraapply.ApplyWaiting || a.Steps[0].Status != infraapply.StepProposed {
		t.Fatalf("removal = %+v", a)
	}
	if f.record(controlplane.EntityInstance, "instance_west_web_web_1") == nil {
		t.Fatal("the instance was removed before its approval")
	}
	f.must("infra.apply.approve", a.ID, m{"step": a.Steps[0].ID})
	if a = f.drive(a.ID); a.Status != infraapply.ApplyDone {
		t.Fatalf("after approval = %+v", a)
	}
	if f.record(controlplane.EntityInstance, "instance_west_web_web_1") != nil {
		t.Fatal("the instance is still there")
	}
	if c := f.config(); c.Applied != v2 {
		t.Fatalf("applied = %d", c.Applied)
	}
	// A plan with refusals, or an applied one, is refused.
	if r := f.send("infra.apply", "", m{"plan": a.ID}); r.Common.ErrorCode != "already_applied" {
		t.Fatalf("applying twice = %+v", r.Common)
	}
}

// The step graph resolves without errors over the nodes the fixture has.
func TestStepGraphResolves(t *testing.T) {
	f := newFixture(t)
	for _, b := range cognition.Graphs.All(f.reg) {
		if b.Graph.ID != infraapply.StepGraph {
			continue
		}
		for _, p := range cognition.CatalogOf(f.reg).Resolve(b.Graph).Problems {
			if p.Severity == "error" {
				t.Errorf("%+v", p)
			}
		}
		return
	}
	t.Fatal("no step graph")
}
