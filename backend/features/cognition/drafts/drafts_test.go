package drafts_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	cognitionnodes "github.com/SgtFlame/foundation/backend/features/cognition/nodes"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/natsd"
	"github.com/SgtFlame/foundation/backend/features/live/project"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
)

type fixture struct {
	t      *testing.T
	bus    *bus.Bus
	subs   *subscribe.Subscriptions
	engine *engine.Engine
	p      *project.Projections
	n      int
	seq    map[string]uint64 // each graph's last commit
}

// echo is a built-in, editable function graph: On call(text) → Return(out = text).
var echo = cognition.Graph{Name: "Echo", Kind: cognition.KindFunction, Version: 1,
	Nodes: []cognition.Node{
		{ID: "on", Type: cognition.EventCall, Config: json.RawMessage(`{"outputs":[{"id":"text","type":"string","description":"what to echo"}]}`)},
		{ID: "ret", Type: "flow.return", Config: json.RawMessage(`{"outputs":[{"id":"out","type":"string"}]}`)},
	},
	Edges: []cognition.Edge{{From: cognition.PinRef{Node: "on", Pin: "then"}, To: cognition.PinRef{Node: "ret", Pin: "then"}},
		{From: cognition.PinRef{Node: "on", Pin: "text"}, To: cognition.PinRef{Node: "ret", Pin: "out"}}},
}

func setup(t *testing.T) *fixture {
	reg := extensions.NewRegistry()
	cognitionnodes.Contribute(reg)
	cognition.Graphs.Register(reg, cognition.BuiltIn{Descriptor: extensions.Descriptor{ID: "echo"}, Graph: echo, Editable: true})
	f := &fixture{t: t, seq: map[string]uint64{}}
	var deps drafts.Deps
	drafts.Contribute(reg, func() drafts.Deps { return deps })
	gdb, err := db.Open(db.WithPath(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb, cognition.Models()...); err != nil {
		t.Fatal(err)
	}
	srv := natsd.StartTest(t)
	p := project.New(srv.JS(), reg)
	t.Cleanup(p.Close)
	f.engine = engine.New(gdb, reg, nil)
	f.engine.SetSource(drafts.Source{Store: p})
	deps = drafts.Deps{Engine: f.engine, Projections: p}
	f.bus, f.subs, f.p = bus.New(p), subscribe.New(p, reg), p
	return f
}

func args() features.ServiceArgs {
	return features.ServiceArgs{Ctx: context.Background(), Principal: features.LocalPrincipal()}
}

// do sends a command and returns the result.
func (f *fixture) do(typ, target string, a any) bus.Result {
	f.n++
	raw, _ := json.Marshal(a)
	res := f.bus.Execute(bus.Request{ServiceArgs: args(), ID: fmt.Sprintf("c%d", f.n), Type: typ, Target: target, Args: raw})
	if res.Common.Code < 300 {
		f.seq[target] = max(f.seq[target], res.Seq)
	}
	return res
}

func (f *fixture) ok(typ, target string, a any) {
	f.t.Helper()
	if res := f.do(typ, target, a); res.Common.Code >= 300 {
		f.t.Fatalf("%s: %s %+v", typ, res.Common.Message, res.Problems)
	}
}

// published waits until a graph's last commit is in the published projection.
func (f *fixture) published(id string) {
	f.t.Helper()
	if _, err := f.p.WaitFor(context.Background(), features.LocalTenantID, drafts.PublishedProjector, "graph."+id, f.seq[id], 2*time.Second); err != nil {
		f.t.Fatal(err)
	}
}

// view reads a graph's draft once its last commit is projected.
func (f *fixture) view(id string) drafts.View {
	f.t.Helper()
	if _, err := f.p.WaitFor(context.Background(), features.LocalTenantID, drafts.DraftProjector, "graph."+id, f.seq[id], 2*time.Second); err != nil {
		f.t.Fatal(err)
	}
	data, _, fail := f.subs.Once(args(), "cognition.graph", json.RawMessage(`{"id":"`+id+`"}`))
	if fail != nil {
		f.t.Fatal(fail.Message)
	}
	raw, _ := json.Marshal(data)
	var v drafts.View
	_ = json.Unmarshal(raw, &v)
	return v
}

func pin(node, p string) map[string]string { return map[string]string{"node": node, "pin": p} }

func TestADraftIsEditedByCommandsAndPublished(t *testing.T) {
	f := setup(t)
	f.ok("bp.create", "greet", map[string]any{"kind": "function", "name": "Greet"})
	f.ok("bp.setConfig", "greet", map[string]any{"node": "event", "config": map[string]any{"outputs": []any{map[string]any{"id": "name", "type": "string", "description": "who"}}}})
	f.ok("bp.addNode", "greet", map[string]any{"type": "flow.return", "id": "ret", "config": map[string]any{"outputs": []any{map[string]any{"id": "out", "type": "string"}}}})
	f.ok("bp.connect", "greet", map[string]any{"from": pin("event", "then"), "to": pin("ret", "then")})

	// A wrong connection is refused with a fix a model can act on
	res := f.do("bp.connect", "greet", map[string]any{"from": pin("event", "then"), "to": pin("ret", "out")})
	if res.Common.Code != 422 || !strings.Contains(res.Problems[0].Message, "exec pin") || res.Problems[0].Fix == "" {
		t.Errorf("exec → data was not refused with a fix: %d %+v", res.Common.Code, res.Problems)
	}
	f.ok("bp.connect", "greet", map[string]any{"from": pin("event", "name"), "to": pin("ret", "out")})

	v := f.view("greet")
	if v.Rev != 5 || len(v.Graph.Nodes) != 2 || len(v.Graph.Edges) != 2 || len(v.Problems) != 0 {
		t.Fatalf("view = rev %d, %d nodes, %d edges, problems %+v", v.Rev, len(v.Graph.Nodes), len(v.Graph.Edges), v.Problems)
	}

	// Publishing what was not seen is refused
	if res := f.do("bp.publish", "greet", map[string]any{"rev": 4}); res.Common.Code != 422 || !strings.Contains(res.Problems[0].Message, "rev 5") {
		t.Errorf("a stale publish was not refused: %+v", res.Problems)
	}
	f.ok("bp.publish", "greet", map[string]any{"rev": 5})
	f.published("greet")
	g, ok := f.engine.Graph(args(), "greet")
	if !ok || g.Version != 1 {
		t.Fatalf("published graph = %v %+v", ok, g)
	}
	run, err := f.engine.Run(engine.RunArgs{ServiceArgs: args(), GraphID: "greet", Event: cognition.EventCall, Inputs: cognition.Values{"name": "Ada"}})
	if err != nil || run.Outputs["out"] != "Ada" {
		t.Errorf("run = %v, %v", run.Outputs, err)
	}

	// Removing a node removes its edges; a paste puts them back
	f.ok("bp.remove", "greet", map[string]any{"nodes": []string{"ret"}})
	if v := f.view("greet"); len(v.Graph.Edges) != 0 || len(v.Graph.Nodes) != 1 {
		t.Errorf("after remove: %d nodes, %d edges", len(v.Graph.Nodes), len(v.Graph.Edges))
	}
	f.ok("bp.paste", "greet", map[string]any{"nodes": []any{map[string]any{"id": "ret", "type": "flow.return", "position": map[string]int{"x": 1, "y": 2},
		"config": map[string]any{"outputs": []any{map[string]any{"id": "out", "type": "string"}}}}},
		"edges": []any{map[string]any{"from": pin("event", "then"), "to": pin("ret", "then")}}})
	if v := f.view("greet"); len(v.Graph.Edges) != 1 || v.Published != 1 {
		t.Errorf("after paste: %d edges, published %d", len(v.Graph.Edges), v.Published)
	}
}

func TestAnEditableBuiltInBecomesTheTenantsDraft(t *testing.T) {
	f := setup(t)
	if v := f.view("echo"); !v.BuiltIn || v.Rev != 0 || v.ReadOnly {
		t.Fatalf("built-in view = %+v", v)
	}
	f.ok("bp.setInfo", "echo", map[string]any{"description": "Says it back"})
	v := f.view("echo")
	if v.Rev != 2 || v.Graph.Description != "Says it back" || len(v.Graph.Nodes) != 2 || v.Published != 1 {
		t.Fatalf("draft from built-in = rev %d %+v", v.Rev, v.Graph)
	}
	f.ok("bp.publish", "echo", map[string]any{"rev": 2})
	f.published("echo")
	if g, _ := f.engine.Graph(args(), "echo"); g.Version != 2 || g.Description != "Says it back" {
		t.Errorf("the tenant's version does not replace the built-in: %+v", g)
	}

	// Call graph takes the callee's signature into its config
	f.ok("bp.create", "caller", map[string]any{"kind": "function", "name": "Caller"})
	f.ok("bp.addNode", "caller", map[string]any{"type": "flow.callGraph", "id": "call", "config": map[string]any{"graph": "echo"}})
	v = f.view("caller")
	pins := v.Pins["call"]
	if len(pins.In) != 2 || pins.In[1].ID != "text" || len(pins.Out) != 2 || pins.Out[1].ID != "out" {
		t.Errorf("Call graph's pins = %+v", pins)
	}

	// A copy keeps the nodes under a new id
	f.ok("bp.create", "echo2", map[string]any{"from": "echo"})
	if v := f.view("echo2"); v.Graph.Name != "Copy of Echo" || len(v.Graph.Nodes) != 2 || v.BuiltIn {
		t.Errorf("copy = %+v", v.Graph)
	}
	f.view("echo2")
	var list map[string]drafts.Summary
	data, _, _ := f.subs.Once(args(), "cognition.graphs", json.RawMessage(`{}`))
	raw, _ := json.Marshal(data)
	_ = json.Unmarshal(raw, &list)
	if len(list) != 3 || !list["graph.echo"].BuiltIn || list["graph.echo"].Published != 2 {
		t.Errorf("graphs = %+v", list)
	}
}

func TestTestRunsTheDraft(t *testing.T) {
	f := setup(t)
	f.ok("bp.setInfo", "echo", map[string]any{"name": "Echo"})
	f.view("echo")
	res, fail := drafts.Test(args(), drafts.Deps{Engine: f.engine, Projections: f.p}, drafts.TestArgs{Graph: "echo", Inputs: cognition.Values{"text": "hi"}})
	if fail != nil || res.Error != "" || res.Outputs["out"] != "hi" || res.RunID == 0 {
		t.Errorf("test = %+v %v", res, fail)
	}
}
