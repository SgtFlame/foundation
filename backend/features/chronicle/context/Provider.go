// Package chroniclecontext puts Chronicle memory into chat rounds
// (documentation/plans/Chronicle.md) by running the built-in Personal
// Cognition graph (chronicle/nodes): the chronicle chat.contextProvider runs
// its On user message event (the Candidates step and stage 1) for each
// message and pins the entries it returns in the prompt; the chronicle
// chat.roundFollower runs its On reply event (the memory update) after each
// reply, and flags what a 🚫 ignored round changed. Both pair with the
// browser's Chronicles control (id chronicle).
package chroniclecontext

import (
	"encoding/json"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/chronicle"
	"github.com/SgtFlame/foundation/backend/features/chronicle/nodes"
	"github.com/SgtFlame/foundation/backend/features/chronicle/services/chronicleservice"
	"github.com/SgtFlame/foundation/backend/features/chronicle/steps"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/conversation"
	"github.com/SgtFlame/foundation/backend/features/conversation/nodes"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
)

// ID pairs the provider and the follower with the browser's Chronicles control.
const ID = "chronicle"

var log = logging.In("chronicle")

// Deps are what the provider and the follower need, once they exist.
type Deps struct {
	Steps   chroniclesteps.Steps     // what Personal's nodes run; its DB also flags entries
	Engine  *engine.Engine           // runs Personal
	Entries chronicleservice.Service // indexes the entries the memory update changed
}

// Contribute registers the provider and the follower. deps returns what
// they need when a round runs: the chat and retrieval services are created
// after the registry is complete.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	conversation.ContextProviders.Register(reg, conversation.ContextProvider{
		Descriptor: extensions.Descriptor{ID: ID, Order: 50, Source: "chronicle"},
		Title:      "Chronicles",
		Schema:     json.RawMessage(schema),
		Defaults:   json.RawMessage(`{"enabled": true, "topK": 5, "match": true, "update": true}`),
		Gather: func(args conversation.GatherArgs) ([]conversation.ContextItem, error) {
			return gather(args, deps())
		},
	})
	conversation.RoundFollowers.Register(reg, conversation.RoundFollower{
		Descriptor: extensions.Descriptor{ID: ID, Order: 100, Source: "chronicle"},
		Title:      "Chronicle memory update",
		Follow: func(args conversation.FollowArgs) (*conversation.Followed, error) {
			return follow(args, deps())
		},
		Feedback: func(args features.ServiceArgs, round conversation.Round, _, to retrieval.Feedback) {
			flag(args, round, to, deps())
		},
	})
}

// run runs one of Personal's events.
func run(args features.ServiceArgs, deps Deps, event string, inputs cognition.Values) (cognition.Values, error) {
	if deps.Engine == nil {
		return nil, nil
	}
	res, err := deps.Engine.Run(engine.RunArgs{ServiceArgs: args, GraphID: chroniclenodes.PersonalID, Event: event, Inputs: inputs, Trigger: "chat"})
	return res.Outputs, err
}

// gather runs Personal's On user message event, and returns the chosen
// entries as pinned, citable items.
func gather(args conversation.GatherArgs, deps Deps) ([]conversation.ContextItem, error) {
	out, err := run(args.ServiceArgs, deps, conversationnodes.EventUserMessage, cognition.Values{
		conversationnodes.PinText: args.Text, conversationnodes.PinWindow: args.Window, conversationnodes.PinSettings: args.Settings,
	})
	if err != nil {
		return nil, err
	}
	return cognition.Get[[]conversation.ContextItem](out, chroniclenodes.OutItems)
}

// followed is the data of the follower's "followed" event.
type followed struct {
	Updated  []entryRef `json:"updated,omitempty"`
	Proposed []entryRef `json:"proposed,omitempty"`
}

type entryRef struct {
	ID    uint64 `json:"id,string"`
	Title string `json:"title"`
}

// follow runs Personal's On reply event, the memory update with the
// entries the round injected as the ones it may update, and indexes what
// changed. Personal skips the update when the settings turn it off.
func follow(args conversation.FollowArgs, deps Deps) (*conversation.Followed, error) {
	outputs, err := run(args.ServiceArgs, deps, conversationnodes.EventReply, cognition.Values{
		conversationnodes.PinRound: args.Round, conversationnodes.PinSegments: args.Segments, conversationnodes.PinSettings: args.Settings,
	})
	if err != nil {
		return nil, err
	}
	changes, err := cognition.Get[chroniclesteps.Changes](outputs, chroniclenodes.OutChanges)
	if err != nil {
		return nil, err
	}
	if len(changes.Updated) == 0 && len(changes.Proposed) == 0 {
		return nil, nil
	}
	var out followed
	var parts []string
	var ids []uint64
	for _, e := range changes.Updated {
		out.Updated = append(out.Updated, entryRef{ID: e.ID, Title: e.Title})
		ids = append(ids, e.ID)
	}
	for _, e := range changes.Proposed {
		out.Proposed = append(out.Proposed, entryRef{ID: e.ID, Title: e.Title})
	}
	if len(out.Updated) > 0 {
		parts = append(parts, "updated "+titles(out.Updated))
	}
	if len(out.Proposed) > 0 {
		parts = append(parts, "proposed "+titles(out.Proposed))
	}
	if len(ids) > 0 && deps.Entries != nil {
		if resp := deps.Entries.Reindex(chronicleservice.ReindexArgs{ServiceArgs: args.ServiceArgs, EntryIDs: ids}); resp.Common.Code >= 300 {
			log.Warn(args.Context(), "cannot index the entries the memory update changed: %s", resp.Common.Message)
		}
	}
	data, _ := json.Marshal(out)
	return &conversation.Followed{Summary: "Memory: " + strings.Join(parts, "; "), Data: data}, nil
}

func titles(refs []entryRef) string {
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = "*" + strings.NewReplacer("*", "", "_", " ").Replace(r.Title) + "*"
	}
	return strings.Join(names, ", ")
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
