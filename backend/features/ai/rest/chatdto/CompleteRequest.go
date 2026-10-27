package chatdto

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
)

type CompleteRequest struct {
	Level       string          `json:"level,omitempty"` // an ai.chat.levels ID; its settings fill what is not given
	Model       string          `json:"model,omitempty"`
	System      string          `json:"system,omitempty"`
	Messages    []Message       `json:"messages"`
	Temperature *float32        `json:"temperature,omitempty"`
	MaxTokens   *int            `json:"maxTokens,omitempty"`
	JSONSchema  json.RawMessage `json:"jsonSchema,omitempty"`
}

// ToArgs converts the request to the service's CompleteArgs.
func (r CompleteRequest) ToArgs(args features.ServiceArgs) chatservice.CompleteArgs {
	messages := make([]chatservice.Message, len(r.Messages))
	for i, m := range r.Messages {
		messages[i] = m.ToMessage()
	}
	return chatservice.CompleteArgs{
		ServiceArgs: args,
		Level:       r.Level,
		Model:       r.Model,
		System:      r.System,
		Messages:    messages,
		Temperature: r.Temperature,
		MaxTokens:   r.MaxTokens,
		JSONSchema:  r.JSONSchema,
	}
}
