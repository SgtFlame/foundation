// Package livetools contributes the live core's chat tools: the catalogs of
// commands and queries, a one-time read, and one tool per command, so the
// chat can do whatever a user can (documentation/Live.md › Tools).
package livetools

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
	"github.com/SgtFlame/foundation/backend/features/live/subscribe"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Deps are what the tools run on, made after the registry.
type Deps struct {
	Bus           *bus.Bus
	Subscriptions *subscribe.Subscriptions
}

// Contribute registers the catalog tools and a tool per command
// registered so far, so it runs after every feature's commands.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "live.listCommands", Source: "live"}, Title: "List commands", Effect: tools.EffectNone,
		Description: "Lists every command (the only way objects change) with its description, argument schema and examples. Each command is also a tool of the same name.",
		Run: func(call tools.Call) (tools.Result, error) {
			var list []map[string]any
			for _, c := range live.Commands.All(reg) {
				list = append(list, map[string]any{"type": c.ID, "title": c.Title, "description": c.Description, "args": c.Args, "examples": c.Examples, "events": c.Events, "targetType": c.TargetType})
			}
			return tools.Result{Content: map[string]any{"commands": list}, Summary: fmt.Sprintf("%d commands", len(list))}, nil
		},
	})
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "live.listQueries", Source: "live"}, Title: "List queries", Effect: tools.EffectNone,
		Description: "Lists every query (a named read, answered with a snapshot and then changes) with its description, parameter schema and examples. Read one with live.query.",
		Run: func(call tools.Call) (tools.Result, error) {
			var list []map[string]any
			for _, q := range live.Queries.All(reg) {
				list = append(list, map[string]any{"query": q.ID, "title": q.Title, "description": q.Description, "params": q.Params, "examples": q.Examples})
			}
			return tools.Result{Content: map[string]any{"queries": list}, Summary: fmt.Sprintf("%d queries", len(list))}, nil
		},
	})
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "live.query", Source: "live"}, Title: "Read a query", Effect: tools.EffectRead,
		Description: "Reads a query's current result once. List the queries and their parameters with live.listQueries.",
		Input:       json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"The query, e.g. hello.counter"},"params":{"type":"object","description":"Its parameters"}},"required":["query"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			args, err := tools.Decode[struct {
				Query  string
				Params json.RawMessage
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			data, pos, fail := deps().Subscriptions.Once(call.ServiceArgs, args.Query, args.Params)
			if fail != nil {
				return tools.Result{}, fmt.Errorf("%s", fail.Message)
			}
			return tools.Result{Content: map[string]any{"data": data, "position": pos}, Summary: "Read " + args.Query}, nil
		},
	})
	for _, c := range live.Commands.All(reg) {
		if !c.Internal {
			tools.Tools.Register(reg, commandTool(c, deps))
		}
	}
	// Commands a tenant defines, such as its command graphs, have no tool
	// of their own
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "live.command", Source: "live"}, Title: "Send a command", Effect: tools.EffectWrite,
		Description: "Sends any command by its type, including those a tenant's published command graphs define (list them with live.query cognition.graphs {kind: \"command\"}). Registered commands also have a tool of their own.",
		Input:       json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"target":{"type":"string","description":"The id of the object to change"},"args":{"type":"object"}},"required":["command","target"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			args, err := tools.Decode[struct {
				Command, Target string
				Args            json.RawMessage
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			res := deps().Bus.Execute(bus.Request{ServiceArgs: call.ServiceArgs, ID: commandID(call), Type: args.Command, Target: args.Target, RequestID: call.ID, Args: args.Args})
			if res.Common.Code >= 300 {
				return tools.Result{Content: map[string]any{"problems": res.Problems}}, fmt.Errorf("%s", res.Common.Message)
			}
			return tools.Result{Content: map[string]any{"stream": res.Stream, "seq": res.Seq}, Summary: "Sent " + args.Command}, nil
		},
	})
}

func commandTool(c live.Command, deps func() Deps) tools.Tool {
	input := fmt.Sprintf(`{"type":"object","properties":{"target":{"type":"string","description":"The id of the %s to change"},"args":%s},"required":["target"]}`, c.TargetType, orObject(c.Args))
	if c.NewTarget != nil {
		input = fmt.Sprintf(`{"type":"object","properties":{"target":{"type":"string","description":"Leave it out: a new id is made and returned"},"args":%s}}`, orObject(c.Args))
	}
	if c.NewTargetFor != nil {
		input = fmt.Sprintf(`{"type":"object","properties":{"target":{"type":"string","description":"The new %s's id; leave it out to make one from its name (returned)"},"args":%s}}`, c.TargetType, orObject(c.Args))
	}
	return tools.Tool{
		Descriptor: extensions.Descriptor{ID: c.ID, Source: c.Source}, Title: c.Title, Effect: tools.EffectWrite,
		Description: c.Description, Input: json.RawMessage(input),
		Run: func(call tools.Call) (tools.Result, error) {
			args, err := tools.Decode[struct {
				Target string
				Args   json.RawMessage
			}](call)
			if err != nil {
				return tools.Result{}, err
			}
			// A model sometimes puts the command's args beside args (forge
			// 04f): say so, rather than run the command without them
			var top map[string]json.RawMessage
			_ = json.Unmarshal(call.Args, &top)
			delete(top, "target")
			delete(top, "args")
			if len(top) > 0 {
				misplaced := slices.Sorted(maps.Keys(top))
				return tools.Result{}, fmt.Errorf("%s belong inside args: call %s with {target, args: {%s, ...}}", strings.Join(misplaced, ", "), c.ID, strings.Join(misplaced, ", "))
			}
			res := deps().Bus.Execute(bus.Request{ServiceArgs: call.ServiceArgs, ID: commandID(call), Type: c.ID, Target: args.Target, RequestID: call.ID, Args: args.Args})
			if res.Common.Code >= 300 {
				return tools.Result{Content: map[string]any{"problems": res.Problems}}, fmt.Errorf("%s", res.Common.Message)
			}
			content := map[string]any{"stream": res.Stream, "seq": res.Seq, "target": res.Target}
			if c.Report != nil {
				tg, _ := c.TargetFor(res.Target, args.Args)
				content["report"] = c.Report(call.ServiceArgs, tg, res.Seq)
			}
			return tools.Result{Content: content, Summary: c.Title}, nil
		},
	}
}

// commandID is a tool call's command id: a call is made once, so a new id.
func commandID(call tools.Call) string {
	return fmt.Sprintf("tool-%d", features.NewTSID())
}

func orObject(schema json.RawMessage) string {
	if len(schema) == 0 {
		return `{"type":"object"}`
	}
	return string(schema)
}
