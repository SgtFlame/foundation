// Package chroniclecontext pairs Chronicle memory with chat
// (documentation/Chronicle.md): the chronicle chat.contextProvider holds
// the conversation's Chronicles settings, which the browser's Chronicles
// control (id chronicle) edits and the Personal mode graph's Chronicle
// nodes read (forge 06i: the mode graph, not the provider, chooses the
// entries and updates memory); the chronicle chat.roundFollower flags what
// a 🚫 ignored round changed.
package chroniclecontext

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/steps"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
)

// ID pairs the provider and the follower with the browser's Chronicles control.
const ID = "chronicle"

var log = logging.In("chronicle")

// Deps are what the follower needs, once it exists.
type Deps struct {
	Steps chroniclesteps.Steps // its DB flags entries
}

// Contribute registers the provider and the follower. deps returns what
// they need when a round runs.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	conversation.ContextProviders.Register(reg, conversation.ContextProvider{
		Descriptor: extensions.Descriptor{ID: ID, Order: 50, Source: "chronicle"},
		Title:      "Chronicles",
		Schema:     json.RawMessage(schema),
		Defaults:   json.RawMessage(`{"enabled": true, "topK": 5, "match": true, "update": true}`),
	})
	conversation.RoundFollowers.Register(reg, conversation.RoundFollower{
		Descriptor: extensions.Descriptor{ID: ID, Order: 100, Source: "chronicle"},
		Title:      "Chronicle memory update",
		Feedback: func(args features.ServiceArgs, round conversation.Round, _, to retrieval.Feedback) {
			flag(args, round, to, deps())
		},
	})
}

// flag marks the entries a round made or changed for review when the round
// is 🚫 ignored: they are kept, but may hold what the user wants forgotten.
func flag(args features.ServiceArgs, round conversation.Round, to retrieval.Feedback, deps Deps) {
	if to != retrieval.FeedbackIgnore || deps.Steps.DB == nil {
		return
	}
	gdb := deps.Steps.DB.WithContext(args.Context())
	ids := gdb.Model(&chronicle.EntryRound{}).Select("entry_id").Where("tenant_id = ? AND round_id = ?", round.TenantID, round.ID)
	if err := chronicle.Owned(gdb, args.Principal).Model(&chronicle.Entry{}).Where("id IN (?)", ids).UpdateColumn("review", true).Error; err != nil {
		log.With("roundId", round.ID).Error(args.Context(), err, "cannot flag the entries round %d changed", round.ID)
	}
}

const schema = `{
  "type": "object",
  "properties": {
    "enabled": {"type": "boolean", "title": "Use Chronicle memory", "description": "Chooses the Chronicle entries each message needs and adds them, numbered, before any Volume passages."},
    "topK": {"type": "integer", "title": "Entries", "min": 1, "max": 20, "description": "How many entries context matching may choose, besides the ones checked in the Chronicles view"},
    "match": {"type": "boolean", "title": "Context matching", "description": "A fast model call (stage 1) chooses among the candidate entries; off keeps retrieval's ranking"},
    "update": {"type": "boolean", "title": "Update memory", "description": "After each reply, update the entries it used and propose new ones to accept"}
  }
}`
