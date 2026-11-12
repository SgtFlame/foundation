// Package toolsnodes contributes the Cognition LLM and tool nodes
// (documentation/Cognition.md, Tools.md): the Agent node (the tool loop as
// a node), Ask model (one plain model call), and the tool set nodes of
// forge 06i: Tools selects server and client tools into a ToolSet, Describe
// tool re-describes one with a template, and Graph tool declares a tool a
// function graph runs. A chat mode's Stream reply and the Agent node offer
// a ToolSet. The Agent runs unattended, so its write tools are denied
// (they need the user's approval) and it has no client tools.
package toolsnodes

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/ai/settings"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/SgtFlame/foundation/backend/features/tools/settings"
	"gorm.io/gorm"
)

// Node type IDs.
const (
	AgentID     = "llm.agent"
	CompleteID  = "llm.complete"
	SelectID    = "tools.select"
	DescribeID  = "tools.describe"
	GraphToolID = "tools.graph"
)

// TypeToolSet is the data type of a tool set: []tools.Spec.
const TypeToolSet = "ToolSet"

// Deps are what the nodes run with, created after the registry.
type Deps struct {
	DB       *gorm.DB
	Chat     chatservice.Service
	Config   config.Reader
	Registry *extensions.Registry // where the tools are
}

// Toolbox is what a run's host gives when it has tools of its own: a chat
// round's server tools and the client tools its browser declared.
type Toolbox interface {
	ServerTools() []tools.Tool
	ClientTools() []tools.Tool
}

// AgentConfig is an Agent node's config.
type AgentConfig struct {
	Level string   `json:"level"` // an ai.chat.levels ID; the default level when empty
	Tools []string `json:"tools"` // the tools it may call; empty allows every server tool that does not ask
	// MaxSteps is its steps in place of the tools section's (forge 06g:
	// the Builder agent builds a whole app); 0 keeps the section's
	MaxSteps int `json:"maxSteps"`
}

// Contribute registers the nodes and the ToolSet type.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	cognition.Types.Register(reg, cognition.DataType{Descriptor: extensions.Descriptor{ID: TypeToolSet, Order: 300, Source: "tools"},
		Color: "#26a69a", Description: "Tools a model may call: server and client tools by name, maybe re-described, and graph tools"})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: AgentID, Order: 300, Source: "tools"},
		Title:       "Agent",
		Category:    "LLM",
		Description: "Asks the model, letting it call tools until it answers: its Tools set when joined, else its config's. Tools that change data and ask first are denied: a run cannot ask for approval.",
		Example:     "Agent with the prompt \"Summarize the open tasks\" and the kb.search tool",
		Effects:     true,
		External:    true,
		Sample:      cognition.Values{"reply": "(the model's reply: not asked in a sandboxed run)", "calls": []any{}},
		Config:      json.RawMessage(agentSchema),
		Pins: cognition.Pins{
			In: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("prompt", "Prompt", "string", "What to ask"),
				{ID: "system", Name: "System", Type: "string", Optional: true, Description: "The instructions"},
				{ID: "maxSteps", Name: "Steps", Type: "number", Optional: true, Description: "How many times it may call tools before it must answer, in place of its config's"},
				{ID: "tools", Name: "Tools", Type: TypeToolSet, Optional: true, Description: "The tools it may call, in place of its config's"},
			},
			Out: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("reply", "Reply", "string", "The model's answer"),
				cognition.DataPin("calls", "Calls", "json", "The tool calls it made: name, status and summary"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			return runAgent(x, in, deps())
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: CompleteID, Order: 305, Source: "tools"},
		Title:       "Ask model",
		Category:    "LLM",
		Description: "One model call without tools: the system instructions and the prompt in, the answer out, at a level; leaves by Failed with the error when the model fails",
		Example:     "Ask model at the chat section's title level to name the conversation from a Template of the exchange",
		Effects:     true,
		External:    true,
		Sample:      cognition.Values{"text": "(the model's answer: not asked in a sandboxed run)"},
		Config: json.RawMessage(`{"type":"object","properties":{
			"level":{"type":"string","title":"Level","ref":"ai.chat.levels#levels","description":"The level asked; the Level pin wins, else the default level"},
			"feature":{"type":"string","title":"Feature","description":"What usage records it under, e.g. chat-title"}}}`),
		Pins: cognition.Pins{
			In: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("prompt", "Prompt", "string", "What to ask"),
				{ID: "system", Name: "System", Type: "string", Optional: true, Description: "The instructions"},
				{ID: "level", Name: "Level", Type: "string", Optional: true, Description: "An ai.chat.levels id, in place of its config's"},
			},
			Out: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.ExecPin("failed", "Failed"),
				cognition.DataPin("text", "Text", "string", "The model's answer"),
				cognition.DataPin("error", "Error", "string", "Why the model failed, when it did"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			var c struct{ Level, Feature string }
			if err := x.Config(&c); err != nil {
				return nil, "", err
			}
			prompt, err := cognition.Get[string](in, "prompt")
			if err != nil {
				return nil, "", err
			}
			system, _ := cognition.Get[string](in, "system")
			if l, _ := cognition.Get[string](in, "level"); l != "" {
				c.Level = l
			}
			d := deps()
			if d.Chat == nil {
				return nil, "", tools.Log.Code("model_not_configured").New("Ask model has no chat service")
			}
			sa := x.ServiceArgs
			sa.Meta.Feature, sa.Meta.Level = cmpOr(c.Feature, cmpOr(sa.Meta.Feature, "graph")), c.Level
			resp := d.Chat.Complete(chatservice.CompleteArgs{ServiceArgs: sa, Level: c.Level, System: system,
				Messages: []chatservice.Message{{Role: chatservice.RoleUser, Content: prompt}}})
			if resp.Common.Code >= 300 {
				return cognition.Values{"error": resp.Common.Message}, "failed", nil
			}
			return cognition.Values{"text": resp.Message.Content}, cognition.Then, nil
		},
	})
	contributeToolSets(reg, deps)
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// runAgent runs the Agent node: the tool loop without a gate.
func runAgent(x *cognition.Exec, in cognition.Values, d Deps) (cognition.Values, string, error) {
	prompt, err := cognition.Get[string](in, "prompt")
	if err != nil {
		return nil, "", err
	}
	system, err := cognition.Get[string](in, "system")
	if err != nil {
		return nil, "", err
	}
	var cfg AgentConfig
	if len(x.Node.Config) > 0 {
		if err := json.Unmarshal(x.Node.Config, &cfg); err != nil {
			return nil, "", tools.Log.Code("bad_agent_config").Wrap(err)
		}
	}
	steps, err := cognition.Get[float64](in, "maxSteps")
	if err != nil {
		return nil, "", err
	}
	if steps > 0 {
		cfg.MaxSteps = int(steps)
	}
	if d.Chat == nil {
		return nil, "", tools.Log.Code("agent_not_configured").New("the Agent node has no chat service")
	}
	settings := Settings(x, d)
	if d.Config != nil && cfg.Level == "" {
		if levels, err := config.Read[aisettings.Levels](d.Config, x.ServiceArgs, aisettings.LevelsID); err == nil {
			if l, ok := levels.Find(""); ok {
				cfg.Level = l.ID
			}
		}
	}
	if cfg.MaxSteps > 0 {
		settings.MaxSteps = cfg.MaxSteps
	}
	var offered []tools.Tool
	if _, joined := in["tools"]; joined {
		set, err := cognition.Get[[]tools.Spec](in, "tools")
		if err != nil {
			return nil, "", err
		}
		offered = Resolve(x, set, tools.Tools.All(d.Registry), nil)
	} else {
		offered = tools.Enabled(tools.Tools.All(d.Registry), nil, settings)
		// Writes listed by name are offered, and denied, so the trace says why
		offered = slices.DeleteFunc(offered, func(t tools.Tool) bool {
			if len(cfg.Tools) == 0 {
				return t.Effect.Asks()
			}
			return !slices.Contains(cfg.Tools, t.ID)
		})
	}
	sa := x.ServiceArgs
	sa.Meta = sa.Meta.WithFeature("agent")
	out := tools.Loop{DB: d.DB, Chat: d.Chat}.Run(tools.RunArgs{
		CompleteArgs: chatservice.CompleteArgs{ServiceArgs: sa, Level: cfg.Level, System: system,
			Messages: []chatservice.Message{{Role: chatservice.RoleUser, Content: prompt}}},
		Tools:    offered,
		Settings: settings,
	})
	if c := out.Response.Common; c.Code >= 300 {
		return nil, "", tools.Log.Code("agent_failed").With("status", c.Code).Errorf("the model failed: %s", c.Message)
	}
	return cognition.Values{"reply": out.Reply, "calls": Calls(out.Calls)}, cognition.Then, nil
}

// Calls are a loop's calls as a node gives them: name, status, summary
// and error.
func Calls(calls []tools.ToolCall) []map[string]any {
	out := make([]map[string]any, len(calls))
	for i, c := range calls {
		out[i] = map[string]any{"name": c.Name, "status": c.Status, "summary": c.Summary, "error": c.Error}
	}
	return out
}

// Settings are the tools section for the run, or its defaults.
func Settings(x *cognition.Exec, d Deps) toolssettings.Tools {
	settings := toolssettings.Defaults
	if d.Config != nil {
		if s, err := config.Read[toolssettings.Tools](d.Config, x.ServiceArgs, toolssettings.ID); err == nil {
			settings = s
		}
	}
	return settings
}

// Resolve turns a tool set into the tools a loop offers: server and client
// tools found by name (with the set's description), and graph tools, each
// running its function graph through the node's run. A name the server
// and the browser no longer have is left out.
func Resolve(x *cognition.Exec, set []tools.Spec, server, client []tools.Tool) []tools.Tool {
	out := make([]tools.Tool, 0, len(set))
	find := func(list []tools.Tool, name string) (tools.Tool, bool) {
		i := slices.IndexFunc(list, func(t tools.Tool) bool { return t.ID == name })
		if i < 0 {
			return tools.Tool{}, false
		}
		return list[i], true
	}
	for _, s := range set {
		if slices.ContainsFunc(out, func(t tools.Tool) bool { return t.ID == s.Name }) {
			continue
		}
		switch s.Source {
		case tools.SourceGraph:
			out = append(out, graphTool(x, s))
		default:
			list := server
			if s.Source == tools.SourceClient {
				list = client
			}
			t, ok := find(list, s.Name)
			if !ok {
				continue
			}
			if s.Description != "" {
				t.Description = s.Description
			}
			if s.Title != "" {
				t.Title = s.Title
			}
			out = append(out, t)
		}
	}
	return out
}

// graphTool is a graph tool as a server tool: a call runs its function
// graph from On call with the arguments as inputs, and the outputs are
// the result, with the Summary template rendered over them and args.
func graphTool(x *cognition.Exec, s tools.Spec) tools.Tool {
	effect := s.Effect
	if effect == "" {
		effect = tools.EffectRead
	}
	t := tools.Tool{
		Descriptor: extensions.Descriptor{ID: s.Name, Source: "graph:" + s.Graph}, Title: s.Title, Description: s.Description,
		Input: s.Input, Effect: effect, Timeout: time.Duration(s.TimeoutSeconds) * time.Second,
	}
	if s.Asks != nil {
		asks := *s.Asks
		t.Asks = func(json.RawMessage) bool { return asks }
	}
	t.Run = func(call tools.Call) (tools.Result, error) {
		inputs := cognition.Values{}
		if len(call.Args) > 0 {
			if err := json.Unmarshal(call.Args, &inputs); err != nil {
				return tools.Result{}, fmt.Errorf("the arguments are not an object: %v", err)
			}
		}
		out, err := x.Call(s.Graph, inputs)
		if err != nil {
			return tools.Result{}, fmt.Errorf("%s failed: %v", s.Name, err)
		}
		content := map[string]any{}
		for k, v := range out {
			content[k] = v
		}
		res := tools.Result{Content: content}
		if s.Summary != "" {
			data := map[string]any{"args": map[string]any(inputs)}
			for k, v := range content {
				data[k] = v
			}
			if summary, err := x.Render(s.Summary, data); err == nil {
				res.Summary = summary
			}
		}
		return res, nil
	}
	return t
}

const agentSchema = `{
  "type": "object",
  "properties": {
    "level": {"type": "string", "title": "Level", "ref": "ai.chat.levels#levels"},
    "tools": {"type": "array", "title": "Tools", "description": "The tools it may call, e.g. kb.search; empty allows every tool that does not ask. A joined Tools set wins.", "items": {"type": "string"}},
    "maxSteps": {"type": "integer", "title": "Steps", "min": 1, "max": 200, "description": "How many times it may call tools before it must answer; empty keeps the Tools settings'"}
  }
}`
