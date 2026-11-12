package conversation

import (
	"encoding/json"

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

// ContextProvider declares a context setting of a round, and may gather
// context for it. Its settings are a JSON object with at least "enabled";
// their defaults are the configuration section DefaultsSection(ID), which
// conversationsettings declares from Schema and Defaults. A mode graph's
// Gather context node runs the Gather of the providers it names (forge
// 06i); a provider without one, such as Chronicle's, only holds settings
// the mode graph's own nodes read.
type ContextProvider struct {
	extensions.Descriptor
	Title    string
	Schema   json.RawMessage // the settings, in the config schema language
	Defaults json.RawMessage
	// Gather returns the items to add to the prompt. It runs concurrently
	// with the other providers a Gather context node names; an error skips
	// this provider for the round.
	Gather func(args GatherArgs) ([]ContextItem, error)
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

// RoundFollowers is the chat.roundFollower point: what follows a round
// beyond its mode graph, such as Chronicle flagging what a round changed
// when it is 🚫 ignored. Work after the reply itself, such as the memory
// update, is the mode graph's (forge 06i), reported by Emit followed.
var RoundFollowers = extensions.NewPoint[RoundFollower](
	"chat.roundFollower",
	"What follows a chat round beyond its mode graph, such as Chronicle flagging what an ignored round changed",
)

// RoundFollower follows rounds; its ID names the follow-ups a mode graph
// reports for it, which its browser counterpart shows.
type RoundFollower struct {
	extensions.Descriptor
	Title string
	// Feedback, when set, hears of a round's changed verdict once it is
	// stored, e.g. to flag what the round changed when it is 🚫 ignored.
	Feedback func(args features.ServiceArgs, round Round, from, to retrieval.Feedback)
}

// Followed is what a follow-up did, shown under the reply.
type Followed struct {
	Summary string          // one line of markdown, e.g. "Updated *Ada Lovelace*"
	Data    json.RawMessage // details for the follower's browser counterpart
}
