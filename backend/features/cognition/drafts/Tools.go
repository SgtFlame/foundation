package drafts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// TestArgs run a draft once with sample inputs (bp.test).
type TestArgs struct {
	Graph  string           `json:"graph"`
	Event  string           `json:"event,omitempty"` // the event node type; the graph's first event when empty
	Inputs cognition.Values `json:"inputs,omitempty"`
}

// TestResult is what a test run did.
type TestResult struct {
	RunID   uint64           `json:"runId,string"`
	Outputs cognition.Values `json:"outputs"`
	// Report is what the nodes did to their host, as the kind tells it:
	// a handler's view-state changes, actions and the commands it would
	// send; a command graph's events. Test hosts do not send commands.
	Report any    `json:"report,omitempty"`
	Error  string `json:"error,omitempty"`
	NodeID string `json:"nodeId,omitempty"` // the node it failed on
}

// Test runs a graph's draft (or built-in) once, traced, with the kind's
// test host.
func Test(args features.ServiceArgs, deps Deps, a TestArgs) (TestResult, *features.ServiceResponse) {
	res := Resource(args.Principal, a.Graph)
	if err := authz.Check(args.Context(), args.Principal, "cognition.execute", res).Err("cognition.execute", res); err != nil {
		return TestResult{}, args.Fail(authz.Status, err)
	}
	g, ok := draftOrBuiltIn(args, deps, a.Graph)
	if !ok {
		return TestResult{}, args.Fail(http.StatusNotFound, log.Code("graph_not_found").With("graphId", a.Graph).Errorf("there is no graph %q", a.Graph))
	}
	cat := deps.Engine.Catalog()
	if a.Event == "" {
		for _, n := range g.Nodes {
			if t, ok := cat.Type(n.Type); ok && t.Event {
				a.Event = t.ID
				break
			}
		}
	}
	var host any
	var report func() any
	if k, ok := cat.Kind(g.Kind); ok && k.Host != nil {
		host, report = k.Host(args, g, a.Inputs)
	}
	run, err := deps.Engine.Run(engine.RunArgs{ServiceArgs: args, Graph: &g, Event: a.Event, Inputs: a.Inputs, Trigger: "test", Host: host})
	out := TestResult{RunID: run.RunID, Outputs: run.Outputs}
	if out.Outputs == nil {
		out.Outputs = cognition.Values{}
	}
	if report != nil {
		out.Report = report()
	}
	if err != nil {
		out.Error = err.Error()
		var rec cognition.Run
		if run.RunID != 0 {
			out.NodeID = failedNode(deps, run.RunID, &rec)
		}
	}
	return out, nil
}

func failedNode(deps Deps, runID uint64, rec *cognition.Run) string {
	if db := deps.Engine.DB(); db != nil && db.First(rec, runID).Error == nil {
		return rec.FailedNode
	}
	return ""
}

// draftOrBuiltIn is a graph's draft, else its built-in version.
func draftOrBuiltIn(args features.ServiceArgs, deps Deps, id string) (cognition.Graph, bool) {
	v, ok, err := deps.Projections.Get(args.Context(), args.Principal.TenantID, DraftProjector, TargetType+"."+id)
	if err == nil && ok && v.Value != nil {
		var d Draft
		if json.Unmarshal(v.Value, &d) == nil {
			return d.Graph, true
		}
	}
	b, ok := deps.Engine.BuiltIn(id)
	return b.Graph, ok
}

// NodeTypeInfo is a node type as the catalog tools describe it.
type NodeTypeInfo struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Category    string          `json:"category"`
	Description string          `json:"description"`
	Example     string          `json:"example"`
	Event       bool            `json:"event,omitempty"`
	Effects     bool            `json:"effects,omitempty"`
	Pins        *cognition.Pins `json:"pins,omitempty"`   // the fixed pins; with a config they may differ
	Config      json.RawMessage `json:"config,omitempty"` // the config's schema
}

func infoOf(t cognition.NodeType, full bool) NodeTypeInfo {
	i := NodeTypeInfo{ID: t.ID, Title: t.Title, Category: t.Category, Description: t.Description, Example: t.Example, Event: t.Event, Effects: t.Effects}
	if full {
		pins := t.Pins
		i.Pins, i.Config = &pins, t.Config
	}
	return i
}

func contributeTools(reg *extensions.Registry, deps func() Deps) {
	live.Calls.Register(reg, live.Call{
		Descriptor: extensions.Descriptor{ID: "bp.test", Source: "cognition"}, Title: "Test a graph",
		Description: "Runs a graph's draft once with sample inputs and returns its outputs, what it did and its run; ends with one result event.",
		Run: func(c live.CallRun) *features.ServiceResponse {
			var a TestArgs
			if err := json.Unmarshal(c.Args, &a); err != nil {
				return c.Fail(http.StatusBadRequest, log.Code("invalid_args").New(err.Error()))
			}
			res, fail := Test(c.ServiceArgs, deps(), a)
			if fail != nil {
				return fail
			}
			c.Emit("result", res)
			return nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "bp.test", Source: "cognition"}, Title: "Test a graph", Effect: tools.EffectRead,
		Description: "Runs a Cognition graph's draft once with sample inputs, as a dry run: a handler's commands and a command graph's events are reported, not sent or committed. Returns the outputs, what the graph did (report), and the error and node it failed on.",
		Input:       json.RawMessage(`{"type":"object","properties":{"graph":{"type":"string"},"event":{"type":"string","description":"The event node type to start from; the graph's first when left out"},"inputs":{"type":"object","description":"The event's outputs by pin id"}},"required":["graph"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[TestArgs](call)
			if err != nil {
				return tools.Result{}, err
			}
			res, fail := Test(call.ServiceArgs, deps(), a)
			if fail != nil {
				return tools.Result{}, fmt.Errorf("%s", fail.Message)
			}
			raw, _ := json.Marshal(res)
			var content map[string]any
			_ = json.Unmarshal(raw, &content)
			summary := "Tested " + a.Graph
			if res.Error != "" {
				summary += ": failed on " + res.NodeID
			}
			return tools.Result{Content: content, Summary: summary}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "bp.listKinds", Source: "cognition"}, Title: "List graph kinds", Effect: tools.EffectNone,
		Description: "Lists the kinds of Cognition graph (blueprint): what each is for, the events that start it, what it returns and the spec it needs (bp.create's spec).",
		Input:       json.RawMessage(`{"type":"object","properties":{}}`),
		Run: func(call tools.Call) (tools.Result, error) {
			var out []map[string]any
			for _, k := range cognition.Kinds.All(reg) {
				out = append(out, map[string]any{"id": k.ID, "title": k.Title, "description": k.Description, "returns": k.Returns, "events": k.Events, "pure": k.Pure, "spec": k.Spec})
			}
			return tools.Result{Content: map[string]any{"kinds": out}, Summary: strconv.Itoa(len(out)) + " kinds"}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "bp.listNodeTypes", Source: "cognition"}, Title: "List node types", Effect: tools.EffectNone,
		Description: "Lists the node types a graph can hold: those a kind allows, matching a search over their titles, categories and descriptions. Describe one with bp.describeNodeType for its pins and config.",
		Input:       json.RawMessage(`{"type":"object","properties":{"kind":{"type":"string","description":"Only types a graph of this kind allows"},"search":{"type":"string"}}}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct{ Kind, Search string }](call)
			if err != nil {
				return tools.Result{}, err
			}
			cat := cognition.CatalogOf(reg)
			k, hasKind := cat.Kind(a.Kind)
			if a.Kind != "" && !hasKind {
				return tools.Result{}, fmt.Errorf("there is no graph kind %q; list them with bp.listKinds", a.Kind)
			}
			search := strings.ToLower(a.Search)
			var out []NodeTypeInfo
			for _, t := range cat.Types {
				if hasKind && !k.Allows(t) {
					continue
				}
				if search != "" && !strings.Contains(strings.ToLower(t.ID+" "+t.Title+" "+t.Category+" "+t.Description), search) {
					continue
				}
				out = append(out, infoOf(t, false))
			}
			return tools.Result{Content: map[string]any{"nodeTypes": out}, Summary: strconv.Itoa(len(out)) + " node types"}, nil
		},
	})

	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "bp.describeNodeType", Source: "cognition"}, Title: "Describe a node type", Effect: tools.EffectNone,
		Description: "Describes one node type: what it does, an example, its pins (id, type, description) and its config's schema. Pins of nodes whose pins follow their config are given for the empty config.",
		Input:       json.RawMessage(`{"type":"object","properties":{"type":{"type":"string"}},"required":["type"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			a, err := tools.Decode[struct{ Type string }](call)
			if err != nil {
				return tools.Result{}, err
			}
			t, ok := cognition.CatalogOf(reg).Type(a.Type)
			if !ok {
				return tools.Result{}, fmt.Errorf("there is no node type %q; list them with bp.listNodeTypes", a.Type)
			}
			info := infoOf(t, true)
			if t.PinsFor != nil {
				if pins, err := t.PinsFor(nil); err == nil {
					info.Pins = &pins
				}
			}
			raw, _ := json.Marshal(info)
			var content map[string]any
			_ = json.Unmarshal(raw, &content)
			return tools.Result{Content: content, Summary: t.Title}, nil
		},
	})
}
