// Package conversation keeps chat conversations and their rounds, and
// declares the chat.contextProvider point that adds context to a round's
// prompt (documentation/plans/ChatCompletion.md). conversationservice runs
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
	LastRoundAt     time.Time `gorm:"index"`
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
	InputTokens    int    // includes CachedTokens
	CachedTokens   int
	OutputTokens   int
	ThinkingTokens int
	CostMicros     int64  // millionths of Currency, priced as the usage record is
	Currency       string // empty when the model is not in the catalog
	LatencyMs      int64
	Feedback       string // a retrieval.Feedback: "", up, down or ignore
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
	return []any{&Conversation{}, &Round{}}
}
