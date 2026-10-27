package chatdto

import (
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/rest"
)

type CompleteResponse struct {
	rest.Status
	Reply          *Message `json:"reply,omitempty"`
	Model          string   `json:"model,omitempty"`
	Level          string   `json:"level,omitempty"`
	FinishReason   string   `json:"finishReason,omitempty"`
	InputTokens    int      `json:"inputTokens,omitempty"` // includes cachedTokens
	CachedTokens   int      `json:"cachedTokens,omitempty"`
	OutputTokens   int      `json:"outputTokens,omitempty"` // excludes thinkingTokens
	ThinkingTokens int      `json:"thinkingTokens,omitempty"`
}

// CompleteResponseFrom converts the service's CompleteResponse; its code is
// carried by the HTTP status rather than the body.
func CompleteResponseFrom(resp chatservice.CompleteResponse) CompleteResponse {
	out := CompleteResponse{
		Status:         rest.StatusFrom(resp.Common),
		Model:          resp.Model,
		Level:          resp.Level,
		FinishReason:   resp.FinishReason,
		InputTokens:    resp.Usage.InputTokens,
		CachedTokens:   resp.Usage.CachedTokens,
		OutputTokens:   resp.Usage.OutputTokens,
		ThinkingTokens: resp.Usage.ThinkingTokens,
	}
	if resp.Message.Role != "" {
		reply := MessageFrom(resp.Message)
		out.Reply = &reply
	}
	return out
}
