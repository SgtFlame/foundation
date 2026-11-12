package impl

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/ai/settings"
	"github.com/SgtFlame/foundation/backend/features/config"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/services/conversationservice"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/tools"
	"github.com/SgtFlame/foundation/backend/features/tools/settings"
)

// host is the round a mode graph runs for (conversation.Host): its nodes
// stream the reply, store the citations, name the conversation and report
// follow-ups through it.
type host struct {
	s      *service
	args   conversationservice.SendArgs
	r      *round
	events chan<- conversationservice.SendEvent

	mu       sync.Mutex
	said     strings.Builder // text said before the reply, or instead of one
	replying bool
	finished bool
}

func newHost(s *service, args conversationservice.SendArgs, r *round, events chan<- conversationservice.SendEvent) *host {
	return &host{s: s, args: args, r: r, events: events}
}

// emit sends an event unless the browser has gone.
func (h *host) emit(e conversationservice.SendEvent) {
	select {
	case h.events <- e:
	case <-h.args.Context().Done():
	}
}

func (h *host) Turn() conversation.Turn {
	return conversation.Turn{Conversation: h.r.conversation, Round: h.r.round, Window: h.r.history, Workbench: h.args.Workbench,
		Level: h.r.level.ID, ContextWindow: h.r.model.ContextWindow}
}

func (h *host) Settings(provider string) json.RawMessage {
	var defaults json.RawMessage
	for _, p := range conversation.ContextProviders.All(h.s.registry) {
		if p.ID == provider {
			defaults = p.Defaults
		}
	}
	return h.s.settingsOf(h.args, h.r, provider, defaults)
}

func (h *host) Gather(providers []string) []conversation.ContextItem {
	return h.s.gather(h.args, h.r, providers)
}

func (h *host) AddContext(system string, items []conversation.ContextItem, header, cite string) (string, []retrieval.RoundSegment) {
	h.mu.Lock()
	start := len(h.r.segments)
	h.mu.Unlock()
	out, cited := prompt(system, items, header, cite, start)
	segments := h.s.storeSegments(h.args, h.r, cited)
	if len(segments) > 0 {
		h.mu.Lock()
		h.r.segments = append(h.r.segments, segments...)
		all := append([]retrieval.RoundSegment(nil), h.r.segments...)
		h.mu.Unlock()
		h.emit(conversationservice.SendEvent{Kind: conversationservice.EventSources, Segments: all})
	}
	return out, segments
}

func (h *host) ServerTools() []tools.Tool { return tools.Tools.All(h.s.registry) }

func (h *host) ClientTools() []tools.Tool { return h.args.ClientTools }

var errReplied = errors.New("the round has already replied: a mode graph streams one reply")

func (h *host) Say(text string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.finished || h.replying {
		return errReplied
	}
	h.said.WriteString(text)
	h.emit(conversationservice.SendEvent{Kind: conversationservice.EventDelta, Text: text})
	return nil
}

func (h *host) SetTitle(title string) {
	h.r.conversation.Title = title
	persist := context.WithoutCancel(h.args.Context())
	if err := h.s.db.WithContext(persist).Model(&h.r.conversation).Update("title", title).Error; err != nil {
		log.With("conversationId", h.r.conversation.ID).Error(persist, err, "cannot store the title of conversation %d", h.r.conversation.ID)
	}
	titled := h.r.conversation
	h.emit(conversationservice.SendEvent{Kind: conversationservice.EventTitle, Conversation: &titled})
}

func (h *host) Followed(follower string, f conversation.Followed) {
	h.emit(conversationservice.SendEvent{Kind: conversationservice.EventFollowed, Follower: follower, Followed: &f})
}

// Reply runs the tool loop, streamed, and completes the round.
func (h *host) Reply(a conversation.ReplyArgs) (conversation.ReplyOutcome, error) {
	h.mu.Lock()
	if h.finished || h.replying {
		h.mu.Unlock()
		return conversation.ReplyOutcome{}, errReplied
	}
	h.replying = true
	said := h.said.String()
	h.mu.Unlock()

	r, args := h.r, h.args
	level, model := r.level, r.model
	if a.Level != "" && a.Level != level.ID {
		l, ok := r.levels.Find(a.Level)
		if !ok {
			return conversation.ReplyOutcome{}, log.Code("unknown_level").With("level", a.Level).Errorf("there is no chat level %q", a.Level)
		}
		m, ok := r.models.ByID(l.ModelRef)
		if !ok {
			return conversation.ReplyOutcome{}, log.Code("unknown_model").With("level", l.ID).Errorf("level %q uses %q, which is not in the model catalog", l.ID, l.ModelRef)
		}
		level, model = l, m
		r.round.Level, r.round.ModelName = l.ID, m.Model
	}
	messages := a.Messages
	if len(messages) == 0 {
		for _, w := range r.history {
			messages = append(messages, chatservice.Message{Role: chatservice.RoleUser, Content: w.Text}, chatservice.Message{Role: chatservice.RoleAssistant, Content: w.Reply})
		}
		messages = append(messages, chatservice.Message{Role: chatservice.RoleUser, Content: r.round.Text})
	}
	settings, err := config.Read[toolssettings.Tools](h.s.config, args.ServiceArgs, toolssettings.ID)
	if err != nil {
		settings = toolssettings.Defaults
	}
	if a.MaxSteps > 0 {
		settings.MaxSteps = a.MaxSteps
	}
	call := args.ServiceArgs
	call.Meta = features.Meta{Feature: "chat", Level: level.ID, ConversationID: r.conversation.ID, RoundID: r.round.ID, Debug: r.debug}
	start := time.Now()
	c := &citer{s: h.s, args: args, r: r, emit: h.emit}
	out := tools.Loop{DB: h.s.db, Chat: h.s.chat}.Run(tools.RunArgs{
		CompleteArgs: chatservice.CompleteArgs{ServiceArgs: call, Level: level.ID, System: a.System, Messages: messages},
		Tools:        a.Tools,
		Settings:     settings,
		Allowed:      allowed(r),
		Gate:         gate{s: h.s, args: args, r: r},
		Stream:       true,
		OnDelta: func(text string) {
			h.emit(conversationservice.SendEvent{Kind: conversationservice.EventDelta, Text: text})
		},
		OnCall: func(tc tools.ToolCall) {
			if tc.Status == tools.StatusAwaiting || tc.Status == tools.StatusClient {
				h.s.expect(args.Principal, tc.RoundID, tc.CallID)
			}
			h.emit(conversationservice.SendEvent{Kind: conversationservice.EventTool, ToolCall: &tc})
		},
		Cite: c.cite,
		OnRetry: func(try int, wait time.Duration, status int) {
			h.emit(conversationservice.SendEvent{Kind: conversationservice.EventRetry, Retry: &conversationservice.Retry{Try: try, WaitMs: wait.Milliseconds(), Status: status}})
		},
	})
	resp := out.Response

	done := &r.round
	done.LatencyMs = time.Since(start).Milliseconds()
	done.Reply = said + out.Reply
	// Every step is priced as usage.Recorder prices its record
	var tokens aisettings.Tokens
	for _, u := range out.Usage {
		t := aisettings.Tokens{Input: u.InputTokens, Cached: u.CachedTokens, Output: u.OutputTokens, Thinking: u.ThinkingTokens}
		done.CostMicros += model.Pricing.CostMicros(t)
		tokens.Input, tokens.Cached, tokens.Output, tokens.Thinking = tokens.Input+t.Input, tokens.Cached+t.Cached, tokens.Output+t.Output, tokens.Thinking+t.Thinking
	}
	done.InputTokens, done.CachedTokens, done.OutputTokens, done.ThinkingTokens = tokens.Input, tokens.Cached, tokens.Output, tokens.Thinking
	if done.Currency = model.Pricing.Currency; done.Currency == "" {
		done.Currency = "USD"
	}
	switch {
	case args.Context().Err() != nil:
		done.Status, done.FinishReason = conversation.StatusStopped, "stopped"
	case resp.Common.Code >= 300:
		done.Status, done.Error = resp.Common.Code, resp.Common.Message
	default:
		done.Status, done.FinishReason, done.ModelName = http.StatusOK, resp.FinishReason, resp.Model
	}
	h.mu.Lock()
	h.finished = true
	segments := append([]retrieval.RoundSegment{}, r.segments...)
	h.mu.Unlock()
	h.s.complete(args, done, h.emit)
	return conversation.ReplyOutcome{Round: *done, OK: done.Status == http.StatusOK, Calls: out.Calls, Segments: segments}, nil
}

// finish completes a round the mode graph did not reply to: with what it
// said, else as failed with the graph's error, or stopped.
func (h *host) finish(err error) {
	h.mu.Lock()
	if h.finished || h.replying {
		h.mu.Unlock()
		return
	}
	h.finished = true
	said := h.said.String()
	h.mu.Unlock()
	done := &h.r.round
	done.Reply = said
	switch {
	case h.args.Context().Err() != nil:
		done.Status, done.FinishReason = conversation.StatusStopped, "stopped"
	case err != nil:
		done.Status, done.Error = http.StatusInternalServerError, clip("The chat mode failed: "+err.Error(), 2000)
	case said == "":
		done.Status, done.Error = http.StatusInternalServerError, "The chat mode "+h.r.mode.Label+" ended without a reply."
	default:
		done.Status, done.FinishReason = http.StatusOK, "said"
	}
	h.s.complete(h.args, done, h.emit)
}
