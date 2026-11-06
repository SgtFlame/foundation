// Package livenodes is the live core's Cognition node family and graph
// kinds (forge 02-Blueprints, documentation/Blueprints.md): command graphs
// decide a command's events, data.query graphs are live queries,
// server.action graphs run server logic that changes data only by sending
// commands, and app.trigger graphs (their kind and events only, until
// forge 07-Email) react to events in the background. A published command
// or query graph is found by the bus and the subscriptions through
// CommandFor and QueryFor, so it is a command or a query like any other.
package livenodes

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/presence"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
)

var log = live.Log.Tags("nodes")

// Kinds.
const (
	KindCommand = "command"
	KindAction  = "server.action"
	KindQuery   = "data.query"
	KindTrigger = "app.trigger"
)

// Event node types.
const (
	OnCommand = "live.onCommand"
	OnQuery   = "live.onQuery"
)

// Deps are what the nodes and resolvers need, made after the registry.
type Deps struct {
	Engine        *engine.Engine
	Bus           *bus.Bus
	Subscriptions *subscribe.Subscriptions
	Store         live.Store
	Presence      *presence.Presence
}

// Sender sends commands for a graph's Send command nodes: a UI session's
// handle as its user, or, for actions, the bus.
type Sender interface {
	Command(typ, target string, args any) ([]live.Problem, *features.ServiceResponse)
}

// CommandHost is a command graph's host: the events its Emit event nodes
// decide and the problems its Reject nodes give.
type CommandHost struct {
	Events   []live.NewEvent `json:"events"`
	Problems []live.Problem  `json:"problems"`
	// Decision is what the command decides on, for nodes that change other
	// targets (records' write nodes); nil in a test run.
	Decision *live.Decision `json:"-"`
}

// Read is a projected value a query graph read. With Pattern, Key is a
// key pattern ("task.*"): the graph runs again, debounced, when any key
// matching it changes.
type Read struct {
	Projector string `json:"projector"`
	Key       string `json:"key"`
	Pattern   bool   `json:"pattern,omitempty"`
}

// patternDebounce is how long a query graph that read a key pattern waits
// for more changes before it runs again.
var patternDebounce = 100 * time.Millisecond

// updater is a store that can watch changes without the current keys, and
// say how far a projector has folded.
type updater interface {
	Updates(ctx context.Context, tenant uint64, projector, pattern string) (<-chan *live.Entry, error)
	Folded(ctx context.Context, tenant uint64, projector string) (live.Position, error)
}

// QueryHost is a data.query graph's host: what it read, which the query
// watches to run it again.
type QueryHost struct {
	Reads []Read        `json:"reads"`
	Pos   live.Position `json:"-"`
}

// TestSender is a test run's sender: it records commands, never sends them.
type TestSender struct {
	Sent []map[string]any `json:"commands"`
}

// Command records the command.
func (t *TestSender) Command(typ, target string, args any) ([]live.Problem, *features.ServiceResponse) {
	t.Sent = append(t.Sent, map[string]any{"command": typ, "target": target, "args": args})
	return nil, nil
}

// busSender sends as the run's principal.
type busSender struct {
	args features.ServiceArgs
	bus  *bus.Bus
}

func (b busSender) Command(typ, target string, args any) ([]live.Problem, *features.ServiceResponse) {
	raw, _ := json.Marshal(args)
	res := b.bus.Execute(bus.Request{ServiceArgs: b.args, ID: "bp-" + strconv.FormatUint(features.NewTSID(), 10), Type: typ, Target: target, Args: raw})
	if res.Common.Code >= 300 {
		return res.Problems, &res.Common
	}
	return nil, nil
}

// CommandGraphID is the id of the graph defining command typ.
func CommandGraphID(typ string) string { return "cmd-" + strings.ReplaceAll(typ, ".", "-") }

// QueryGraphID is the id of the graph defining query typ.
func QueryGraphID(typ string) string { return "qry-" + strings.ReplaceAll(typ, ".", "-") }

// CommandSpec is a command graph's spec: the command it defines.
type CommandSpec struct {
	Command     string          `json:"command"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Args        json.RawMessage `json:"args,omitempty"`
	Area        string          `json:"area"` // "app_<appId>", "rec" or "def"
	TargetType  string          `json:"targetType"`
	Projection  string          `json:"projection"` // the projector whose value is the target's state
	Action      string          `json:"action"`     // the authz action it needs
	Events      []string        `json:"events,omitempty"`
}

// QuerySpec is a data.query graph's spec: the query it defines.
type QuerySpec struct {
	Query       string          `json:"query"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Params      json.RawMessage `json:"params,omitempty"`
	Action      string          `json:"action"`
}

const commandSpecSchema = `{"type":"object","properties":{
  "command":{"type":"string","description":"The command type it defines, e.g. hello.reset; the graph's id is cmd-<type with - for .>"},
  "title":{"type":"string"},"description":{"type":"string","description":"For a model: what it does and when to use it"},
  "args":{"type":"object","description":"The args' JSON Schema"},
  "area":{"type":"string","description":"The stream of its target: app_<appId>, rec or def"},
  "targetType":{"type":"string"},"projection":{"type":"string","description":"The projector whose value is the target's state"},
  "action":{"type":"string","description":"The authz action it needs, e.g. hello.write"},
  "events":{"type":"array","items":{"type":"string"}}},
  "required":["command","title","description","area","targetType","projection","action"]}`

const querySpecSchema = `{"type":"object","properties":{
  "query":{"type":"string","description":"The query it defines, e.g. hello.counter; the graph's id is qry-<query with - for .>"},
  "title":{"type":"string"},"description":{"type":"string","description":"For a model: what it returns"},
  "params":{"type":"object","description":"The params' JSON Schema"},
  "action":{"type":"string","description":"The authz action it needs, e.g. hello.read"}},
  "required":["query","title","description","action"]}`

func specOf[T any](raw json.RawMessage) (T, error) {
	var s T
	if len(raw) == 0 {
		return s, fmt.Errorf("the graph has no spec")
	}
	err := json.Unmarshal(raw, &s)
	return s, err
}

// tracing is how a graph's runs are traced (Blueprints question 4): fully
// while it is open in an editor, else only when they fail.
func tracing(args features.ServiceArgs, d Deps, graphID string) string {
	if d.Presence != nil && d.Presence.Here(args.Context(), args.Principal.TenantID, "graph."+graphID) {
		return engine.TraceAll
	}
	return engine.TraceFailures
}

// Limits for a command graph, which runs inside the commit and decides
// again on a conflict, so it must be quick.
var commandLimits = &engine.Limits{MaxSteps: 200, MaxWallMs: 2000, MaxLoop: 200}

// inputs are an event's outputs: the fields of obj at the top level, and
// obj itself under name.
func inputs(name string, raw json.RawMessage, extra cognition.Values) cognition.Values {
	in := cognition.Values{}
	var obj any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &obj)
	}
	if m, ok := obj.(map[string]any); ok {
		for k, v := range m {
			in[k] = v
		}
	} else {
		obj = map[string]any{}
	}
	in[name] = obj
	for k, v := range extra {
		in[k] = v
	}
	return in
}

// CommandFor is the command a tenant's published command graph defines,
// for the bus's Resolve.
func CommandFor(deps func() Deps) func(args features.ServiceArgs, typ string) (live.Command, bool) {
	return func(args features.ServiceArgs, typ string) (live.Command, bool) {
		d := deps()
		id := CommandGraphID(typ)
		g, ok := d.Engine.Graph(args, id)
		if !ok || g.Kind != KindCommand {
			return live.Command{}, false
		}
		spec, err := specOf[CommandSpec](g.Spec)
		if err != nil || spec.Command != typ {
			return live.Command{}, false
		}
		return live.Command{
			Descriptor: extensions.Descriptor{ID: typ, Source: "graph:" + id}, Title: spec.Title, Description: spec.Description,
			Args: spec.Args, Events: spec.Events, Area: live.Area(spec.Area), TargetType: spec.TargetType, Projection: spec.Projection, Action: spec.Action,
			Decide: func(dec live.Decision) ([]live.NewEvent, []live.Problem) {
				var state any
				if dec.State != nil {
					_ = json.Unmarshal(dec.State, &state)
				}
				host := &CommandHost{Decision: &dec}
				_, err := d.Engine.Run(engine.RunArgs{ServiceArgs: dec.ServiceArgs, GraphID: id, Event: OnCommand, Trigger: "command:" + typ,
					Inputs: inputs("args", dec.Args, cognition.Values{"state": state, "target": dec.Target.ID}),
					Host:   host, Trace: tracing(dec.ServiceArgs, d, id), Limits: commandLimits})
				if err != nil {
					return nil, []live.Problem{{Message: "the command graph failed: " + err.Error(), Fix: "see its run in the graph's Log Viewer"}}
				}
				if len(host.Problems) > 0 {
					return nil, host.Problems
				}
				return host.Events, nil
			},
		}, true
	}
}

// QueryFor is the query a tenant's published query graph defines, for the
// subscriptions' Resolve: it runs the graph, then again whenever a value it
// read changes or the graph is published again.
func QueryFor(deps func() Deps) func(args features.ServiceArgs, typ string) (live.Query, bool) {
	return func(args features.ServiceArgs, typ string) (live.Query, bool) {
		d := deps()
		id := QueryGraphID(typ)
		g, ok := d.Engine.Graph(args, id)
		if !ok || g.Kind != KindQuery {
			return live.Query{}, false
		}
		spec, err := specOf[QuerySpec](g.Spec)
		if err != nil || spec.Query != typ {
			return live.Query{}, false
		}
		return live.Query{
			Descriptor: extensions.Descriptor{ID: typ, Source: "graph:" + id}, Title: spec.Title, Description: spec.Description,
			Params: spec.Params, Action: spec.Action,
			Run: func(f live.Feed) error { return runQuery(f, d, id) },
		}, true
	}
}

func runQuery(f live.Feed, d Deps, id string) error {
	tenant := f.Args().Principal.TenantID
	first := true
	for {
		host := &QueryHost{Pos: live.Position{}}
		res, err := d.Engine.Run(engine.RunArgs{ServiceArgs: f.Args(), GraphID: id, Event: OnQuery, Trigger: "query",
			Inputs: inputs("params", f.Params(), nil), Host: host, Trace: tracing(f.Args(), d, id), Limits: commandLimits})
		if err != nil {
			return err
		}
		value := res.Outputs["result"]
		if first {
			f.Snapshot(value, host.Pos)
			first = false
		} else {
			raw, _ := json.Marshal(value)
			f.Change(live.Delta{Op: "replace", Value: raw}, host.Pos)
		}
		// Run again when what it read changes, or when it is published again
		ctx, cancel := context.WithCancel(f.Context())
		changed := make(chan struct{}, 1)
		watch := append(host.Reads, Read{Projector: "cognition.published", Key: "graph." + id})
		debounce := false
		for _, r := range watch {
			var entries <-chan *live.Entry
			var err error
			updates := false // an updates-only watch sends no marker after the current keys
			if u, ok := f.Store().(updater); ok && r.Pattern {
				entries, err = u.Updates(ctx, tenant, r.Projector, r.Key)
				debounce, updates = true, true
			} else {
				entries, err = f.Store().Watch(ctx, tenant, r.Projector, r.Key)
			}
			if err != nil {
				cancel()
				return err
			}
			go func() {
				started := updates
				for e := range entries {
					if e == nil {
						started = true
						continue
					}
					if started {
						select {
						case changed <- struct{}{}:
						default:
						}
					}
				}
			}()
		}
		// A pattern's watch starts after the run read: a change folded in
		// between shows as the projector being past what the run saw
		for _, r := range host.Reads {
			u, ok := f.Store().(updater)
			if !ok || !r.Pattern {
				continue
			}
			if folded, err := u.Folded(ctx, tenant, r.Projector); err == nil {
				for stream, seq := range folded {
					if !host.Pos.Reached(stream, seq) {
						select {
						case changed <- struct{}{}:
						default:
						}
					}
				}
			}
		}
		select {
		case <-changed:
			if debounce {
				select {
				case <-time.After(patternDebounce):
				case <-f.Context().Done():
				}
			}
			cancel()
		case <-f.Context().Done():
			cancel()
			return nil
		}
	}
}
