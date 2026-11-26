// Package conversation keeps chat conversations and their rounds, and
// declares the chat.contextProvider point that adds context to a round's
// prompt (documentation/ChatCompletion.md). conversationservice runs
// the rounds; Chronicle's pipeline (Chronicle.md) later runs inside Send.
package conversation

import (
	"time"

	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
)

// Conversation is one conversation in the Chat editor's continuous scroll,
// private to its owner.
type Conversation struct {
	db.Model
	db.Tenanted
	db.Owned
	Title           string    // generated after the first round; empty until then
	ContextSettings string    // JSON object: the chat.contextSettings values by contribution ID, as last sent
	AllowedTools    string    // JSON array: the write tools the user chose "Always allow" for here
	Mode            string    // the chat.modes id its rounds run in, as last sent (forge 06i)
	LastRoundAt     time.Time `gorm:"index"`
	// WorkspaceID is the owner's workspace it belongs to (forge 16d): the
	// one its first message was sent from, or the one it was moved to. An
	// empty or no longer existing id is their Default's (OwnWorkspace).
	WorkspaceID string `gorm:"index;not null;default:''"`
}

// ConversationWorkspace shows a conversation in another of its owner's
// workspaces besides its own (forge 16d: Show in…).
type ConversationWorkspace struct {
	db.Model
	db.Tenanted
	db.Owned
	ConversationID uint64 `gorm:"uniqueIndex:conversation_workspace"`
	WorkspaceID    string `gorm:"uniqueIndex:conversation_workspace;index"`
}

// Round is one message and its reply. It is stored when the message is
// sent, so the reply's usage records can name it, and completed when the
// reply ends.
type Round struct {
	db.Model
	db.Tenanted
	db.Owned
	ConversationID uint64 `gorm:"index"`
	Text           string // the user's message
	Reply          string // the assistant's reply, partial when stopped or failed
	Level          string
	ModelName      string `gorm:"column:model"` // the provider's model ID
	Status         uint16 // 0 while the reply streams, 200 when done, StatusStopped, or the failure's status
	FinishReason   string
	Error          string // the failure's message, safe to show
	Workbench      string // the workbench snapshot sent with the message, JSON
	Mode           string // the chat.modes id it ran in (e.g. personal, builder)
	// Activity is the person's gestures sent with the message, JSON
	// []Gesture with their summaries, so a reload shows them after the
	// activity stream has dropped them (forge 15d)
	Activity string
	// PreMode is what the pre-mode decided before the round ran (forge
	// 15f), JSON PreModeDecision; empty when it did not run
	PreMode        string
	InputTokens    int // includes CachedTokens
	CachedTokens   int
	OutputTokens   int
	ThinkingTokens int
	CostMicros     int64  // millionths of Currency, priced as the usage record is
	Currency       string // empty when the model is not in the catalog
	LatencyMs      int64
	Feedback       string // a retrieval.Feedback: "", up, down or ignore
}

// PreModeDecision is what the pre-mode decided for a round (forge 15f),
// kept on Round.PreMode for its debug view and Undo.
type PreModeDecision struct {
	Decision   string  `json:"decision"` // what the graph said: stay, switch or ask
	Mode       string  `json:"mode"`     // the mode it named
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
	Ms         int64   `json:"ms"`              // how long it took
	From       string  `json:"from"`            // the mode the round was in before
	Outcome    string  `json:"outcome"`         // stayed, switched, asked-switched, asked-stayed, or timeout / failed
	Error      string  `json:"error,omitempty"` // why it stayed when it failed
}

// StatusStopped is a round the user stopped, keeping the reply so far
// (nginx's "client closed request").
const StatusStopped = 499

// InHistory reports whether the round is sent as history with later rounds.
func (r Round) InHistory() bool {
	return (r.Status == 200 || r.Status == StatusStopped) && r.Reply != "" && r.Feedback != string(retrieval.FeedbackIgnore)
}

// Models are the conversation feature's tables, for db.Migrate.
func Models() []any {
	return []any{&Conversation{}, &Round{}, &ConversationWorkspace{}}
}
