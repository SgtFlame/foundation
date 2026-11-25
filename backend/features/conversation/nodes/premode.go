package conversationnodes

import (
	_ "embed"
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// KindPreMode is the graph kind of the chat's pre-mode (forge 15f): run
// from On call before each round, it says whether the round stays in the
// conversation's mode, switches to another, or asks the person.
const KindPreMode = "chat.premode"

// PreModeID is the built-in pre-mode graph, the chat section's default
// preMode.graph.
const PreModeID = "chatPreMode"

// The pre-mode's inputs (its On call outputs), and so the keys of its
// runs' inputs.
const (
	PreText        = "text"        // string: the message
	PreMode        = "mode"        // string: the conversation's mode
	PreDefaultMode = "defaultMode" // string: the default mode's id
	PreOffered     = "offered"     // json: [{id, icon, label, purpose}]
	PreWorkbench   = "workbench"   // json: the workbench snapshot
	PreTitles      = "titles"      // json: recent conversations' titles
	PreFirstSwitch = "firstSwitch" // bool: the conversation was never in another mode
	PrePrevious    = "previous"    // json: {mode, reply} of the previous round, or nil
)

//go:embed premode.json
var preModeJSON []byte

// contributePreMode registers the chat.premode kind and the editable
// built-in chatPreMode.
func contributePreMode(reg *extensions.Registry) {
	cognition.Kinds.Register(reg, cognition.Kind{
		Descriptor: extensions.Descriptor{ID: KindPreMode, Order: 15, Source: source},
		Title:      "Chat pre-mode",
		Events:     []string{cognition.EventCall},
		Description: "Runs before each chat round (forge 15f), from On call with the message, the conversation's mode, the modes offered and where the message was sent, " +
			"and decides whether the round stays in its mode, switches to another, or asks the person which. The chat section's preMode names the graph; keep it to one quick model call, since the round waits for it",
		Returns: "decision (stay, switch or ask), mode (the mode the message needs), confidence (0 to 1) and reason (one sentence for the person)",
	})
	var g cognition.Graph
	if err := json.Unmarshal(preModeJSON, &g); err != nil {
		panic("conversationnodes: premode.json: " + err.Error()) // embedded, so a test catches it
	}
	g.ID = PreModeID
	cognition.Graphs.Register(reg, cognition.BuiltIn{Descriptor: extensions.Descriptor{ID: PreModeID, Order: 90, Source: source}, Graph: g, Editable: true})
}
