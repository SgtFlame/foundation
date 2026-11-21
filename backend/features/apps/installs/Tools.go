package installs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/apps"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// ContributeTools adds the installed apps' manifest tools (forge 07j) to
// every round's and run's tools: <appId>.<name>, run by the installed
// version's copy of its function graph as the calling principal.
func ContributeTools(reg *extensions.Registry, deps func() Deps) {
	tools.Sources.Register(reg, tools.Source{
		Descriptor: extensions.Descriptor{ID: "apps", Source: "apps"},
		Tools: func(args features.ServiceArgs) []tools.Tool {
			return InstalledTools(args, reg, deps())
		},
	})
}

// InstalledTools are the tools of the apps installed in the principal's
// tenant.
func InstalledTools(args features.ServiceArgs, reg *extensions.Registry, d Deps) []tools.Tool {
	if d.Projections == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(args.Context())
	defer cancel()
	entries, err := d.Projections.Watch(ctx, args.Principal.TenantID, Projector, TargetType+".*")
	if err != nil {
		return nil
	}
	types := cognition.Nodes.All(reg)
	var out []tools.Tool
	for e := range entries {
		if e == nil {
			break
		}
		var in apps.Install
		if e.Deleted || json.Unmarshal(e.Value.Value, &in) != nil || !in.Installed() {
			continue
		}
		for _, t := range in.Version.Manifest.Tools {
			if _, g, ok := in.Version.Tool(t.Name); ok {
				out = append(out, appTool(d, in.App, t, g, types))
			}
		}
	}
	return out
}

// appTool is one manifest tool as a server tool.
func appTool(d Deps, app string, t apps.Tool, g cognition.Graph, types []cognition.NodeType) tools.Tool {
	effect := tools.Effect(t.Effect)
	if effect == "" {
		effect = tools.EffectRead
	}
	in, _ := cognition.SignatureOf(g, types)
	id := t.ToolID(app)
	title := t.Title
	if title == "" {
		title = id
	}
	return tools.Tool{
		Descriptor: extensions.Descriptor{ID: id, Source: "app:" + app}, Title: title, Description: t.Description,
		Input: InputSchema(in), Effect: effect,
		Run: func(call tools.Call) (tools.Result, error) {
			inputs := cognition.Values{}
			if len(call.Args) > 0 {
				if err := json.Unmarshal(call.Args, &inputs); err != nil {
					return tools.Result{}, fmt.Errorf("the arguments are not an object: %v", err)
				}
			}
			if d.Nodes.Engine == nil {
				return tools.Result{}, fmt.Errorf("%s cannot run here: there is no engine", id)
			}
			graph := g
			res, err := d.Nodes.Engine.Run(engine.RunArgs{ServiceArgs: call.ServiceArgs, Graph: &graph, Event: cognition.EventCall,
				Inputs: inputs, Trigger: "tool:" + id, Trace: engine.TraceFailures})
			if err != nil {
				msg := err.Error()
				// A refusal of the principal's rights reads as forbidden to
				// the model, so it stops trying
				if strings.Contains(msg, "access_denied") || strings.Contains(msg, "You may not") {
					return tools.Result{}, fmt.Errorf("forbidden: %s", msg)
				}
				return tools.Result{}, fmt.Errorf("%s failed: %s", id, msg)
			}
			content := map[string]any{}
			for k, v := range res.Outputs {
				content[k] = v
			}
			return tools.Result{Content: content}, nil
		},
	}
}

// InputSchema is a JSON Schema object of a function graph's inputs: its
// On call outputs, each a property, required unless optional.
func InputSchema(pins []cognition.Pin) json.RawMessage {
	props := map[string]any{}
	required := []string{}
	for _, p := range pins {
		s := map[string]any{}
		switch t := p.Type; {
		case t == "string":
			s["type"] = "string"
		case t == "number":
			s["type"] = "number"
		case t == "bool":
			s["type"] = "boolean"
		case strings.HasSuffix(t, "[]"):
			s["type"] = "array"
		}
		desc := p.Description
		if desc == "" {
			desc = p.Name
		}
		if desc != "" {
			s["description"] = desc
		}
		props[p.ID] = s
		if !p.Optional {
			required = append(required, p.ID)
		}
	}
	raw, _ := json.Marshal(map[string]any{"type": "object", "properties": props, "required": required})
	return raw
}
