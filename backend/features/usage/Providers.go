package usage

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/providers"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/ai/services/embedservice"
	"github.com/SgtFlame/foundation/backend/features/ai/settings"
)

// ChatProvider records every call p makes. name is the provider's ID in the
// model catalog, e.g. "gemini".
func ChatProvider(p chatservice.Provider, name string, r *Recorder) chatservice.Provider {
	return &chatProvider{Provider: p, name: name, recorder: r}
}

// EmbedProvider records every call p makes.
func EmbedProvider(p embedservice.Provider, name string, r *Recorder) embedservice.Provider {
	return &embedProvider{Provider: p, name: name, recorder: r}
}

type chatProvider struct {
	chatservice.Provider
	name     string
	recorder *Recorder
}

func (c *chatProvider) Complete(ctx context.Context, in chatservice.ChatInput) (chatservice.ChatOutput, error) {
	start := time.Now()
	out, err := c.Provider.Complete(ctx, in)
	c.record(ctx, in, out, err, start)
	return out, err
}

func (c *chatProvider) Stream(ctx context.Context, in chatservice.ChatInput, onDelta func(chatservice.Delta) error) (chatservice.ChatOutput, error) {
	start := time.Now()
	out, err := c.Provider.Stream(ctx, in, onDelta)
	c.record(ctx, in, out, err, start)
	return out, err
}

func (c *chatProvider) record(ctx context.Context, in chatservice.ChatInput, out chatservice.ChatOutput, err error, start time.Time) {
	u := out.Usage
	rec := newRecord(ctx, "chat", c.name, in.Model, err, start)
	rec.InputTokens, rec.CachedTokens, rec.OutputTokens, rec.ThinkingTokens = u.InputTokens, u.CachedTokens, u.OutputTokens, u.ThinkingTokens
	tokens := aisettings.Tokens{Input: u.InputTokens, Cached: u.CachedTokens, Output: u.OutputTokens, Thinking: u.ThinkingTokens}
	if !c.recorder.Capturing(ctx) {
		c.recorder.Record(ctx, rec, tokens)
		return
	}
	messages := make([]capturedMessage, len(in.Messages))
	for i, m := range in.Messages {
		messages[i] = capturedMessage{Role: string(m.Role), Content: m.Content, ToolCalls: capturedCalls(m.ToolCalls)}
		for _, r := range m.Results {
			messages[i].Results = append(messages[i].Results, capturedResult{Name: r.Name, Result: r.Result})
		}
	}
	encoded, _ := json.Marshal(messages)
	reply := out.Content
	if calls := capturedCalls(out.ToolCalls); len(calls) > 0 {
		b, _ := json.Marshal(calls)
		reply += "\n\ntool calls: " + string(b)
	}
	c.recorder.RecordCaptured(ctx, rec, tokens, Capture{System: in.System, Messages: string(encoded), Reply: reply})
}

// capturedMessage is one message in Capture.Messages, with the tool calls
// an assistant message made and the results a tool message carries.
type capturedMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []capturedCall   `json:"toolCalls,omitempty"`
	Results   []capturedResult `json:"results,omitempty"`
}

type capturedCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type capturedResult struct {
	Name   string          `json:"name"`
	Result json.RawMessage `json:"result"`
}

func capturedCalls(calls []chatservice.ToolCall) []capturedCall {
	var out []capturedCall
	for _, c := range calls {
		out = append(out, capturedCall{Name: c.Name, Args: c.Args})
	}
	return out
}

type embedProvider struct {
	embedservice.Provider
	name     string
	recorder *Recorder
}

func (e *embedProvider) Embed(ctx context.Context, in embedservice.EmbedInput) (embedservice.EmbedOutput, error) {
	start := time.Now()
	out, err := e.Provider.Embed(ctx, in)
	feature := "embed"
	if in.Purpose == embedservice.PurposeQuery {
		feature = "embed-query"
	}
	rec := newRecord(ctx, feature, e.name, in.Model, err, start)
	rec.EmbedTokens = out.Usage.InputTokens
	if err == nil && rec.EmbedTokens == 0 {
		// The Gemini API reports no token count for embeddings; about four
		// characters a token is close enough to see what indexing costs
		for _, t := range in.Texts {
			rec.EmbedTokens += (len(t) + 3) / 4
		}
		rec.Estimated = true
	}
	e.recorder.Record(ctx, rec, aisettings.Tokens{Embed: rec.EmbedTokens})
	return out, err
}

// newRecord fills what every record has from the call context that
// ServiceArgs.CallContext made: the principal and the work's Meta.
func newRecord(ctx context.Context, feature, provider, model string, err error, start time.Time) Record {
	p, meta, ok := features.CallFrom(ctx)
	if !ok {
		p = features.ServicePrincipal(features.LocalTenantID)
	}
	meta = meta.WithFeature(feature)
	rec := Record{
		Feature: meta.Feature, Provider: provider, ModelName: model, Level: meta.Level,
		ConversationID: meta.ConversationID, RoundID: meta.RoundID, FileID: meta.FileID,
		RunID: meta.RunID, NodeID: meta.NodeID,
		LatencyMs: time.Since(start).Milliseconds(), Status: http.StatusOK, UserID: p.UserID,
	}
	rec.TenantID = p.TenantID
	if err != nil {
		rec.Status = providers.Code(err)
	}
	return rec
}
