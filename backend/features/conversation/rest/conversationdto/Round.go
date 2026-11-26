package conversationdto

import (
	"encoding/json"
	"time"

	"github.com/SgtFlame/foundation/backend/features/conversation"
)

type Round struct {
	ID             uint64                        `json:"id,string"`
	ConversationID uint64                        `json:"conversationId,string"`
	Time           time.Time                     `json:"time"`
	Text           string                        `json:"text"`
	Reply          string                        `json:"reply"`
	Level          string                        `json:"level"`
	Model          string                        `json:"model"`
	Status         uint16                        `json:"status"` // 0 while streaming, 200 done, 499 stopped, else failed
	FinishReason   string                        `json:"finishReason,omitempty"`
	Error          string                        `json:"error,omitempty"`
	Workbench      json.RawMessage               `json:"workbench,omitempty"`
	Mode           string                        `json:"mode,omitempty"`     // the mode it ran in, e.g. builder
	Activity       []Gesture                     `json:"activity,omitempty"` // the person's gestures sent with the message, oldest first (forge 15d)
	PreMode        *conversation.PreModeDecision `json:"preMode,omitempty"`  // what the pre-mode decided before it ran (forge 15f)
	InputTokens    int                           `json:"inputTokens"`        // includes cachedTokens
	CachedTokens   int                           `json:"cachedTokens"`
	OutputTokens   int                           `json:"outputTokens"` // excludes thinkingTokens
	ThinkingTokens int                           `json:"thinkingTokens"`
	CostMicros     int64                         `json:"costMicros"`
	Currency       string                        `json:"currency,omitempty"`
	LatencyMs      int64                         `json:"latencyMs"`
	Feedback       string                        `json:"feedback,omitempty"`  // up, down or ignore; omitted when neutral
	Segments       []RoundSegment                `json:"segments,omitempty"`  // what the round injected, [1] first
	ToolCalls      []ToolCall                    `json:"toolCalls,omitempty"` // in the order the model made them
}

func RoundFrom(r conversation.Round) Round {
	out := Round{
		ID: r.ID, ConversationID: r.ConversationID, Time: r.CreatedAt, Text: r.Text, Reply: r.Reply, Level: r.Level, Model: r.ModelName, Mode: r.Mode,
		Status: r.Status, FinishReason: r.FinishReason, Error: r.Error,
		InputTokens: r.InputTokens, CachedTokens: r.CachedTokens, OutputTokens: r.OutputTokens, ThinkingTokens: r.ThinkingTokens,
		CostMicros: r.CostMicros, Currency: r.Currency, LatencyMs: r.LatencyMs, Feedback: r.Feedback,
	}
	for _, g := range r.Gestures() {
		out.Activity = append(out.Activity, GestureFrom(g))
	}
	if r.Workbench != "" {
		out.Workbench = json.RawMessage(r.Workbench)
	}
	if r.PreMode != "" {
		var d conversation.PreModeDecision
		if json.Unmarshal([]byte(r.PreMode), &d) == nil {
			out.PreMode = &d
		}
	}
	return out
}
