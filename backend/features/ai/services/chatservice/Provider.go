package chatservice

import "context"

// Provider is one AI vendor's chat completion API.
type Provider interface {
	ChatModel() string
	// Complete and Stream return errors as *providers.Error so they carry a
	// status. Stream calls onDelta for each piece of the reply as it arrives
	// and stops when onDelta returns an error.
	Complete(ctx context.Context, in ChatInput) (ChatOutput, error)
	Stream(ctx context.Context, in ChatInput, onDelta func(Delta) error) (ChatOutput, error)
}

// ChatInput is CompleteArgs after validation, with the model resolved.
type ChatInput struct {
	Model       string
	System      string
	Messages    []Message
	Temperature *float32
	MaxTokens   *int
	// ThinkingBudget caps the model's thinking tokens: -1 lets the model
	// decide and 0 turns thinking off where the model allows; nil is the
	// provider's default.
	ThinkingBudget *int
	JSONSchema     []byte
	Tools          []ToolSpec
	ToolChoice     string
}

type ChatOutput struct {
	Content      string
	ToolCalls    []ToolCall
	FinishReason string
	Usage        Usage
}
