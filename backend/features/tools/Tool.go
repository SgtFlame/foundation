// Package tools is function calling for chat and agents
// (documentation/plans/Tools.md): the tools.tool point, the calls the model
// makes, and Loop, the one tool loop, which chat's Send and Cognition's
// Agent node both run.
package tools

import (
	"encoding/json"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/retrieval"
)

// Tools is the tools.tool point: the server's tools. Client tools are the
// browser's chat.clientTool contributions, declared with each message.
var Tools = extensions.NewPoint[Tool](
	"tools.tool",
	"Functions the model may call, such as kb.search, with their effect and input schema",
)

// ContextID is the chat.contextProvider (and chat.contextSettings) whose
// "enabled" setting turns a conversation's tools on and off.
const ContextID = "tools"

// Effect is what a tool does, which decides whether it asks first.
type Effect string

const (
	EffectNone     Effect = "none"     // computes only, e.g. time.now
	EffectRead     Effect = "read"     // reads the user's data
	EffectWrite    Effect = "write"    // changes the user's data: asks unless allowed for the conversation
	EffectExternal Effect = "external" // reaches outside Foundation: asks like write
	EffectUI       Effect = "ui"       // client tools only: changes only what the user sees
)

// Asks reports whether a call with this effect needs the user's approval.
func (e Effect) Asks() bool {
	return e == EffectWrite || e == EffectExternal
}

// Location is where a tool runs.
type Location string

const (
	LocationServer Location = "server"
	LocationClient Location = "client" // in the browser of the chat that sent the message
)

// WaitForUser is a client tool's Wait when it waits for the user (such as
// chat.askChoice) rather than for the browser: until the round is stopped.
const WaitForUser time.Duration = -1

// Tool is one function the model may call. Its Descriptor's ID is its
// dotted, stable name ("kb.search"), sent to the model with '.' as '_'.
type Tool struct {
	extensions.Descriptor
	Title       string          // a short name for the call's card, e.g. "Search Volumes"
	Description string          // for the model: what it does and when to use it
	Input       json.RawMessage // the arguments' JSON Schema, an object; empty takes none
	Effect      Effect
	Location    Location // server when empty
	// Wait is how long a client tool's result is waited for: 0 is the tools
	// section's clientTimeoutSeconds, WaitForUser until the round is stopped.
	Wait time.Duration
	// Run runs a server tool. An error's text goes to the model and the
	// user, so it names nothing private (a service's Common.Message is safe).
	Run func(call Call) (Result, error)
}

// Client reports whether the tool runs in the browser.
func (t Tool) Client() bool {
	return t.Location == LocationClient
}

// Call is one call of a server tool, with the round's (or run's) principal
// and Meta, whose Feature names the tool.
type Call struct {
	features.ServiceArgs
	ID   string // the call's ID in its round
	Name string
	Args json.RawMessage // checked against the tool's Input
}

// Result is what a call returns.
type Result struct {
	// Content is the result for the model, as a JSON object.
	Content map[string]any
	// Summary is one line of markdown for the call's card, e.g. "Found 5
	// passages"; empty shows none.
	Summary string
	// Segments make the result citable: in a chat round they are numbered
	// after the round's context and listed in its sources, and the model
	// sees them as Content's "passages", each with its [n].
	Segments []retrieval.Scored
}

// Log is the tools feature's logging and error builder.
var Log = logging.In("tools")
