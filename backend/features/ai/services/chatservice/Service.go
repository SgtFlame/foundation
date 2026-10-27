package chatservice

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
)

// Service completes chat conversations. It is provider-neutral: the provider
// (Gemini, OpenAI, Azure AI, ...) is chosen when the service is created.
type Service interface {
	Complete(args CompleteArgs) CompleteResponse
	Stream(args StreamArgs) *StreamRequest
}

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool" // the results of the assistant message's ToolCalls before it
)

// Message is one turn. An assistant message may carry ToolCalls besides (or
// instead of) Content; the tool message after it carries their Results, one
// per call.
type Message struct {
	Role      Role
	Content   string
	ToolCalls []ToolCall
	Results   []ToolResult
}

// ToolSpec declares a function the model may call.
type ToolSpec struct {
	Name        string // the provider's allowed characters: letters, digits, _ and -
	Description string
	Parameters  json.RawMessage // a JSON Schema object; empty takes no arguments
}

// ToolCall is a call the model asked for.
type ToolCall struct {
	ID   string // the provider's call ID; may be empty
	Name string
	Args json.RawMessage // a JSON object
	// Signature is the provider's opaque state for the call (Gemini's thought
	// signature), sent back with it in the history.
	Signature []byte
}

// ToolResult answers one ToolCall.
type ToolResult struct {
	CallID string
	Name   string
	Result json.RawMessage // a JSON object
}

// ToolChoice says whether the model must call a tool.
const (
	ToolChoiceAuto     = ""         // the model decides
	ToolChoiceNone     = "none"     // no calls, though the tools are declared
	ToolChoiceRequired = "required" // at least one call
)

type CompleteArgs struct {
	features.ServiceArgs
	// Level is an ai.chat.levels ID; its model and generation settings are
	// the defaults for the fields below. Without a level, the provider's
	// default model is used.
	Level       string
	Model       string // optional; the level's model, else the provider's default
	System      string // system instructions, kept apart from Messages
	Messages    []Message
	Temperature *float32
	MaxTokens   *int
	JSONSchema  json.RawMessage // optional; asks for a JSON reply matching this schema
	// Tools the model may call (documentation/plans/Tools.md); its calls come
	// back in the response's Message.ToolCalls, and tools.Loop runs them.
	Tools      []ToolSpec
	ToolChoice string // a ToolChoice constant
}

// Usage counts a call's tokens as the provider reports them. InputTokens
// includes CachedTokens; OutputTokens excludes ThinkingTokens.
type Usage struct {
	InputTokens    int
	CachedTokens   int
	OutputTokens   int
	ThinkingTokens int
}

type CompleteResponse struct {
	Common       features.ServiceResponse
	Message      Message
	Model        string // the provider's model ID
	Level        string // the level used, if any
	FinishReason string // e.g. "stop", "length", "safety"; provider-specific values are passed through lower-cased
	Usage        Usage
}

type StreamArgs = CompleteArgs

// Delta is the next piece of the assistant's reply. Tool calls are not
// streamed: they come whole in the response.
type Delta struct {
	Text string
}

// StreamRequest delivers the reply as it is generated: Deltas is closed when
// generation ends, after which Response receives the complete reply.
type StreamRequest struct {
	Deltas   <-chan Delta
	Response chan *CompleteResponse
}
