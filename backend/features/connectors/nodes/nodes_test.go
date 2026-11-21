package connectorsnodes_test

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
	apptools "github.com/SgtFlame/foundation/backend/features/apps/tools"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	cognitionnodes "github.com/SgtFlame/foundation/backend/features/cognition/nodes"
	"github.com/SgtFlame/foundation/backend/features/connectors"
	"github.com/SgtFlame/foundation/backend/features/connectors/adapter"
	"github.com/SgtFlame/foundation/backend/features/connectors/connectorstest"
	connectorsnodes "github.com/SgtFlame/foundation/backend/features/connectors/nodes"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/jobs"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/records"
	"github.com/SgtFlame/foundation/backend/features/ui"
	uicomponents "github.com/SgtFlame/foundation/backend/features/ui/components"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
)

// hits records test.hit runs: the name and value.
type hits struct {
	mu   sync.Mutex
	list []hit
}

type hit struct {
	Name  string
	Value any
}

func (h *hits) add(x hit) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.list = append(h.list, x)
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

type settings struct{ s jobs.Settings }

func (s settings) Resolved(features.ServiceArgs, string) (json.RawMessage, error) { return json.Marshal(s.s) }

type fixture struct {
	t    *testing.T
	srv  *natsd.Server
	reg  *extensions.Registry
	p    *project.Projections
	bus  *bus.Bus
	eng  *engine.Engine
	cx   connectors.Deps
	hits *hits
	n    atomic.Int64
	run  *jobs.Runner
}

func setup(t *testing.T) *fixture {
	reg := extensions.NewRegistry()
	reg.Declare(connectors.Contracts, connectors.Types)
	cognitionnodes.Contribute(reg)
	uinodes.Contribute(reg)
	if err := uicomponents.Contribute(reg); err != nil {
		t.Fatal(err)
	}
	var (
		nodeDeps   livenodes.Deps
		draftDeps  drafts.Deps
		appDeps    manifests.Deps
		installDep installs.Deps
		jobDeps    jobs.Deps
		cxDeps     connectors.Deps
	)
	livenodes.Contribute(reg, func() livenodes.Deps { return nodeDeps })
	drafts.Contribute(reg, func() drafts.Deps { return draftDeps })
	manifests.Contribute(reg, func() manifests.Deps { return appDeps })
	installs.Contribute(reg, func() installs.Deps { return installDep })
	jobs.Contribute(reg, func() jobs.Deps { return jobDeps })
	connectorstest.Contribute(reg)
	connectorsnodes.Contribute(reg, func() connectors.Deps { return cxDeps })
	h := &hits{}
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor: extensions.Descriptor{ID: "test.hit", Source: "test"}, Title: "Hit", Category: "Test", Effects: true,
		Description: "Records that it ran, for tests", Example: "Hit a", Examples: cognition.ConfigExample("test.hit", `{"name":"a"}`),
		Config: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`),
		Pins: cognition.Pins{
			In:  []cognition.Pin{cognition.ExecPin(cognition.Then, ""), {ID: "value", Name: "Value", Type: cognition.Any, Optional: true, Description: "Recorded"}},
			Out: []cognition.Pin{cognition.ExecPin(cognition.Then, "")},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Name string }
			_ = x.Config(&c)
			h.add(hit{Name: c.Name, Value: in["value"]})
			return nil, cognition.Then, nil
		},
	})
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
	appDeps = manifests.Deps{Projections: p, DB: gdb}
	installDep = installs.Deps{Projections: p, DB: gdb, Nodes: nodeDeps}
	b.Resolve = installs.CommandFor(func() installs.Deps { return installDep }, livenodes.CommandFor(func() livenodes.Deps { return nodeDeps }))
	jobDeps = jobs.Deps{Projections: p, Bus: b, Engine: eng, Config: settings{jobs.Settings{MaxConcurrent: 4, MaxAttempts: 20, BackoffSeconds: 1, MaxBackoffSeconds: 1, DoneHours: 1}}}
	r := connectors.NewRegistry(reg, srv.JS())
	r.SetCheckEvery(50 * time.Millisecond)
	t.Cleanup(r.Close)
	cxDeps = connectors.Deps{Registry: r, Dispatcher: &connectors.Dispatcher{NC: srv.Conn(), Registry: r, Timeout: 2 * time.Second}}
	run := jobs.New(jobDeps, jobs.Options{Name: "runner", Discover: 200 * time.Millisecond})
	t.Cleanup(run.Close)
	if err := run.Serve(features.LocalTenantID); err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, srv: srv, reg: reg, p: p, bus: b, eng: eng, cx: cxDeps, hits: h, run: run}
}

func args() features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
}

func (f *fixture) ok(typ, target string, a any) bus.Result {
	f.t.Helper()
	raw, _ := json.Marshal(a)
	res := f.bus.Execute(bus.Request{ServiceArgs: args(), ID: fmt.Sprintf("c%d", f.n.Add(1)), Type: typ, Target: target, Args: raw})
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

func (f *fixture) publish(id string, g map[string]any) drafts.Draft {
	f.t.Helper()
	res := f.ok("bp.import", id, map[string]any{"graph": g})
	var d drafts.Draft
	_ = json.Unmarshal(f.wait(drafts.DraftProjector, "graph."+id, res.Seq).Value, &d)
	res = f.ok("bp.publish", id, map[string]any{"rev": d.Rev})
	f.wait(drafts.PublishedProjector, "graph."+id, res.Seq)
	return d
}

func (f *fixture) serve(name string) func() {
	f.t.Helper()
	nc, err := f.srv.Connect(name)
	if err != nil {
		f.t.Fatal(err)
	}
	o := connectorstest.Options(name, nil)
	o.AnnounceEvery, o.TTL = 100*time.Millisecond, 400*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = adapter.Serve(ctx, nc, o); close(done) }()
	stop := func() { cancel(); <-done; nc.Close() }
	f.t.Cleanup(stop)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if ti, ok, _ := f.cx.Registry.Type(context.Background(), 1, "test.echo"); ok && ti.Online {
			return stop
		}
		if time.Now().After(deadline) {
			f.t.Fatal("the adapter did not come online")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

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

// caller is a server.action graph calling op with the input text (echo)
// or code (fail), hitting ok with the result's text, or err with the
// error.
func caller(op string, extra map[string]any) map[string]any {
	cfg := map[string]any{"contract": "test.echo@1", "operation": op}
	for k, v := range extra {
		cfg[k] = v
	}
	input, field, result := "text", "text", "call.text -> ok.value"
	if op == "fail" {
		input, field, result = "code", "code", "call.then -> ok.then"
	}
	es := []string{"event.then -> call.then", "event." + input + " -> call." + field, "call.failed -> err.then", "call.error -> err.value"}
	if op != "fail" {
		es = append(es, "call.then -> ok.then")
	}
	es = append(es, result)
	return map[string]any{"name": "Call " + op, "kind": livenodes.KindAction,
		"nodes": []any{
			node("event", cognition.EventCall, map[string]any{"outputs": []any{map[string]any{"id": input, "type": "string"}}}),
			node("call", connectorsnodes.Call, cfg),
			node("ok", "test.hit", map[string]any{"name": "ok"}),
			node("err", "test.hit", map[string]any{"name": "err"}),
		},
		"edges": edges(es...),
	}
}

func (f *fixture) job(id string) jobs.Job {
	v, _, _ := f.p.Get(context.Background(), features.LocalTenantID, jobs.Projector, jobs.TargetType+"."+id)
	var j jobs.Job
	_ = json.Unmarshal(v.Value, &j)
	return j
}

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

func (f *fixture) enqueue(id, graph string, inputs any) {
	f.t.Helper()
	raw, _ := json.Marshal(inputs)
	if _, err := f.run.Enqueue(args(), jobs.Request{ID: id, Graph: graph, Inputs: raw}); err != nil {
		f.t.Fatal(err)
	}
}

// Call connector's pins follow the operation's schemas: one input per
// top-level arg (a nested object is one json pin), one output per result
// field, typed, with Error and the optional connection.
func TestCallPins(t *testing.T) {
	f := setup(t)
	d := f.publish("call-echo", caller("echo", nil))
	n, _ := d.Graph.NodeByID("call")
	var c connectorsnodes.CallConfig
	_ = json.Unmarshal(n.Config, &c)
	if c.Contract != "test.echo@1" || c.Effect != "read" || c.Sample["text"] != "hihi" {
		t.Errorf("normalized: %+v", c)
	}
	t0, _ := cognition.CatalogOf(f.reg).Type(connectorsnodes.Call)
	pins, err := t0.PinsOf(n.Config)
	if err != nil {
		t.Fatal(err)
	}
	ids := func(ps []cognition.Pin) string {
		var out []string
		for _, p := range ps {
			s := p.ID
			if !p.Exec {
				s += ":" + p.Type
			}
			if p.Optional {
				s += "?"
			}
			out = append(out, s)
		}
		return strings.Join(out, " ")
	}
	if got := ids(pins.In); got != "then connection:string? text:string repeat:number? options:json?" {
		t.Errorf("inputs: %s", got)
	}
	if got := ids(pins.Out); got != "then failed text:string length:number by:string error:json" {
		t.Errorf("outputs: %s", got)
	}
	// An unknown operation is refused when the node is added
	raw, _ := json.Marshal(map[string]any{"node": "call", "config": map[string]any{"contract": "test.echo@1", "operation": "nope"}})
	res := f.bus.Execute(bus.Request{ServiceArgs: args(), ID: "c-bad", Type: "bp.setConfig", Target: "call-echo", Args: raw})
	if res.Common.Code < 300 {
		t.Error("an unknown operation was accepted")
	}
}

// In a job: a call returns its result to the next nodes; a typed error
// that is not retryable leaves by Error and the job is done; a retryable
// one with no adapter makes the job retry, until an adapter starts.
func TestCallInJobs(t *testing.T) {
	f := setup(t)
	f.publish("call-echo", caller("echo", nil))
	f.publish("call-fail", caller("fail", nil))
	f.enqueue("retry-1", "call-echo", map[string]any{"text": "later"})
	f.until("retry-1", "retrying", 5*time.Second, func(j jobs.Job) bool { return j.Status == jobs.StatusRetrying })
	if len(f.hits.named("err")) != 0 {
		t.Fatalf("a retryable error took Error: %v", f.hits.named("err"))
	}
	f.serve("a")
	j := f.until("retry-1", "done", 10*time.Second, func(j jobs.Job) bool { return j.Status == jobs.StatusDone })
	if j.Attempts < 2 {
		t.Errorf("attempts %d", j.Attempts)
	}
	if got := f.hits.named("ok"); len(got) != 1 || got[0].Value != "later" {
		t.Errorf("ok after the retry: %v", got)
	}
	f.enqueue("echo-1", "call-echo", map[string]any{"text": "hi"})
	f.until("echo-1", "done", 5*time.Second, func(j jobs.Job) bool { return j.Status == jobs.StatusDone })
	if got := f.hits.named("ok"); len(got) != 2 || got[1].Value != "hi" {
		t.Errorf("ok: %v", got)
	}
	f.enqueue("fail-1", "call-fail", map[string]any{"code": "notFound"})
	j = f.until("fail-1", "done", 5*time.Second, func(j jobs.Job) bool { return j.Finished() })
	errs := f.hits.named("err")
	if j.Status != jobs.StatusDone || j.Attempts != 1 || len(errs) != 1 || errs[0].Value.(map[string]any)["code"] != "notFound" {
		t.Errorf("notFound: %+v, err hits %v", j, errs)
	}
	f.enqueue("fail-2", "call-fail", map[string]any{"code": "rateLimited"})
	f.until("fail-2", "retrying", 5*time.Second, func(j jobs.Job) bool { return j.Status == jobs.StatusRetrying })
	_ = f.run.Cancel(args(), "fail-2")
}

// forge.test stubs Call connector with the operation's result example,
// with no adapter running.
func TestForgeTestStubs(t *testing.T) {
	f := setup(t)
	f.publish("call-echo", caller("echo", nil))
	res, err := apptools.Test(args(), f.reg, apptools.ForgeDeps{Store: f.p, Engine: f.eng, Bus: f.bus}, apptools.TestArgs{Graph: "call-echo", Inputs: map[string]any{"text": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(res.Stubbed, "call") {
		t.Errorf("stubbed: %v (failed %+v)", res.Stubbed, res.Failed)
	}
	if got := f.hits.named("ok"); len(got) != 1 || got[0].Value != "hihi" {
		t.Errorf("the stub's result: %v", got)
	}
}

// A discovered contract (conn.<id>) gives its pins too, with no
// connection input: the contract names its connection.
func TestDiscoveredPins(t *testing.T) {
	f := setup(t)
	disc := connectorstest.Contract()
	disc.Events = nil
	if _, err := connectors.PutDiscovered(context.Background(), f.srv.JS(), 1, "77", disc); err != nil {
		t.Fatal(err)
	}
	g := caller("echo", map[string]any{"contract": "conn.77"})
	d := f.publish("call-disc", g)
	n, _ := d.Graph.NodeByID("call")
	t0, _ := cognition.CatalogOf(f.reg).Type(connectorsnodes.Call)
	pins, _ := t0.PinsOf(n.Config)
	if _, ok := pins.Find("connection", true); ok {
		t.Error("a discovered contract's call has a connection input")
	}
	if _, ok := pins.Find("text", true); !ok {
		t.Errorf("pins: %+v", pins)
	}
}
