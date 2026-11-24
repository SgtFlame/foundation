package conversationnodes

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/ai/settings"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/settings"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/SgtFlame/foundation/backend/features/tools/nodes"
)

// RunModeID is Run mode's node type (forge 15b).
const RunModeID = "chat.runMode"

// runModeConfig is Run mode's config.
type runModeConfig struct {
	Mode  string `json:"mode"`
	Level string `json:"level"`
	toolsnodes.ModelSettings
	Rag       toolsnodes.DialsConfig `json:"rag"`
	Chronicle *bool                  `json:"chronicle"`
	Tools     *bool                  `json:"tools"`
}

// ContributeRunMode registers Run mode, which runs a chat mode's graph
// inside another graph as one step, on a headless host
// (conversation.CallHost). deps are the Agent node's.
func ContributeRunMode(reg *extensions.Registry, deps func() toolsnodes.Deps) {
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: RunModeID, Order: 120, Source: source},
		Title:       "Run mode",
		Category:    "LLM",
		Kinds:       []string{cognition.KindAgent, cognition.KindFunction, "server.action", cognition.KindMode},
		Description: "Runs a chat mode's graph (such as Personal) as one step of this graph, for a message and optional earlier turns: the mode gathers context, builds its prompt and replies as it does in chat, with this node's level, model settings and Retrieval group (a preset or the dials). Nothing is kept: no round is stored, it names nothing, and Chronicle memory is read only as the run may (an agent's Memory of) and never changed. Tools that change data are denied unless the run is an agent's, whose own roles decide; there are no client tools. Its passages are numbered on the run's, so [n] stay unique across the run's model calls. Leaves by Failed with the error when the mode fails.",
		Example:     "Run mode Personal with the preset Explore ideas on the customer's question, then put its Reply in a draft",
		Examples:    []json.RawMessage{json.RawMessage(`{"type":"chat.runMode","config":{"mode":"personal","rag":{"preset":"brainstorm"},"chronicle":true,"tools":true}}`)},
		Tags:        []string{"chat", "mode", "llm", "retrieval", "agents"},
		Effects:     true,
		External:    true,
		Model:       true,
		Sample:      cognition.Values{"reply": "(the mode's reply: not asked in a sandboxed run)", "calls": []any{}, "segments": []any{}, "usage": map[string]any{}, "error": ""},
		Config: json.RawMessage(`{"type":"object","required":["mode"],"properties":{
			"mode":{"type":"string","title":"Mode","ref":"chat.modes#modes","description":"The chat mode whose graph runs"},
			"level":{"type":"string","title":"Level","ref":"ai.chat.levels#levels","description":"The level asked; empty is the default level"},
			` + toolsnodes.ModelProps + `,
			` + toolsnodes.RetrievalProps + `,
			"chronicle":{"type":"boolean","title":"Chronicle memory","description":"Whether the mode may read Chronicle memory (as the run may); on by default. It never updates memory"},
			"tools":{"type":"boolean","title":"Tools","description":"Whether the mode offers its tools; on by default"}}}`),
		Pins: cognition.Pins{
			In: append([]cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.DataPin("message", "Message", "string", "The message the mode answers"),
				{ID: "messages", Name: "Messages", Type: TypeMessages, Optional: true, Description: "Earlier turns, oldest first: the user's and the assistant's in pairs"},
				{ID: "retrieval", Name: "Retrieval", Type: "json", Optional: true, Description: "Narrows its searches to the run's {sources, volumeIds}, as on the Agent node; never widens them"},
				{ID: "tools", Name: "Tools", Type: toolsnodes.TypeToolSet, Optional: true, Description: "Tools added to those the mode offers"},
				{ID: "level", Name: "Level", Type: "string", Optional: true, Description: "An ai.chat.levels id, in place of its config's"},
			}, toolsnodes.ModelPins()...),
			Out: []cognition.Pin{
				cognition.ExecPin(cognition.Then, ""),
				cognition.ExecPin("failed", "Failed"),
				cognition.DataPin("reply", "Reply", "string", "The mode's reply"),
				cognition.DataPin("calls", "Calls", "json", "The tool calls it made: name, status, summary, error, args and result"),
				cognition.DataPin("segments", "Segments", TypeSegments, "The passages it numbered, as the run cites them"),
				cognition.DataPin("usage", "Usage", "json", "What it cost: {steps, inputTokens, cachedTokens, outputTokens, thinkingTokens, costMicros, currency, level, model, temperature}"),
				cognition.DataPin("error", "Error", "string", "Why it failed, when it did"),
			},
		},
		Run: func(x *cognition.Exec, in cognition.Values) (cognition.Values, string, error) {
			return runMode(x, in, deps())
		},
	})
}

func runMode(x *cognition.Exec, in cognition.Values, d toolsnodes.Deps) (cognition.Values, string, error) {
	var c runModeConfig
	if err := x.Config(&c); err != nil {
		return nil, "", err
	}
	fail := func(msg string) (cognition.Values, string, error) {
		return cognition.Values{"reply": "", "calls": []any{}, "segments": []retrieval.RoundSegment{}, "usage": map[string]any{}, "error": msg}, "failed", nil
	}
	message, err := cognition.Get[string](in, "message")
	if err != nil {
		return nil, "", err
	}
	if l, _ := cognition.Get[string](in, "level"); l != "" {
		c.Level = l
	}
	m, err := toolsnodes.ModelSettingsOf(x, in)
	if err != nil {
		return nil, "", err
	}
	if d.Config == nil {
		return fail("Run mode cannot read the chat modes")
	}
	modes, err := config.Read[conversationsettings.Modes](d.Config, x.ServiceArgs, conversationsettings.ModesID)
	if err != nil {
		return nil, "", err
	}
	mode, ok := modes.Find(c.Mode)
	if !ok {
		return fail(fmt.Sprintf("there is no chat mode %q", c.Mode))
	}
	levels, err := config.Read[aisettings.Levels](d.Config, x.ServiceArgs, aisettings.LevelsID)
	if err != nil {
		return nil, "", err
	}
	level, ok := levels.Find(c.Level)
	if !ok {
		return fail(fmt.Sprintf("there is no chat level %q", c.Level))
	}
	window := 0
	if models, err := config.Read[aisettings.Models](d.Config, x.ServiceArgs, aisettings.ModelsID); err == nil {
		ref := level.ModelRef
		if m.Model != "" {
			ref = m.Model
		}
		if mm, ok := models.ByID(ref); ok {
			window = mm.ContextWindow
		}
	}
	turns, err := cognition.Get[[]chatservice.Message](in, "messages")
	if err != nil {
		return nil, "", err
	}
	sa := x.ServiceArgs
	opts, set, err := toolsnodes.RetrievalOf(x, d.Config, c.Rag, in["retrieval"])
	if err != nil {
		return nil, "", err
	}
	if !set {
		// The tenant's chat default, as an empty group means
		opts = toolsnodes.RetrievalDefaults(x, d.Config)
	}
	sa.Ctx = retrieval.WithOptions(sa.Context(), opts)
	passages := tools.PassagesFrom(sa.Context())
	if passages == nil {
		sa.Ctx, passages = tools.WithPassages(sa.Context())
	}
	on := func(b *bool) bool { return b == nil || *b }
	host := &conversation.CallHost{
		Args: sa, Registry: d.Registry, DB: d.DB, Chat: d.Chat, Config: d.Config,
		ModeID: mode.ID, ModeLabel: mode.Label, Message: message, Window: windowOf(turns),
		Level: level.ID, ContextWindow: window, Passages: passages,
		ProviderSettings: map[string]json.RawMessage{
			"workbench": json.RawMessage(`{"enabled":false}`),
			"debug":     json.RawMessage(`{"enabled":false}`),
			"rag":       toolsnodes.RAGSettings(opts),
			"chronicle": json.RawMessage(fmt.Sprintf(`{"enabled":%t,"update":false,"checked":[]}`, on(c.Chronicle))),
			"tools":     json.RawMessage(fmt.Sprintf(`{"enabled":%t}`, on(c.Tools))),
		},
	}
	var ca chatservice.CompleteArgs
	m.Apply(&ca)
	host.ModelRef, host.Temperature, host.MaxTokens, host.ThinkingBudget = ca.ModelRef, ca.Temperature, ca.MaxTokens, ca.ThinkingBudget
	if _, joined := in["tools"]; joined {
		extra, err := cognition.Get[[]tools.Spec](in, "tools")
		if err != nil {
			return nil, "", err
		}
		host.Extra = toolsnodes.Resolve(x, extra, tools.Available(d.Registry, x.ServiceArgs), nil)
	}
	inputs := cognition.Values{
		PinText: message, PinWindow: host.Window, PinSettings: host.ProviderSettings, PinWorkbench: json.RawMessage(`{}`),
		PinConversation: host.Turn().Conversation, PinTitle: mode.Label, PinLevel: level.ID, PinRound: host.Turn().Round,
	}
	_, runErr := x.Start(cognition.Start{ServiceArgs: sa, GraphID: mode.GraphRef, Event: EventUserMessage, Inputs: inputs, Host: host,
		Trigger: "runMode:" + x.Node.ID})
	out := host.Outcome()
	segments := out.Segments
	if segments == nil {
		segments = []retrieval.RoundSegment{}
	}
	values := cognition.Values{"reply": out.Reply, "calls": toolsnodes.Calls(out.Calls), "segments": segments,
		"usage": toolsnodes.UsageOf(x, d, level.ID, m, out.Model, out.Steps, out.Usage), "error": ""}
	switch {
	case runErr != nil:
		values["error"] = "the mode failed: " + runErr.Error()
		return values, "failed", nil
	case out.Failed != "":
		values["error"] = "the model failed: " + out.Failed
		return values, "failed", nil
	case strings.TrimSpace(out.Reply) == "":
		values["error"] = "the mode " + mode.Label + " ended without a reply"
		return values, "failed", nil
	}
	return values, cognition.Then, nil
}

// windowOf turns earlier turns into rounds: each user message with the
// assistant's reply after it.
func windowOf(turns []chatservice.Message) []conversation.Round {
	var out []conversation.Round
	for _, t := range turns {
		switch t.Role {
		case chatservice.RoleUser:
			out = append(out, conversation.Round{Text: t.Content})
		case chatservice.RoleAssistant:
			if len(out) == 0 {
				out = append(out, conversation.Round{})
			}
			out[len(out)-1].Reply += t.Content
		}
	}
	return out
}
