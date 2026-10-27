package conversationservice

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Service runs chat conversations for AgentOS › Chat: it keeps each
// conversation and round of the principal, and streams replies from
// chatservice with the context the chat.contextProvider contributions
// gather. Conversations are private to their owner.
type Service interface {
	List(args ListArgs) ListResponse
	Rounds(args RoundsArgs) RoundsResponse
	Send(args SendArgs) *SendRequest
	Feedback(args FeedbackArgs) FeedbackResponse
	ContextProviders(args ContextProvidersArgs) ContextProvidersResponse
	Answer(args AnswerArgs) AnswerResponse
}

// ListArgs pages through the principal's conversations, newest first.
type ListArgs struct {
	features.ServiceArgs
	Before uint64   // only conversations older than this one; 0 starts at the newest
	Limit  int      // 5 when 0, at most 50
	IDs    []uint64 // optional; only these conversations (the Dashboard's top conversations)
}

type ListResponse struct {
	Common        features.ServiceResponse
	Conversations []conversation.Conversation
	More          bool // older conversations remain
}

// RoundsArgs names the conversation whose rounds to read, oldest first.
type RoundsArgs struct {
	features.ServiceArgs
	ConversationID uint64
}

type RoundsResponse struct {
	Common    features.ServiceResponse
	Rounds    []conversation.Round
	Segments  []retrieval.RoundSegment // what the rounds injected, by RoundID and Number
	ToolCalls []tools.ToolCall         // what the rounds' tool calls did, by RoundID, in order
}

// SendArgs sends one message. Cancelling args.Ctx stops the reply, which is
// kept as far as it got.
type SendArgs struct {
	features.ServiceArgs
	ConversationID uint64 // 0 starts a new conversation
	Text           string
	Level          string // an ai.chat.levels ID; the default level when empty
	// Workbench is the browser's workbench snapshot (WorkbenchContext.md),
	// stored with the round; untrusted.
	Workbench json.RawMessage
	// ContextSettings are the chat.contextSettings values by contribution
	// ID; a provider left out uses the conversation's last values, else its
	// defaults section.
	ContextSettings map[string]json.RawMessage
	// ClientTools are the browser's chat.clientTool contributions, offered
	// with the server's tools while the conversation's tools setting is on.
	// Their effect must be read or ui; the browser runs them.
	ClientTools []tools.Tool
}

// SendEventKind says what a SendEvent carries.
type SendEventKind string

const (
	EventStarted  SendEventKind = "started"  // Conversation and Round, as stored when the message was sent
	EventSources  SendEventKind = "sources"  // Segments, the context the round injected and may cite, before the reply
	EventDelta    SendEventKind = "delta"    // Text, the next piece of the reply
	EventDone     SendEventKind = "done"     // Round, completed; its Status says whether the reply failed
	EventTitle    SendEventKind = "title"    // Conversation, now with the title generated after its first round
	EventTool     SendEventKind = "tool"     // ToolCall, each time a call changes status: awaiting and client calls wait for Answer
	EventFollowed SendEventKind = "followed" // Follower and Followed: what a chat.roundFollower did after the reply
	EventFailed   SendEventKind = "failed"   // Common: the message was refused before a round was stored
)

type SendEvent struct {
	Kind         SendEventKind
	Conversation *conversation.Conversation
	Round        *conversation.Round
	Text         string
	Segments     []retrieval.RoundSegment
	Follower     string // the chat.roundFollower's ID
	Followed     *conversation.Followed
	ToolCall     *tools.ToolCall
	Common       features.ServiceResponse
}

// SendRequest delivers the round as it happens: started first (or failed,
// and nothing else), the deltas and tool calls (with sources again when a
// tool's result adds citations), done, possibly title, then what each
// follower did. Events is closed at the end.
type SendRequest struct {
	Events <-chan SendEvent
}

// FeedbackArgs sets the verdict on one of the principal's rounds. 👍 and
// 👎 move the counts of every segment the round injected (a changed verdict
// moves its vote); 🚫 drops the round from later rounds' history.
type FeedbackArgs struct {
	features.ServiceArgs
	RoundID  uint64
	Feedback retrieval.Feedback
}

type FeedbackResponse struct {
	Common features.ServiceResponse
	Round  conversation.Round
}

type ContextProvidersArgs struct {
	features.ServiceArgs
}

// ContextProvidersResponse lists the registered chat.contextProvider
// contributions, so the browser shows only the controls that have one.
type ContextProvidersResponse struct {
	Common    features.ServiceResponse
	Providers []ProviderInfo
}

type ProviderInfo struct {
	ID    string
	Title string
}

// AnswerArgs answers a tool call its round is waiting on: the user's
// decision on a write (Approval), or a client tool's Result or Error.
type AnswerArgs struct {
	features.ServiceArgs
	RoundID  uint64
	CallID   string
	Approval tools.Approval  // approved, always or denied, for an awaiting call
	Result   json.RawMessage // a client call's result, a JSON object
	Error    string          // a client call's failure, told to the model
}

type AnswerResponse struct {
	Common features.ServiceResponse
}
