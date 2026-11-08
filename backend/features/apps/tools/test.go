package apptools

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/apps/manifests"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/drafts"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	livenodes "github.com/SgtFlame/foundation/backend/features/live/nodes"
	"github.com/SgtFlame/foundation/backend/features/tools"
	uinodes "github.com/SgtFlame/foundation/backend/features/ui/nodes"
)

// TestArgs are forge.test's arguments (forge 06f).
type TestArgs struct {
	Graph string `json:"graph"`
	// Inputs by kind: a command's {target, args}, a query's {params}, a
	// server action's call inputs, a handler's event fields with {state, props}
	// (a logic's too); records, in any of them, seeds the sandbox.
	Inputs map[string]any `json:"inputs,omitempty"`
	Event  string         `json:"event,omitempty"` // a prefab.logic graph's lifecycle event; the graph's first event when empty
	As     string         `json:"as,omitempty"`    // a role of the graph's app
}

// TestEvent is an event a sandboxed run's commands would commit.
type TestEvent struct {
	Type   string      `json:"type"`
	Target live.Target `json:"target"`
	Data   any         `json:"data,omitempty"`
}

// TraceStep is one node a sandboxed run ran, its values cut for a model.
type TraceStep struct {
	Node    string `json:"node"`
	Type    string `json:"type"`
	Title   string `json:"title,omitempty"`
	Context string `json:"context,omitempty"` // the loop frames it ran in
	Inputs  any    `json:"inputs,omitempty"`
	Outputs any    `json:"outputs,omitempty"`
	Pin     string `json:"pin,omitempty"` // the exec pin it left by
	Error   string `json:"error,omitempty"`
	Stubbed bool   `json:"stubbed,omitempty"` // an External node, given its sample
}

// TestFailure is the node a failed run failed on, with what it was given.
type TestFailure struct {
	Node   string `json:"node"`
	Type   string `json:"type,omitempty"`
	Title  string `json:"title,omitempty"`
	Inputs any    `json:"inputs,omitempty"`
	Error  string `json:"error"`
}

// TestResult is what forge.test gives.
type TestResult struct {
	Graph    string           `json:"graph"`
	Kind     string           `json:"kind"`
	Outputs  cognition.Values `json:"outputs,omitempty"`
	Result   any              `json:"result,omitempty"` // a data.query graph's
	Events   []TestEvent      `json:"events"`           // what its commands would commit
	Commands []bus.DryCommand `json:"commands"`         // the commands it decided, the command graph's own first
	Problems []live.Problem   `json:"problems"`         // why its command, or one it sent, was refused
	Report   any              `json:"report,omitempty"` // a handler's or logic's view-state changes, actions, raised events, subscriptions
	Trace    []TraceStep      `json:"trace"`
	Cut      int              `json:"traceCut,omitempty"` // steps left out of the trace
	Stubbed  []string         `json:"stubbed,omitempty"`  // External nodes not run
	Failed   *TestFailure     `json:"failed,omitempty"`
	Seeded   []string         `json:"seeded,omitempty"` // the ids of the records the inputs seeded
	As       string           `json:"as,omitempty"`
	runID    uint64
}

// Trace limits: a model gets the first steps, each value cut to a preview.
const (
	traceSteps = 50
	traceValue = 800
)

// testable are the kinds forge.test runs.
var testable = []string{livenodes.KindCommand, livenodes.KindQuery, livenodes.KindAction, uinodes.KindHandler, uinodes.KindPrefab}

// testGraph is a graph's draft, else its published or built-in version;
// the drafts are read once they have folded what was committed.
func testGraph(args features.ServiceArgs, d ForgeDeps, id string) (cognition.Graph, bool) {
	if err := d.Store.Settle(args.Context(), args.Principal.TenantID, drafts.DraftProjector); err != nil {
		return cognition.Graph{}, false
	}
	v, ok, err := d.Store.Get(args.Context(), args.Principal.TenantID, drafts.DraftProjector, drafts.TargetType+"."+id)
	if err == nil && ok && v.Value != nil {
		var dr drafts.Draft
		if json.Unmarshal(v.Value, &dr) == nil {
			return dr.Graph, true
		}
	}
	return d.Engine.Graph(args, id)
}

// Test runs a graph in a sandbox (forge 06f): a dry run whose commands are
// decided against the records with the run's own events over them, kept
// in memory and dropped at the end. Nothing reaches the stream or the
// projections; External nodes are stubbed.
func Test(args features.ServiceArgs, reg *extensions.Registry, d ForgeDeps, a TestArgs) (TestResult, error) {
	res := drafts.Resource(args.Principal, a.Graph)
	if err := authz.Check(args.Context(), args.Principal, "cognition.execute", res).Err("cognition.execute", res); err != nil {
		return TestResult{}, err
	}
	g, ok := testGraph(args, d, a.Graph)
	if !ok {
		return TestResult{}, fmt.Errorf("there is no graph %q; list them with bp.list", a.Graph)
	}
	if !contains(testable, g.Kind) {
		return TestResult{}, fmt.Errorf("forge.test runs %s graphs, not %s: test it with bp.test", strings.Join(testable, ", "), g.Kind)
	}
	out := TestResult{Graph: a.Graph, Kind: g.Kind, Events: []TestEvent{}, Commands: []bus.DryCommand{}, Problems: []live.Problem{}, Trace: []TraceStep{}, As: a.As}
	inputs := cognition.Values{}
	for k, v := range a.Inputs {
		if k != "records" {
			inputs[k] = v
		}
	}

	dry := d.Bus.Dry()
	args.Ctx = bus.WithDry(args.Context(), dry)
	// The seeds are the user's, made before the run takes a role
	seeds, err := seedsOf(a.Inputs["records"])
	if err != nil {
		return TestResult{}, err
	}
	for i, s := range seeds {
		raw, _ := json.Marshal(map[string]any{"entity": s.Entity, "values": s.Values})
		r := dry.Execute(bus.Request{ServiceArgs: args, Type: "record.create", Target: s.ID, Args: raw})
		if r.Common.Code >= 300 {
			msg := r.Common.Message
			if len(r.Problems) > 0 {
				msg = r.Problems[0].Message
			}
			return TestResult{}, fmt.Errorf("inputs.records.%d cannot be made: %s", i, msg)
		}
		out.Seeded = append(out.Seeded, r.Target)
	}
	before, sent := len(dry.Events()), len(dry.Commands())

	// An app's graph runs as its draft run: its commands are the draft's
	app, owned := strings.CutPrefix(g.Owner, "app:")
	dr, hasDraft := manifests.GetDraft(args.Context(), d.Store, args.Principal.TenantID, app)
	if owned && hasDraft {
		dry.Names = map[string]string{}
		for _, c := range dr.Manifest.Commands {
			dry.Names[c.Type] = apps.DraftScoped(app, c.Type)
		}
	}
	if a.As != "" {
		if !owned || !hasDraft {
			return TestResult{}, fmt.Errorf("%s belongs to no app draft, so it has no roles to test as", a.Graph)
		}
		if args, err = roleArgs(args, reg, d, dr.Manifest, app, a.As); err != nil {
			return TestResult{}, err
		}
	}

	cat := cognition.CatalogOf(reg)
	event := a.Event
	if event == "" {
		for _, n := range g.Nodes {
			if t, ok := cat.Type(n.Type); ok && t.Event {
				event = t.ID
				break
			}
		}
	}
	var run engine.Result
	var runErr error
	var report func() any
	switch g.Kind {
	case livenodes.KindCommand:
		o := &livenodes.Observer{Tune: func(ra *engine.RunArgs) { ra.Stub, ra.Steps = true, true }}
		cmd, err := livenodes.CommandOf(livenodes.Deps{Engine: d.Engine, Bus: d.Bus}, g.ID, &g, o)
		if err != nil {
			return TestResult{}, err
		}
		target, _ := inputs["target"].(string)
		if target == "" {
			target = strconv.FormatUint(features.NewTSID(), 10)
		}
		cmdArgs := inputs["args"]
		if cmdArgs == nil {
			cmdArgs = map[string]any{}
		}
		raw, _ := json.Marshal(cmdArgs)
		dry.Decide(bus.Request{ServiceArgs: args, Type: cmd.ID, Target: target, Args: raw}, cmd)
		run, runErr = o.Result, o.Err
	default:
		ra := engine.RunArgs{ServiceArgs: args, Graph: &g, Event: event, Inputs: inputs, Trigger: "forge.test", Trace: engine.TraceFailures, Stub: true, Steps: true}
		switch g.Kind {
		case livenodes.KindQuery:
			params, _ := inputs["params"].(map[string]any)
			in := cognition.Values{"params": map[string]any{}}
			for k, v := range params {
				in[k] = v
			}
			if params != nil {
				in["params"] = params
			}
			ra.Inputs, ra.Host = in, &livenodes.QueryHost{Pos: live.Position{}}
		case livenodes.KindAction:
			// No host: Send command goes to the dry run, as the user
		default:
			if k, ok := cat.Kind(g.Kind); ok && k.Host != nil {
				ra.Host, report = k.Host(args, g, inputs)
			}
		}
		run, runErr = d.Engine.Run(ra)
		out.Outputs = run.Outputs
		if report != nil {
			out.Report = report()
		}
		if g.Kind == livenodes.KindQuery {
			out.Result = run.Outputs["result"]
		}
	}

	for _, e := range dry.Events()[before:] {
		var data any
		_ = json.Unmarshal(e.Data, &data)
		out.Events = append(out.Events, TestEvent{Type: e.Type, Target: e.Target, Data: data})
	}
	for _, c := range dry.Commands()[sent:] {
		out.Commands = append(out.Commands, c)
		out.Problems = append(out.Problems, c.Problems...)
	}
	out.trace(g, cat, run)
	if runErr != nil {
		out.fail(g, cat, run, runErr)
	}
	return out, nil
}

// seed is one record forge.test makes in the sandbox before the run.
type seed struct {
	Entity string         `json:"entity"`
	ID     string         `json:"id,omitempty"`
	Values map[string]any `json:"values"`
}

func seedsOf(v any) ([]seed, error) {
	if v == nil {
		return nil, nil
	}
	raw, _ := json.Marshal(v)
	var out []seed
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("inputs.records must be a list of {entity, id?, values}: %v", err)
	}
	return out, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// cut is a captured value for a model: as it is when short, else a
// preview of it.
func cut(captured string) any {
	if captured == "" {
		return nil
	}
	if len(captured) > traceValue {
		return map[string]any{"$cut": len(captured), "preview": captured[:traceValue]}
	}
	var v any
	if json.Unmarshal([]byte(captured), &v) != nil {
		return captured
	}
	return v
}

func titleOf(g cognition.Graph, cat cognition.Catalog, id string) (typ, title string) {
	n, ok := g.NodeByID(id)
	if !ok {
		return "", ""
	}
	title = n.Title
	if t, ok := cat.Type(n.Type); ok && title == "" {
		title = t.Title
	}
	return n.Type, title
}

func (out *TestResult) trace(g cognition.Graph, cat cognition.Catalog, run engine.Result) {
	out.runID = run.RunID
	stubbed := map[string]bool{}
	for _, id := range run.Stubbed {
		stubbed[id] = true
		out.Stubbed = append(out.Stubbed, id)
	}
	for i, s := range run.Steps {
		if i >= traceSteps {
			out.Cut = len(run.Steps) - traceSteps
			break
		}
		_, title := titleOf(g, cat, s.NodeID)
		out.Trace = append(out.Trace, TraceStep{Node: s.NodeID, Type: s.NodeType, Title: title, Context: s.Context,
			Inputs: cut(s.Inputs), Outputs: cut(s.Outputs), Pin: s.ExecPin, Error: s.Error, Stubbed: stubbed[s.NodeID]})
	}
}

// fail names the node the run failed on, with its inputs: the step that
// failed, else the run's failed node.
func (out *TestResult) fail(g cognition.Graph, cat cognition.Catalog, run engine.Result, err error) {
	f := &TestFailure{Node: run.FailedNode, Error: err.Error()}
	for i := len(run.Steps) - 1; i >= 0; i-- {
		if s := run.Steps[i]; s.Error != "" {
			f.Node, f.Inputs, f.Error = s.NodeID, cut(s.Inputs), s.Error
			break
		}
	}
	f.Type, f.Title = titleOf(g, cat, f.Node)
	out.Failed = f
}

// ContributeTest registers forge.test (forge 06f).
func ContributeTest(reg *extensions.Registry, deps func() ForgeDeps) {
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "forge.test", Source: "apps"}, Title: "Test a blueprint", Effect: tools.EffectWrite,
		Asks:        func(json.RawMessage) bool { return false },
		Description: "Runs a blueprint (a graph's draft) once in a sandbox and says what it would do, without changing anything: reads see the app's records with the run's own writes over them, its commands are decided but never committed, and the sandbox is dropped afterwards. Graph kinds and their inputs: command {target, args} (the command it defines, sent to target), data.query {params} (result is its rows), server.action (its On call inputs), ui.handler (its On event's fields, with state and props: the actions it returns and the commands it sends, as the Prefab Editor's Interact runs it) and prefab.logic {state, props} with event (ui.onMount, ui.onPropsChanged, ui.onQueryChanged, ui.onUnmount). inputs.records [{entity, id?, values}] makes records in the sandbox first. as runs it as one of the graph's app's roles. Nodes that reach outside (a model, a file upload) are not run: they give a sample and are listed in stubbed. Gives the outputs, the events its commands would commit, each command it decided with its problems, a trace of the nodes run with their inputs and outputs, and the node it failed on with its inputs.",
		Input: json.RawMessage(`{"type":"object","properties":{
  "graph":{"type":"string","description":"The graph's id, e.g. cmd-tasks-task-reassign"},
  "inputs":{"type":"object","description":"What starts it, by kind (see the description); records seeds the sandbox: [{entity, id?, values}]"},
  "event":{"type":"string","description":"A prefab.logic graph's event node type; the graph's first event when left out"},
  "as":{"type":"string","description":"The id of a role of the app that owns the graph, e.g. member"}},"required":["graph"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[TestArgs](call)
			if err != nil {
				return tools.Result{}, err
			}
			out, err := Test(call.ServiceArgs, reg, deps(), a)
			if err != nil {
				return tools.Result{}, err
			}
			raw, _ := json.Marshal(out)
			var content map[string]any
			_ = json.Unmarshal(raw, &content)
			summary := fmt.Sprintf("Tested %s: %d events", a.Graph, len(out.Events))
			if len(out.Problems) > 0 {
				summary += fmt.Sprintf(", %d problems", len(out.Problems))
			}
			if out.Failed != nil {
				summary += ", failed on " + out.Failed.Node
			}
			return tools.Result{Content: content, Summary: summary}, nil
		},
	})
}
