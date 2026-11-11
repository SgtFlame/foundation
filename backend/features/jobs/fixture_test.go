package jobs_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/installs"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	cognitionnodes "github.com/SgtFlame/foundation/backend/features/cognition/nodes"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/jobs"
	jobsnodes "github.com/SgtFlame/foundation/backend/features/jobs/nodes"
	"github.com/SgtFlame/foundation/backend/features/live"
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

// hit is one run of a test.hit node.
type hit struct {
	Name      string
	Job       string
	Attempt   int
	Principal features.Principal
	Value     any
	Start     time.Time
	End       time.Time
}

// hits records test.hit runs; a node with block waits until its run's
// context ends on its first attempt (a runner killed mid-run).
type hits struct {
	mu   sync.Mutex
	list []hit
	got  chan struct{}
}

func (h *hits) add(x hit) {
	h.mu.Lock()
	h.list = append(h.list, x)
	h.mu.Unlock()
	select {
	case h.got <- struct{}{}:
	default:
	}
}

func (h *hits) named(name string) []hit {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []hit
	for _, x := range h.list {
		if x.Name == name {
			out = append(out, x)
		}
	}
	return out
}

// wait waits until name has at least n hits.
func (h *hits) wait(t *testing.T, name string, n int, within time.Duration) []hit {
	t.Helper()
	deadline := time.After(within)
	for {
		if got := h.named(name); len(got) >= n {
			return got
		}
		select {
		case <-h.got:
		case <-time.After(50 * time.Millisecond):
		case <-deadline:
			t.Fatalf("%s: %d hits, want %d", name, len(h.named(name)), n)
		}
	}
}

// settings is a jobs section for the runners.
type settings struct{ s jobs.Settings }

func (s settings) Resolved(features.ServiceArgs, string) (json.RawMessage, error) { return json.Marshal(s.s) }

type fixture struct {
	t      *testing.T
	srv    *natsd.Server
	reg    *extensions.Registry
	bus    *bus.Bus
	subs   *subscribe.Subscriptions
	p      *project.Projections
	eng    *engine.Engine
	deps    jobs.Deps
	install *installs.Deps
	hits    *hits
	n      atomic.Int64
	runner *jobs.Runner // the nodes' runner: enqueues, serves nothing until served
}

func setup(t *testing.T, s jobs.Settings) *fixture {
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
	jobs.Contribute(reg, func() jobs.Deps { return jobDeps })
	jobsnodes.Contribute(reg, func() jobsnodes.Deps { return jobNodes })
	h := &hits{got: make(chan struct{}, 1)}
	contributeHit(reg, h)
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
	f := &fixture{t: t, srv: srv, reg: reg, bus: bus.New(p), subs: subscribe.New(p, reg), p: p, eng: eng, hits: h}
	nodeDeps = livenodes.Deps{Engine: eng, Bus: f.bus, Subscriptions: f.subs, Store: p}
	draftDeps = drafts.Deps{Engine: eng, Projections: p}
	dataDeps = data.Deps{Projections: p, Bus: f.bus}
	entityDeps = entities.Deps{Projections: p, DB: gdb, OnPublish: func(args features.ServiceArgs) error { return data.Follow(args, dataDeps) }}
	prefabDeps = prefabs.Deps{Projections: p, DB: gdb}
	appDeps = manifests.Deps{Projections: p, DB: gdb}
	installDep = installs.Deps{Projections: p, DB: gdb, Nodes: nodeDeps}
	f.bus.Resolve = installs.CommandFor(func() installs.Deps { return installDep }, livenodes.CommandFor(func() livenodes.Deps { return nodeDeps }))
	f.subs.Resolve = installs.QueryFor(func() installs.Deps { return installDep }, livenodes.QueryFor(func() livenodes.Deps { return nodeDeps }))
	jobDeps = jobs.Deps{Projections: p, Bus: f.bus, Engine: eng, Config: settings{s}}
	f.deps, f.install = jobDeps, &installDep
	// The nodes' runner stands for the process the graphs run in; the
	// runners a test starts are the processes running jobs
	f.runner = jobs.New(jobDeps, jobs.Options{Name: "nodes"})
	t.Cleanup(f.runner.Close)
	jobNodes = jobsnodes.Deps{Runner: f.runner, Store: p}
	return f
}

// proc is a process running jobs: its own projections, bus and runner
// over the shared JetStream.
type proc struct {
	r *jobs.Runner
	p *project.Projections
}

// Close stops the process: its runner, then its followers.
func (x *proc) Close() {
	x.r.Close()
	x.p.Close()
}

// start starts a process whose runner serves tenant 1.
func (f *fixture) start(name string, ackWait time.Duration) *proc {
	f.t.Helper()
	p := project.New(f.srv.JS(), f.reg)
	b := bus.New(p)
	b.Resolve = f.bus.Resolve
	d := f.deps
	d.Projections, d.Bus = p, b
	x := &proc{r: jobs.New(d, jobs.Options{Name: name, AckWait: ackWait, Discover: 200 * time.Millisecond}), p: p}
	f.t.Cleanup(x.Close)
	if err := x.r.Serve(features.LocalTenantID); err != nil {
		f.t.Fatal(err)
	}
	return x
}

// contributeHit registers test.hit: it records its run and, with block,
// waits on its first attempt until the run's context ends; with sleepMs it
// takes that long.
func contributeHit(reg *extensions.Registry, h *hits) {
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "test.hit", Source: "test"}, Title: "Hit", Category: "Test", Effects: true,
		Description: "Records that it ran, for tests", Example: "Hit a",
		Examples: cognition.ConfigExample("test.hit", `{"name":"a"}`),
		Config:   json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"sleepMs":{"type":"integer"},"block":{"type":"boolean"}},"required":["name"]}`),
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.ExecPin(cognition.Then, ""), {ID: "value", Name: "Value", Type: cognition.Any, Optional: true, Description: "Recorded with the hit"}},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, "")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct {
				Name    string
				SleepMs int
				Block   bool
			}
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			info, _ := jobs.InfoOf(x.Context())
			start := time.Now()
			if c.Block && info.Attempt == 1 {
				<-x.Context().Done()
				return nil, "", x.Context().Err()
			}
			if c.SleepMs > 0 {
				time.Sleep(time.Duration(c.SleepMs) * time.Millisecond)
			}
			h.add(hit{Name: c.Name, Job: info.ID, Attempt: info.Attempt, Principal: x.Principal, Value: in["value"], Start: start, End: time.Now()})
			return nil, cognition.Then, nil
		},
	})
}

func args() features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
}

func (f *fixture) do(typ, target string, a any) bus.Result {
	raw, _ := json.Marshal(a)
	return f.bus.Execute(bus.Request{ServiceArgs: args(), ID: fmt.Sprintf("c%d", f.n.Add(1)), Type: typ, Target: target, Args: raw})
}

func (f *fixture) ok(typ, target string, a any) bus.Result {
	f.t.Helper()
	res := f.do(typ, target, a)
	if res.Common.Code >= 300 {
		f.t.Fatalf("%s: %s %+v", typ, res.Common.Message, res.Problems)
	}
	return res
}

func (f *fixture) wait(projector, key string, seq uint64) live.Projected {
	f.t.Helper()
	v, err := f.p.WaitFor(context.Background(), features.LocalTenantID, projector, key, seq, 3*time.Second)
	if err != nil || v.Seq < seq {
		f.t.Fatalf("%s %s did not reach %d: %v", projector, key, seq, err)
	}
	return v
}

// job reads a job's state.
func (f *fixture) job(id string) jobs.Job {
	v, _, _ := f.p.Get(context.Background(), features.LocalTenantID, jobs.Projector, jobs.TargetType+"."+id)
	var j jobs.Job
	_ = json.Unmarshal(v.Value, &j)
	return j
}

// until waits until the job satisfies ok.
func (f *fixture) until(id, what string, within time.Duration, ok func(jobs.Job) bool) jobs.Job {
	f.t.Helper()
	deadline := time.Now().Add(within)
	for {
		j := f.job(id)
		if ok(j) {
			return j
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("job %s is not %s: %+v", id, what, j)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func status(s string) func(jobs.Job) bool { return func(j jobs.Job) bool { return j.Status == s } }

func node(id, typ string, config map[string]any) map[string]any {
	n := map[string]any{"id": id, "type": typ, "position": map[string]any{"x": 0, "y": 0}}
	if config != nil {
		n["config"] = config
	}
	return n
}

func edges(specs ...string) []map[string]any {
	var out []map[string]any
	for _, s := range specs {
		from, to, _ := strings.Cut(s, " -> ")
		fn, fp, _ := strings.Cut(from, ".")
		tn, tp, _ := strings.Cut(to, ".")
		out = append(out, map[string]any{"from": map[string]any{"node": fn, "pin": fp}, "to": map[string]any{"node": tn, "pin": tp}})
	}
	return out
}

// publishGraph imports a graph and publishes it.
func (f *fixture) publishGraph(id string, g map[string]any) {
	f.t.Helper()
	res := f.ok("bp.import", id, map[string]any{"graph": g})
	var d drafts.Draft
	_ = json.Unmarshal(f.wait(drafts.DraftProjector, "graph."+id, res.Seq).Value, &d)
	res = f.ok("bp.publish", id, map[string]any{"rev": d.Rev})
	f.wait(drafts.PublishedProjector, "graph."+id, res.Seq)
}

// action is a server.action graph that hits name with its input value.
func action(name string, hitConfig map[string]any, extra ...func(g map[string]any)) map[string]any {
	cfg := map[string]any{"name": name}
	for k, v := range hitConfig {
		cfg[k] = v
	}
	g := map[string]any{"name": "Work " + name, "kind": livenodes.KindAction,
		"nodes": []any{
			node("event", cognition.EventCall, map[string]any{"outputs": []any{map[string]any{"id": "value", "type": "json"}}}),
			node("hit", "test.hit", cfg),
		},
		"edges": edges("event.then -> hit.then", "event.value -> hit.value"),
	}
	for _, x := range extra {
		x(g)
	}
	return g
}

// enqueue enqueues a job as the local user through the nodes' runner.
func (f *fixture) enqueue(r jobs.Request) jobs.Job {
	f.t.Helper()
	j, err := f.runner.Enqueue(args(), r)
	if err != nil {
		f.t.Fatalf("enqueue %s: %v", r.ID, err)
	}
	return j
}
