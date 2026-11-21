package conversation

import (
	"encoding/json"
	"slices"

	"github.com/SgtFlame/foundation/backend/features/ai/services/chatservice"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Modes is the chat.mode point: the chat modes offered by default (forge
// 06i). Each runs a published mode graph for every round; the chat.modes
// settings list them, built from these, and may add others, such as an
// agent of the user's.
var Modes = extensions.NewPoint[ModeEntry](
	"chat.mode",
	"Chat modes offered in the composer by default, each running a mode graph for every round",
)

// ModeEntry is a mode as the chat.modes settings list it; its
// Descriptor's ID is the mode's, stored on conversations and rounds.
type ModeEntry struct {
	extensions.Descriptor
	Label    string
	Icon     string
	GraphRef string // the mode graph's id
	Default  bool   // the mode of a new conversation where no Focus matches
	// Focus, when set, offers the mode only while the editor the user was
	// in matches it, and picks it for a new conversation started there.
	Focus *Focus
}

// Focus is where a mode is offered: the editor the user was in has one of
// Kinds, or a subject of one of Subjects.
type Focus struct {
	Kinds    []string `json:"kinds,omitempty"`
	Subjects []string `json:"subjects,omitempty"`
}

// Matches reports whether an editor of kind, with a subject of type
// subject (maybe empty), is in the focus.
func (f *Focus) Matches(kind, subject string) bool {
	if f == nil {
		return true
	}
	return slices.Contains(f.Kinds, kind) || (subject != "" && slices.Contains(f.Subjects, subject))
}

// Host is what a mode graph's chat nodes act on: the round Send runs the
// graph for, given as the run's cognition.Exec.Host. Its methods are safe
// to call from the graph's nodes in order; the reply ends the round once.
type Host interface {
	// Turn is the round as Send resolved it.
	Turn() Turn
	// Settings are a context provider's settings for the conversation:
	// the conversation's, else its defaults section, else its declared.
	Settings(provider string) json.RawMessage
	// Gather runs the Gather of the named providers that are enabled for
	// the conversation (every provider with one when none are named),
	// concurrently; a provider that fails is skipped.
	Gather(providers []string) []ContextItem
	// AddContext is system followed by the context: pinned segments first,
	// then by score, numbered [n] when citable, under header (and cite when
	// any are citable). It stores the cited segments as the round's and
	// sends them as its sources.
	AddContext(system string, items []ContextItem, header, cite string) (string, []retrieval.RoundSegment)
	// ServerTools and ClientTools are the tools a tool set may name.
	ServerTools() []tools.Tool
	ClientTools() []tools.Tool
	// Reply streams the reply: the tool loop at the level with the tools,
	// asking the user for writes and running client tools in the browser.
	// Text said before it starts the reply. It completes the round and
	// sends done; a second reply fails.
	Reply(args ReplyArgs) (ReplyOutcome, error)
	// Say adds text to the reply, streamed, before the round completes.
	Say(text string) error
	// SetTitle names the conversation and sends the title.
	SetTitle(title string)
	// Followed reports what a follow-up did, under the reply.
	Followed(follower string, f Followed)
}

// Turn is a round as a mode graph sees it.
type Turn struct {
	Conversation  Conversation
	Round         Round
	Window        []Round // the rounds sent as history, oldest first
	Workbench     json.RawMessage
	Level         string // the round's ai.chat.levels id
	ContextWindow int    // the level's model's window, in tokens
}

// ReplyArgs are a streamed reply's.
type ReplyArgs struct {
	System   string
	Messages []chatservice.Message // empty sends the window and the message
	Tools    []tools.Tool          // empty makes one streamed call
	MaxSteps int                   // 0 keeps the tools section's
	Level    string                // empty keeps the round's
}

// ReplyOutcome is how a reply ended.
type ReplyOutcome struct {
	Round Round // completed
	OK    bool  // replied: neither stopped nor failed
	Calls []tools.ToolCall
	// Segments are what the round injected and its tools cited.
	Segments []retrieval.RoundSegment
}
