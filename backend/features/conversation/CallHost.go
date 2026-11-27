package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/SgtFlame/foundation/backend/features/tools/settings"
)

var callLog = logging.In("conversation").Tags("runMode")

// CallHost is a headless host (forge 15b): it stands in for a chat round
// when Run mode runs a chat mode's graph inside another graph.
//   - Its turn is the message and the window; its settings are Run mode's
//     config by provider id (the workbench off, rag resolved from the
//     Retrieval group), over the tenant's defaults.
//   - Its reply runs tools.Loop with no gate, under the Agent node's rules
//     (an agent's own roles decide its writes; there are no client tools),
//     numbering passages on the run's tools.Passages.
//   - Nothing is kept: Say adds to the reply, Set title and Emit followed
//     do nothing, no round is stored, and Chronicle's memory update does
//     nothing under it (Headless).
type CallHost struct {
	Args     features.ServiceArgs // the run's, with its retrieval options on the context
	Registry *extensions.Registry
	Calls    tools.CallStore // where its tool calls are kept (forge 21d-2)
	Chat     chatservice.Service
	Config   config.Reader

	ModeID, ModeLabel string
	Message           string
	Window            []Round
	Level             string // an ai.chat.levels id, resolved
	ContextWindow     int
	// Settings are the providers' settings by id, laid over their defaults
	ProviderSettings map[string]json.RawMessage
	// Extra are tools added to those the mode offers (Run mode's Tools input)
	Extra []tools.Tool
	// The model settings, over those the mode's Stream reply sets
	ModelRef       string
	Temperature    *float32
	MaxTokens      *int
	ThinkingBudget *int
	Passages       *tools.Passages

	mu       sync.Mutex
	said     strings.Builder
	replied  bool
	outcome  CallOutcome
	segments []retrieval.RoundSegment
}

// CallOutcome is what a CallHost's mode did.
type CallOutcome struct {
	Reply    string
	Calls    []tools.ToolCall
	Usage    []chatservice.Usage
	Model    string
	Steps    int
	Failed   string // why the reply failed, when it did
	Replied  bool
	Segments []retrieval.RoundSegment
}

// Headless says h is a CallHost: a host on which nothing is kept, such as
// Chronicle's memory update.
func Headless(h any) bool {
	_, ok := h.(*CallHost)
	return ok
}

// Outcome is what the mode did: its reply (what it said when it streamed
// none), calls, usage and the segments it numbered.
func (h *CallHost) Outcome() CallOutcome {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := h.outcome
	if !out.Replied {
		out.Reply = h.said.String()
	}
	out.Segments = slices.Clone(h.segments)
	return out
}

func (h *CallHost) Turn() Turn {
	return Turn{Conversation: Conversation{Title: h.ModeLabel, Mode: h.ModeID}, Round: Round{Text: h.Message, Level: h.Level},
		Window: h.Window, Level: h.Level, ContextWindow: h.ContextWindow}
}

// Settings are the provider's settings: Run mode's for it laid over the
// tenant's defaults section, else the provider's declared defaults.
func (h *CallHost) Settings(provider string) json.RawMessage {
	var base json.RawMessage
	if h.Config != nil {
		if v, err := h.Config.Resolved(h.Args, DefaultsSection(provider)); err == nil {
			base = v
		}
	}
	if len(base) == 0 {
		for _, p := range ContextProviders.All(h.Registry) {
			if p.ID == provider {
				base = p.Defaults
			}
		}
	}
	over, ok := h.ProviderSettings[provider]
	if !ok {
		return base
	}
	merged := map[string]any{}
	_ = json.Unmarshal(base, &merged)
	var o map[string]any
	_ = json.Unmarshal(over, &o)
	for k, v := range o {
		merged[k] = v
	}
	b, _ := json.Marshal(merged)
	return b
}

// Gather runs the named providers enabled by its settings, concurrently,
// on the run's context (so rag searches the run's Volumes).
func (h *CallHost) Gather(named []string) []ContextItem {
	providers := ContextProviders.All(h.Registry)
	results := make([][]ContextItem, len(providers))
	var wg sync.WaitGroup
	for i, p := range providers {
		if p.Gather == nil || (len(named) > 0 && !slices.Contains(named, p.ID)) {
			continue
		}
		settings := h.Settings(p.ID)
		var v struct {
			Enabled *bool `json:"enabled"`
		}
		_ = json.Unmarshal(settings, &v)
		if v.Enabled != nil && !*v.Enabled {
			continue
		}
		ga := GatherArgs{ServiceArgs: h.Args, Text: h.Message, Window: h.Window, Settings: settings, ContextWindow: h.ContextWindow}
		wg.Go(func() {
			items, err := p.Gather(ga)
			if err != nil {
				callLog.With("provider", p.ID).Warn(h.Args.Context(), "context provider %s failed, so Run mode goes without it: %v", p.ID, err)
				return
			}
			results[i] = items
		})
	}
	wg.Wait()
	return slices.Concat(results...)
}

// AddContext is the system prompt with the context, its citable items
// numbered on the run's passages.
func (h *CallHost) AddContext(system string, items []ContextItem, header, cite string) (string, []retrieval.RoundSegment) {
	out, cited := Prompt(system, items, header, cite, func(s retrieval.Scored) int {
		return h.Passages.Cite([]retrieval.Scored{s})[0].Number
	})
	segments := h.keep(cited)
	return out, segments
}

// keep adds numbered passages to the segments, once per number.
func (h *CallHost) keep(cited []retrieval.Scored) []retrieval.RoundSegment {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]retrieval.RoundSegment, 0, len(cited))
	for _, s := range cited {
		seg := retrieval.RoundSegmentFrom(0, db.Tenanted{TenantID: h.Args.Principal.TenantID}, s)
		out = append(out, seg)
		if !slices.ContainsFunc(h.segments, func(x retrieval.RoundSegment) bool { return x.Number == seg.Number }) {
			h.segments = append(h.segments, seg)
		}
	}
	return out
}

func (h *CallHost) ServerTools() []tools.Tool { return tools.Available(h.Registry, h.Args) }

// ClientTools are none: there is no browser.
func (h *CallHost) ClientTools() []tools.Tool { return nil }

var errCallReplied = errors.New("the mode has already replied: a mode graph streams one reply")

// Reply runs the tool loop without a gate.
func (h *CallHost) Reply(a ReplyArgs) (ReplyOutcome, error) {
	h.mu.Lock()
	if h.replied {
		h.mu.Unlock()
		return ReplyOutcome{}, errCallReplied
	}
	h.replied = true
	said := h.said.String()
	h.mu.Unlock()
	if h.Chat == nil {
		return ReplyOutcome{}, callLog.Code("model_not_configured").New("Run mode has no chat service")
	}
	messages := a.Messages
	if len(messages) == 0 {
		for _, w := range h.Window {
			messages = append(messages, chatservice.Message{Role: chatservice.RoleUser, Content: w.Text}, chatservice.Message{Role: chatservice.RoleAssistant, Content: w.Reply})
		}
		messages = append(messages, chatservice.Message{Role: chatservice.RoleUser, Content: h.Message})
	}
	settings := toolssettings.Defaults
	if h.Config != nil {
		if s, err := config.Read[toolssettings.Tools](h.Config, h.Args, toolssettings.ID); err == nil {
			settings = s
		}
	}
	if a.MaxSteps > 0 {
		settings.MaxSteps = a.MaxSteps
	}
	offered := slices.Clone(a.Tools)
	for _, t := range h.Extra {
		if !slices.ContainsFunc(offered, func(o tools.Tool) bool { return o.ID == t.ID }) {
			offered = append(offered, t)
		}
	}
	// Only server tools: there is no browser to run client ones
	offered = slices.DeleteFunc(offered, func(t tools.Tool) bool { return t.Client() })
	level := cmpOr(a.Level, h.Level)
	call := h.Args
	call.Meta = call.Meta.WithFeature("run-mode")
	call.Meta.Level = level
	args := chatservice.CompleteArgs{ServiceArgs: call, Level: level, System: a.System, Messages: messages,
		ModelRef: cmpOr(h.ModelRef, a.ModelRef), Temperature: firstOf(h.Temperature, a.Temperature),
		MaxTokens: firstOf(h.MaxTokens, a.MaxTokens), ThinkingBudget: firstOf(h.ThinkingBudget, a.ThinkingBudget)}
	out := tools.Loop{Calls: h.Calls, Chat: h.Chat}.Run(tools.RunArgs{
		CompleteArgs: args,
		Tools:        offered,
		Settings:     settings,
		Cite: func(segments []retrieval.Scored) []retrieval.Scored {
			numbered := h.Passages.Cite(segments)
			h.keep(numbered)
			return numbered
		},
	})
	h.mu.Lock()
	h.outcome = CallOutcome{Reply: said + out.Reply, Calls: out.Calls, Usage: out.Usage, Model: out.Response.Model, Steps: out.Steps, Replied: true}
	ok := out.Response.Common.Code < 300 && h.Args.Context().Err() == nil
	if !ok {
		h.outcome.Failed = cmpOr(out.Response.Common.Message, "stopped")
	}
	segments := slices.Clone(h.segments)
	h.mu.Unlock()
	round := Round{Text: h.Message, Reply: said + out.Reply, Level: level, ModelName: out.Response.Model}
	return ReplyOutcome{Round: round, OK: ok, Calls: out.Calls, Segments: segments}, nil
}

// Say adds text to the reply.
func (h *CallHost) Say(text string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.replied {
		return errCallReplied
	}
	h.said.WriteString(text)
	return nil
}

// SetTitle does nothing: there is no conversation.
func (h *CallHost) SetTitle(string) {}

// Followed does nothing: there is no browser.
func (h *CallHost) Followed(string, Followed) {}

// Context is the run's context.
func (h *CallHost) Context() context.Context { return h.Args.Context() }

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func firstOf[T any](a, b *T) *T {
	if a != nil {
		return a
	}
	return b
}
