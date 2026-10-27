// Package usage records the tokens and cost of every AI call
// (documentation/plans/Usage.md). Decorators wrap the chat and embed
// providers, so every call is counted whichever service or feature made it;
// usageservice reads the records back.
package usage

import (
	"github.com/SgtFlame/foundation/backend/features/db"
	"github.com/SgtFlame/foundation/backend/features/events"
)

// Record is one AI call. Its cost is priced when it is written, from the
// model catalog (ai.models), so editing a price never rewrites history. No
// prompt or reply text is stored here; with the usage.capture setting on, a
// chat call's text goes to a Capture instead.
type Record struct {
	db.Model
	db.Tenanted
	UserID         uint64 `gorm:"index"` // 0 for background work
	Feature        string `gorm:"index"` // chat, embed, embed-index, embed-query, ...
	Provider       string
	ModelName      string `gorm:"column:model"` // the provider's model ID
	Level          string // the chat level; empty outside the chat levels
	ConversationID uint64 `gorm:"index"`
	RoundID        uint64
	FileID         uint64
	RunID          uint64 `gorm:"index"` // the Cognition run, and NodeID the node in it, that made the call
	NodeID         string
	InputTokens    int // includes CachedTokens
	CachedTokens   int
	OutputTokens   int
	ThinkingTokens int
	EmbedTokens    int
	Estimated      bool   // the provider reported no token count, so it was estimated from the text
	CostMicros     int64  // millionths of Currency
	Currency       string // empty when the model is not in the catalog, and CostMicros is 0
	LatencyMs      int64
	Status         uint16 // the call's HTTP status, so failed calls that still billed are visible
	Captured       bool   // a Capture holds the call's text, until it expires
}

// TableName keeps the plan's name for the table.
func (Record) TableName() string { return "usage_records" }

// Capture is the text of one chat call, kept for debugging while the
// usage.capture setting is on, and deleted once it is older than the
// setting's retention.
type Capture struct {
	db.Model
	db.Tenanted
	RecordID uint64 `gorm:"uniqueIndex"` // the Record of the call
	System   string
	Messages string // JSON: [{role, content}]
	Reply    string
}

// TableName keeps the plan's name for the table.
func (Capture) TableName() string { return "usage_captures" }

// Models are the usage feature's tables, for db.Migrate.
func Models() []any {
	return []any{&Record{}, &Capture{}}
}

// Events carries every record once it is stored: a VictoriaLogs sink and
// budget checks subscribe here later, without touching callers.
type Events = events.Bus[Record]

func NewEvents() *Events {
	return events.NewBus[Record]()
}
