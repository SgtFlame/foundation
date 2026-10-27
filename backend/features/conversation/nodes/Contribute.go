// Package conversationnodes contributes Chat's Cognition events and data
// types (documentation/Cognition.md › How chat uses it): On user
// message, which starts a round's context gathering, and On reply, which
// follows a successful reply. Their outputs are the run's inputs, named by
// the pin IDs below.
package conversationnodes

import (
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// The events' node type IDs.
const (
	EventUserMessage = "event.userMessage"
	EventReply       = "event.reply"
)

// The events' output pins, and so the keys of their runs' inputs.
const (
	PinText     = "text"     // string: the user's message
	PinWindow   = "window"   // []conversation.Round: the rounds sent as history, oldest first
	PinSettings = "settings" // json.RawMessage: the graph's context settings for the conversation
	PinRound    = "round"    // conversation.Round: the round just replied to
	PinSegments = "segments" // []retrieval.RoundSegment: what the round injected
)

// The data types Chat contributes.
const (
	TypeRound        = "Round"
	TypeRounds       = "Round[]"
	TypeSegments     = "RoundSegment[]"
	TypeContextItems = "ContextItem[]"
)

const source = "conversation"

// Contribute registers the events and the data types.
func Contribute(reg *extensions.Registry) {
	for i, t := range []cognition.DataType{
		{Descriptor: extensions.Descriptor{ID: TypeRound}, Color: "#00acc1", Description: "A chat round: the message, the reply and their usage"},
		{Descriptor: extensions.Descriptor{ID: TypeRounds}, Color: "#00838f", Description: "Chat rounds, oldest first"},
		{Descriptor: extensions.Descriptor{ID: TypeSegments}, Color: "#7e57c2", Description: "The segments a round injected, numbered as cited"},
		{Descriptor: extensions.Descriptor{ID: TypeContextItems}, Color: "#d81b60", Description: "Context for a round's prompt; citable when they carry a segment"},
	} {
		t.Order, t.Source = 100+i, source
		cognition.Types.Register(reg, t)
	}
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: EventUserMessage, Order: 10, Source: source},
		Title:       "On user message",
		Category:    "Events",
		Description: "A chat message was sent: gather the context its reply needs",
		Event:       true,
		Pins: cognition.Pins{Out: []cognition.Pin{
			cognition.ExecPin(cognition.Then, ""),
			cognition.DataPin(PinText, "Text", "string", "The user's message"),
			cognition.DataPin(PinWindow, "Window", TypeRounds, "The rounds sent as history with the message, oldest first"),
			cognition.DataPin(PinSettings, "Settings", "json", "The graph's context settings for the conversation"),
		}},
	})
	cognition.Nodes.Register(reg, cognition.NodeType{
		Descriptor:  extensions.Descriptor{ID: EventReply, Order: 20, Source: source},
		Title:       "On reply",
		Category:    "Events",
		Description: "A chat round was replied to successfully: learn from it",
		Event:       true,
		Pins: cognition.Pins{Out: []cognition.Pin{
			cognition.ExecPin(cognition.Then, ""),
			cognition.DataPin(PinRound, "Round", TypeRound, "The round, with its reply"),
			cognition.DataPin(PinSegments, "Segments", TypeSegments, "What the round injected"),
			cognition.DataPin(PinSettings, "Settings", "json", "The graph's context settings for the conversation"),
		}},
	})
}
