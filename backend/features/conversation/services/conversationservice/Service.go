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
	//svc:stream
	Send(args SendArgs) *SendRequest
	Feedback(args FeedbackArgs) FeedbackResponse
	ContextProviders(args ContextProvidersArgs) ContextProvidersResponse
	Modes(args ModesArgs) ModesResponse
	Answer(args AnswerArgs) AnswerResponse
	SetMode(args SetModeArgs) SetModeResponse
	SetWorkspaces(args SetWorkspacesArgs) SetWorkspacesResponse
	// SaveToolCall keeps a tool call of a round or a run (an upsert by its
	// ID, which the caller sets), for a tool loop in any process (forge
	// 21d-2). Its tenant and owner are the principal's. Only Go code calls
	// it.
	//svc:idempotent
	SaveToolCall(args SaveToolCallArgs) SaveToolCallResponse
}

// ListArgs pages through the principal's conversations, newest first.
type ListArgs struct {
	features.ServiceArgs
	Before uint64   // only conversations older than this one; 0 starts at the newest
	Limit  int      // 5 when 0, at most 50
	IDs    []uint64 // optional; only these conversations (the Dashboard's top conversations)
	// Workspace lists only the conversations of one of the caller's
	// workspaces (forge 16d): its own and those shown there. It must be
	// theirs (workspace_not_found); empty with IDs lists any, empty
	// without them is their Default.
	Workspace string
}

type ListResponse struct {
	Common        features.ServiceResponse
	Conversations []conversation.Conversation
	More          bool // older conversations remain
	// Places are the listed conversations' workspaces, by conversation id
	// (forge 16d); empty when the server has no workspaces
	Places map[uint64]Places
}

// Places are the workspaces a conversation is listed in (forge 16d).
type Places struct {
	Own     string   // the workspace it belongs to (conversation.OwnWorkspace)
	ShownIn []string // the others it is shown in, in no order
}

// SetWorkspacesArgs moves one of the caller's conversations to another of
// their workspaces, or sets the others it is shown in (forge 16d: Move
// to…, Show in…). Every id must be one of the caller's workspaces.
type SetWorkspacesArgs struct {
	features.ServiceArgs
	ConversationID uint64
	Move           string    // the workspace it now belongs to; empty keeps it
	ShownIn        *[]string // every workspace it is shown in besides its own; nil keeps them
}

type SetWorkspacesResponse struct {
	Common       features.ServiceResponse
	Conversation conversation.Conversation
	Places       Places
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
	// Mode is a chat.modes ID (forge 06i); empty keeps the conversation's,
	// else the default mode. Switching it changes the next rounds' graph,
	// not the history.
	Mode string
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
	// Offered are the chat.modes ids the browser offers where the message
	// was sent (forge 15f), since offering follows the editor in focus.
	// The pre-mode chooses only among those that exist; with fewer than
	// two it does not run.
	Offered []string
	// ModeChosen says the person chose Mode for this message (the menu or
	// /mode since the last round), so the pre-mode does not run.
	ModeChosen bool
	// Undoes is a round of the conversation this message runs again (forge
	// 15f: Undo of a pre-mode switch, with Mode the mode it left): that
	// round leaves the history, and the pre-mode does not run.
	Undoes uint64
	// Workspace is the id of the workspace the browser tab shows (forge
	// 16c): the round searches its checked Volumes and always uses its
	// checked Chronicle entries. It must be the caller's own, else the
	// message is refused (workspace_not_found); empty is their Default.
	Workspace string
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
	EventRetry    SendEventKind = "retry"    // Retry: the provider was busy, and the step is tried again after a wait
	EventMode     SendEventKind = "mode"     // Switch and Round: the pre-mode switched the round's and the conversation's mode (forge 15f)
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
	Retry        *Retry
	Switch       *Switch
	Common       features.ServiceResponse
}

// Switch is a mode switch the pre-mode made (forge 15f).
type Switch struct {
	From, To string // chat.modes ids
	Reason   string
}

// Retry is a model call tried again because the provider was busy.
type Retry struct {
	Try    int // from 1
	WaitMs int64
	Status int // 429 or 503
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

type ModesArgs struct {
	features.ServiceArgs
}

// ModesResponse lists the chat.modes the principal sees, each with what its
// mode graph says of itself, so the composer offers them and shows only
// the context settings each uses.
type ModesResponse struct {
	Common features.ServiceResponse
	Modes  []ModeInfo
}

type ModeInfo struct {
	ID, Label, Icon, GraphRef string
	Default                   bool
	Focus                     *conversation.Focus
	Description               string   // the graph's
	Settings                  []string // the context settings it uses; empty uses them all
	Missing                   bool     // its graph does not exist
	// Forge 15h: the mode that makes modes, and a mode a person made (Made),
	// the principal's own (Mine, so the menu offers to edit its graph) or
	// shared with the tenant
	MakesModes, Made, Mine, Shared bool
}

// SetModeArgs switches one of the principal's conversations to a chat.modes
// mode (forge 15e), so its next rounds run in it without the send naming
// it, on any tab or device.
type SetModeArgs struct {
	features.ServiceArgs
	ConversationID uint64
	Mode           string
}

type SetModeResponse struct {
	Common       features.ServiceResponse
	Conversation conversation.Conversation
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

// SaveToolCallArgs is one tool call's record, its ID set.
type SaveToolCallArgs struct {
	features.ServiceArgs
	Call tools.ToolCall
}

type SaveToolCallResponse struct {
	Common features.ServiceResponse
}
