package conversation

import (
	"encoding/json"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
)

// ContextProviders is the chat.contextProvider point. A provider pairs with
// the browser's chat.contextSettings contribution of the same ID, whose
// control edits the provider's settings; the browser hides a control with
// no provider.
var ContextProviders = extensions.NewPoint[ContextProvider](
	"chat.contextProvider",
	"Context gathered for a chat round's prompt, each paired with a chat.contextSettings control in the browser",
)

// ContextProvider gathers context for a round. Its settings are a JSON
// object with at least "enabled"; their defaults are the configuration
// section DefaultsSection(ID), which conversationsettings declares from
// Schema and Defaults.
type ContextProvider struct {
	extensions.Descriptor
	Title    string
	Schema   json.RawMessage // the settings, in the config schema language
	Defaults json.RawMessage
	// Gather returns the items to add to the prompt. It runs concurrently
	// with the other providers; an error skips this provider for the round.
	Gather func(args GatherArgs) ([]ContextItem, error)
	// Mode, when set, may make the round run in a mode: instructions for
	// the model, its own steps limit and tools (forge 06g, until chat modes
	// exist: forge 06i). It runs beside Gather; nil keeps the plain chat.
	// When several providers give one, the first in Order wins.
	Mode func(args GatherArgs) (*Mode, error)
}

// Mode is how a round runs when a provider puts it in one, such as
// Forge's Builder while a Forge editor is focused.
type Mode struct {
	ID    string // stored on the round, e.g. "builder"
	Title string // shown on the round, e.g. "Builder"
	// Instructions go in the system prompt, after the chat's own and before
	// the context. Unlike context items, they are instructions.
	Instructions string
	MaxSteps     int      // steps per round in place of the tools section's; 0 keeps it
	Tools        []string // the tools offered: names, or prefixes ending in "." (forge.); empty offers all
}

// Offers reports whether the mode offers a tool.
func (m *Mode) Offers(tool string) bool {
	if m == nil || len(m.Tools) == 0 {
		return true
	}
	for _, t := range m.Tools {
		if t == tool || (strings.HasSuffix(t, ".") && strings.HasPrefix(tool, t)) {
			return true
		}
	}
	return false
}

// GatherArgs is what a provider sees of the round.
type GatherArgs struct {
	features.ServiceArgs
	ConversationID uint64
	Text           string          // the user's message
	Window         []Round         // the rounds sent as history with the message, oldest first
	Workbench      json.RawMessage // the browser's workbench snapshot; untrusted, may be empty
	Settings       json.RawMessage // this provider's settings for the conversation
	ContextWindow  int             // the round's model's context window, in tokens; 0 when unknown
}

// ContextItem is one piece of context. Items are added to the prompt by
// Score, highest first, until the context budget is spent.
type ContextItem struct {
	Kind   string // e.g. "workbench", "chunk", "chronicle"
	Ref    string // what a citation names
	Title  string
	Text   string
	Tokens int // estimated; 0 lets Send estimate
	Score  float64
	// Segment makes the item citable: Send numbers it [n] in the prompt,
	// records it as the round's retrieval.RoundSegment and sends it as the
	// round's sources.
	Segment *retrieval.Scored
}

// DebugID is the chat.contextProvider (and chat.contextSettings) whose
// "enabled" setting is the conversation's Debug checkbox: it adds no
// context, and turns on features.Meta.Debug for the round's AI calls.
const DebugID = "debug"

// DefaultsSection is the configuration section of a provider's defaults.
func DefaultsSection(id string) string {
	return "chat.context.defaults." + id
}

// RoundEnds is the chat.roundEnd point: work done when a round ends,
// however it ends (done, failed or stopped), such as clearing the
// presence the round's tools set (forge 06g).
var RoundEnds = extensions.NewPoint[RoundEnd](
	"chat.roundEnd",
	"Work done when a chat round ends, however it ends, such as clearing the presence its tools set",
)

// RoundEnd runs once per round, after its done event. Its context
// outlives the browser.
type RoundEnd struct {
	extensions.Descriptor
	End func(args features.ServiceArgs, round Round)
}

// RoundFollowers is the chat.roundFollower point: work that follows a
// successful reply in the same stream, such as Chronicle's memory update.
// What a follower did reaches the browser as a "followed" event.
var RoundFollowers = extensions.NewPoint[RoundFollower](
	"chat.roundFollower",
	"Work that follows a chat round's reply, such as Chronicle's memory update",
)

// RoundFollower runs after each successful reply, one after another in
// Order, once the title is written.
type RoundFollower struct {
	extensions.Descriptor
	Title string
	// Follow does the follower's work. Its context outlives the browser, so
	// the work is done even when nobody is left to tell. A nil Followed
	// reports nothing; an error is logged.
	Follow func(args FollowArgs) (*Followed, error)
	// Feedback, when set, hears of a round's changed verdict once it is
	// stored, e.g. to flag what the round changed when it is 🚫 ignored.
	Feedback func(args features.ServiceArgs, round Round, from, to retrieval.Feedback)
}

// FollowArgs is the round a follower follows.
type FollowArgs struct {
	features.ServiceArgs
	Conversation Conversation
	Round        Round
	// Settings are the conversation's settings of the context provider
	// with the follower's ID, resolved as Send resolves them; empty when
	// there is no such provider.
	Settings json.RawMessage
	Segments []retrieval.RoundSegment // what the round injected
}

// Followed is what a follower did, shown under the reply.
type Followed struct {
	Summary string          // one line of markdown, e.g. "Updated *Ada Lovelace*"
	Data    json.RawMessage // details for the follower's browser counterpart
}
