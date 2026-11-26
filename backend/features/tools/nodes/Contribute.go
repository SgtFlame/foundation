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
	"github.com/SgtFlame/foundation/backend/features"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/ai/settings"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
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
	CallToolID  = "tools.call"
	// RunPassagesID gives an agent run's numbered passages (forge 15a).
	RunPassagesID = "tools.runPassages"
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
	// Rag is the Retrieval group (forge 15b): how kb.search searches in this call
	Rag DialsConfig `json:"rag"`
}

// Contribute registers the nodes and the ToolSet type.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	cognition.Types.Register(reg, cognition.DataType{Descriptor: extensions.Descriptor{ID: TypeToolSet, Order: 300, Source: "tools"},
		Color: "#26a69a", Description: "Tools a model may call: server and client tools by name, maybe re-described, and graph tools"})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: AgentID, Order: 300, Source: "tools"},
		Title:       "Agent",
		Category:    "LLM",
		Description: "Asks the model, letting it call tools until it answers: its Tools set when joined, else its config's. Its model, temperature, max output tokens and thinking budget win over the level's. Its Retrieval group (a preset or the dials) says how kb.search searches in this call; the Retrieval input narrows it to the run's Volumes. Passages are numbered once per run, as chat numbers a round's: one found twice keeps its number. Tools that change data and ask first are denied (a run cannot ask for approval), unless the run is an agent's: then its own roles decide, and a call it lacks the rights for fails with forbidden.",
		Example:     "Agent with the prompt \"Summarize the open tasks\" and the kb.search tool",
		Effects:     true,
		External:    true,
		Model:       true,
		Sample:      cognition.Values{"reply": "(the model's reply: not asked in a sandboxed run)", "calls": []any{}, "usage": map[string]any{}},
		Config:      json.RawMessage(agentSchema),
		Pins: cognition.Pins{
			In: append([]cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("prompt", "Prompt", "string", "What to ask"),
				{ID: "system", Name: "System", Type: "string", Optional: true, Description: "The instructions"},
				{ID: "maxSteps", Name: "Steps", Type: "number", Optional: true, Description: "How many times it may call tools before it must answer, in place of its config's"},
				{ID: "tools", Name: "Tools", Type: TypeToolSet, Optional: true, Description: "The tools it may call, in place of its config's"},
				{ID: "level", Name: "Level", Type: "string", Optional: true, Description: "An ai.chat.levels id, in place of its config's (forge 7m: an agent record's level)"},
				{ID: "retrieval", Name: "Retrieval", Type: "json", Optional: true, Description: "Narrows how kb.search searches during the run (forge 15a, 15b): {sources, volumeIds} from an agent's record, never from the model. Its Volumes are searched, and its sources are intersected with the Retrieval group's: it never widens them. Whose memory the chronicle source reads is the run's principal's (Call graph's Memory of)"},
			}, ModelPins()...),
			Out: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("reply", "Reply", "string", "The model's answer"),
				cognition.DataPin("calls", "Calls", "json", "The tool calls it made: name, status, summary, error, args and the result the model saw (cut like the tools section's maxResultKB)"),
				cognition.DataPin("usage", "Usage", "json", "What the run cost: {steps, inputTokens, cachedTokens, outputTokens, thinkingTokens, costMicros, currency, level, model, temperature}, priced from ai.models as usage records are"),
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
		Description: "One model call without tools: the system instructions and the prompt in, the answer out, at a level, with its own model, temperature, max output tokens and thinking budget when set (the level fills the rest). With a JSON schema the reply is asked for as JSON matching it and given parsed on Value. Leaves by Failed with the error when the model fails or its JSON cannot be read",
		Example:     "Ask model at the chat section's title level to name the conversation from a Template of the exchange",
		Effects:     true,
		External:    true,
		Model:       true,
		Sample:      cognition.Values{"text": "(the model's answer: not asked in a sandboxed run)", "value": nil, "usage": map[string]any{}},
		Config: json.RawMessage(`{"type":"object","properties":{
			"level":{"type":"string","title":"Level","ref":"ai.chat.levels#levels","description":"The level asked; the Level pin wins, else the default level"},
			"feature":{"type":"string","title":"Feature","description":"What usage records it under, e.g. chat-title"},
			` + ModelProps + `,
			"jsonSchema":{"type":"string","title":"JSON schema","format":"multiline","description":"A JSON Schema the reply must match; the reply is then given parsed on Value"}}}`),
		Pins: cognition.Pins{
			In: append([]cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("prompt", "Prompt", "string", "What to ask"),
				{ID: "system", Name: "System", Type: "string", Optional: true, Description: "The instructions"},
				{ID: "level", Name: "Level", Type: "string", Optional: true, Description: "An ai.chat.levels id, in place of its config's"},
				{ID: "jsonSchema", Name: "JSON schema", Type: "json", Optional: true, Description: "A JSON Schema the reply must match, in place of its config's"},
			}, ModelPins()...),
			Out: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.ExecPin("failed", "Failed"),
				cognition.DataPin("text", "Text", "string", "The model's answer"),
				cognition.DataPin("value", "Value", cognition.Any, "With a JSON schema: the answer parsed; else nothing"),
				cognition.DataPin("usage", "Usage", "json", "What the call cost: {inputTokens, cachedTokens, outputTokens, thinkingTokens, costMicros, currency, level, model, temperature}"),
				cognition.DataPin("error", "Error", "string", "Why the model failed, when it did"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			return runComplete(x, in, deps())
		},
	})
	contributeToolSets(reg, deps)
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: RunPassagesID, Order: 345, Source: "tools"},
		Title:       "Run passages",
		Category:    "Tools",
		Description: "The passages the agent run this graph is part of has numbered so far, across all its model calls (forge 15a, 15b), as its tools found them: [{number, sourceId, key, title, entryId, volumeId, path, startLine, endLine}], numbers as the run's replies cite them. A tool an agent calls reads them to see what a text it is given cites, such as a draft citing a person's private memory (sourceId chronicle). Empty outside an agent's run.",
		Example:     "Run passages, then refuse to send a draft whose cited numbers name a chronicle passage",
		Tags:        []string{"tools", "citations", "agents", "retrieval"},
		Pins: cognition.Pins{Out: []cognition.Pin{
			cognition.DataPin("passages", "Passages", "json", "The run's numbered passages, in order; empty outside an agent's run"),
			cognition.DataPin("segments", "Segments", "json", "The same as a chat round's segments (forge 15b), for an agent run's record: [{number, sourceId, key, title, snippet, location: {volumeId, path, startLine, endLine, entryId}}], the snippet cut to 400 bytes and left out for Chronicle memory"),
		}},
		Run: func(x *cognition.Exec, _ cognition.Values) (cognition.Values, string, error) {
			out := []any{}
			segments := []any{}
			for _, s := range tools.PassagesFrom(x.Context()).All() {
				loc := map[string]any{"volumeId": "", "path": s.Location.Path, "startLine": s.Location.StartLine, "endLine": s.Location.EndLine, "entryId": ""}
				snippet := retrieval.Snippet(s.Text, 400)
				if s.Location.VolumeID != 0 {
					loc["volumeId"] = strconv.FormatUint(s.Location.VolumeID, 10)
				}
				if s.Location.EntryID != 0 {
					loc["entryId"] = strconv.FormatUint(s.Location.EntryID, 10)
				}
				if s.SourceID == "chronicle" {
					snippet = "" // a person's memory: its title only
				}
				segments = append(segments, map[string]any{"number": s.Number, "sourceId": s.SourceID, "key": s.Key, "title": s.Title, "snippet": snippet, "location": loc})
				p := map[string]any{"number": s.Number, "sourceId": s.SourceID, "key": s.Key, "title": s.Title, "path": s.Location.Path,
					"startLine": s.Location.StartLine, "endLine": s.Location.EndLine, "entryId": "", "volumeId": ""}
				if s.Location.EntryID != 0 {
					p["entryId"] = strconv.FormatUint(s.Location.EntryID, 10)
				}
				if s.Location.VolumeID != 0 {
					p["volumeId"] = strconv.FormatUint(s.Location.VolumeID, 10)
				}
				out = append(out, p)
			}
			return cognition.Values{"passages": out, "segments": segments}, "", nil
		},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: CallToolID, Order: 340, Source: "tools"},
		Title:       "Call tool",
		Category:    "Tools",
		Description: "Calls one server tool by name with arguments, as the run's principal, and gives its result; leaves by Failed with the error when there is no such tool, the arguments do not fit, the principal may not, or it fails. A tool that asks first (a write) is refused unless the run is an agent's, whose own roles decide (forge 7m: an agent graph reads its subject through the host app's Describe subject tool).",
		Example:     "Call tool mail.describeThread with {id} to put the thread's text in an agent's prompt",
		Tags:        []string{"tools", "function calling"},
		Effects:     true,
		Pins: cognition.Pins{
			In: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("name", "Name", "string", "The tool's name, such as tasks.readTask"),
				{ID: "args", Name: "Args", Type: cognition.Any, Optional: true, Description: "Its arguments, an object"},
			},
			Out: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.ExecPin("failed", "Failed"),
				cognition.DataPin("result", "Result", "json", "The result's content"),
				cognition.DataPin("summary", "Summary", "string", "The result's one line of markdown"),
				cognition.DataPin("error", "Error", "string", "Why it failed, when it did"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			name, _ := cognition.Get[string](in, "name")
			fail := func(msg string) (cognition.Values, string, error) {
				return cognition.Values{"error": msg, "result": nil, "summary": ""}, "failed", nil
			}
			var tool tools.Tool
			found := false
			for _, t := range tools.Available(deps().Registry, x.ServiceArgs) {
				if t.ID == name && !t.Client() {
					tool, found = t, true
					break
				}
			}
			if !found {
				return fail(fmt.Sprintf("there is no tool %q", name))
			}
			args := json.RawMessage("{}")
			if v, ok := in["args"]; ok && v != nil {
				raw, err := json.Marshal(v)
				if err != nil {
					return fail("the arguments cannot be read: " + err.Error())
				}
				args = raw
			}
			if err := tools.CheckArgs(tool.Input, args); err != nil {
				return fail(err.Error())
			}
			if tool.AsksFor(args) && x.Principal.Kind != features.PrincipalAgent {
				return fail(fmt.Sprintf("%s needs a person's approval, which a graph cannot ask for", name))
			}
			res, err := tool.Run(tools.Call{ServiceArgs: x.ServiceArgs, ID: fmt.Sprintf("%s-%s", x.Node.ID, name), Name: name, Args: args})
			if err != nil {
				return fail(err.Error())
			}
			content := map[string]any{}
			for k, v := range res.Content {
				content[k] = v
			}
			return cognition.Values{"result": content, "summary": res.Summary, "error": ""}, cognition.Then, nil
		},
	})
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// runComplete runs Ask model: one call, its settings over the level's.
func runComplete(x *cognition.Exec, in cognition.Values, d Deps) (cognition.Values, string, error) {
	var c struct {
		Level, Feature string
		JSONSchema     string `json:"jsonSchema"`
	}
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
	m, err := ModelSettingsOf(x, in)
	if err != nil {
		return nil, "", err
	}
	var schema json.RawMessage
	if strings.TrimSpace(c.JSONSchema) != "" {
		schema = json.RawMessage(c.JSONSchema)
	}
	if v, ok := in["jsonSchema"]; ok && v != nil {
		switch t := v.(type) {
		case string:
			if strings.TrimSpace(t) != "" {
				schema = json.RawMessage(t)
			}
		default:
			if schema, err = json.Marshal(t); err != nil {
				return nil, "", err
			}
		}
	}
	if len(schema) > 0 && !json.Valid(schema) {
		return cognition.Values{"error": "the JSON schema is not valid JSON", "text": "", "value": nil, "usage": map[string]any{}}, "failed", nil
	}
	if d.Chat == nil {
		return nil, "", tools.Log.Code("model_not_configured").New("Ask model has no chat service")
	}
	sa := x.ServiceArgs
	sa.Meta.Feature, sa.Meta.Level = cmpOr(c.Feature, cmpOr(sa.Meta.Feature, "graph")), c.Level
	args := chatservice.CompleteArgs{ServiceArgs: sa, Level: c.Level, System: system, JSONSchema: schema,
		Messages: []chatservice.Message{{Role: chatservice.RoleUser, Content: prompt}}}
	m.Apply(&args)
	resp := d.Chat.Complete(args)
	usage := UsageOf(x, d, resp.Level, m, resp.Model, 0, []chatservice.Usage{resp.Usage})
	if resp.Common.Code >= 300 {
		return cognition.Values{"error": resp.Common.Message, "text": "", "value": nil, "usage": usage}, "failed", nil
	}
	out := cognition.Values{"text": resp.Message.Content, "value": nil, "usage": usage}
	if len(schema) > 0 {
		var v any
		if err := json.Unmarshal([]byte(stripFence(resp.Message.Content)), &v); err != nil {
			out["error"] = "the model's answer is not JSON: " + err.Error()
			return out, "failed", nil
		}
		out["value"] = v
	}
	return out, cognition.Then, nil
}

// stripFence is a reply without the ```json fence some models add.
func stripFence(s string) string {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, "```") {
		t = strings.TrimPrefix(t, "```json")
		t = strings.TrimPrefix(t, "```")
		t = strings.TrimSuffix(strings.TrimSpace(t), "```")
	}
	return strings.TrimSpace(t)
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
	if l, _ := cognition.Get[string](in, "level"); l != "" {
		cfg.Level = l
	}
	m, err := ModelSettingsOf(x, in)
	if err != nil {
		return nil, "", err
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
		offered = Resolve(x, set, tools.Available(d.Registry, x.ServiceArgs), nil)
	} else {
		offered = tools.Enabled(tools.Available(d.Registry, x.ServiceArgs), nil, settings)
		// Writes listed by name are offered, and denied, so the trace says why
		agent := x.Principal.Kind == features.PrincipalAgent
		offered = slices.DeleteFunc(offered, func(t tools.Tool) bool {
			if len(cfg.Tools) == 0 {
				// An agent's writes are held to its roles, not asked (forge 07j)
				return t.Effect.Asks() && !agent
			}
			return !slices.Contains(cfg.Tools, t.ID)
		})
	}
	sa := x.ServiceArgs
	sa.Meta = sa.Meta.WithFeature("agent")
	// kb.search searches as the node's Retrieval group says, narrowed by the run's (forge 15b)
	if o, set, err := RetrievalOf(x, d.Config, cfg.Rag, in["retrieval"]); err != nil {
		return nil, "", tools.Log.Code("bad_retrieval").Wrap(err)
	} else if set {
		sa.Ctx = retrieval.WithOptions(sa.Context(), o)
	}
	// One numbering for the whole run, which the tools it calls see (forge 15a, 15b)
	kept := tools.PassagesFrom(sa.Context())
	if kept == nil {
		sa.Ctx, kept = tools.WithPassages(sa.Context())
	}
	args := chatservice.CompleteArgs{ServiceArgs: sa, Level: cfg.Level, System: system,
		Messages: []chatservice.Message{{Role: chatservice.RoleUser, Content: prompt}}}
	m.Apply(&args)
	out := tools.Loop{DB: d.DB, Chat: d.Chat}.Run(tools.RunArgs{
		CompleteArgs: args,
		Tools:        offered,
		Settings:     settings,
		Cite:         kept.Cite,
	})
	if c := out.Response.Common; c.Code >= 300 {
		return nil, "", tools.Log.Code("agent_failed").With("status", c.Code).Errorf("the model failed: %s", c.Message)
	}
	return cognition.Values{"reply": out.Reply, "calls": Calls(out.Calls), "usage": UsageOf(x, d, cfg.Level, m, out.Response.Model, out.Steps, out.Usage)}, cognition.Then, nil
}

// Calls are a loop's calls as a node gives them: name, status, summary,
// error, and (forge 7m, for an agent run's record) the arguments and the
// result the model saw, each parsed from its JSON.
func Calls(calls []tools.ToolCall) []map[string]any {
	out := make([]map[string]any, len(calls))
	for i, c := range calls {
		out[i] = map[string]any{"name": c.Name, "status": c.Status, "summary": c.Summary, "error": c.Error,
			"args": parsed(c.Args), "result": parsed(c.Result)}
	}
	return out
}

// parsed is a stored JSON value as data, the text itself when it is not
// JSON (a result cut short), nil when empty.
func parsed(raw string) any {
	if raw == "" {
		return nil
	}
	var v any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return raw
	}
	return v
}

// UsageOf sums a node's model calls and prices them from the model in
// ai.models (the node's own, else the level's), as conversation rounds and
// usage records are priced.
func UsageOf(x *cognition.Exec, d Deps, level string, m ModelSettings, model string, steps int, calls []chatservice.Usage) map[string]any {
	var t aisettings.Tokens
	for _, u := range calls {
		t.Input, t.Cached, t.Output, t.Thinking = t.Input+u.InputTokens, t.Cached+u.CachedTokens, t.Output+u.OutputTokens, t.Thinking+u.ThinkingTokens
	}
	var temperature any
	if m.Temperature != nil {
		temperature = *m.Temperature
	}
	res := map[string]any{"steps": steps, "inputTokens": t.Input, "cachedTokens": t.Cached, "outputTokens": t.Output,
		"thinkingTokens": t.Thinking, "costMicros": int64(0), "currency": "", "level": level, "model": model, "temperature": temperature}
	if d.Config == nil {
		return res
	}
	ref := m.Model
	levels, err := config.Read[aisettings.Levels](d.Config, x.ServiceArgs, aisettings.LevelsID)
	if err != nil {
		return res
	}
	if l, ok := levels.Find(level); ok {
		if ref == "" {
			ref = l.ModelRef
		}
		if temperature == nil && l.Temperature != nil {
			res["temperature"] = *l.Temperature
		}
	}
	models, err := config.Read[aisettings.Models](d.Config, x.ServiceArgs, aisettings.ModelsID)
	if err != nil {
		return res
	}
	if mm, ok := models.ByID(ref); ok {
		var cost int64
		for _, u := range calls {
			cost += mm.Pricing.CostMicros(aisettings.Tokens{Input: u.InputTokens, Cached: u.CachedTokens, Output: u.OutputTokens, Thinking: u.ThinkingTokens})
		}
		res["costMicros"], res["currency"] = cost, cmpOr(mm.Pricing.Currency, "USD")
	}
	return res
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
    ` + ModelProps + `,
    ` + RetrievalProps + `,
    "level": {"type": "string", "title": "Level", "ref": "ai.chat.levels#levels"},
    "tools": {"type": "array", "title": "Tools", "description": "The tools it may call, e.g. kb.search; empty allows every tool that does not ask. A joined Tools set wins.", "items": {"type": "string"}},
    "maxSteps": {"type": "integer", "title": "Steps", "min": 1, "max": 200, "description": "How many times it may call tools before it must answer; empty keeps the Tools settings'"}
  }
}`
