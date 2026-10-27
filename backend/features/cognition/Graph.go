// Package cognition is the Cognition Engine's model
// (documentation/plans/Cognition.md): graphs of typed nodes and edges, the
// node types contributed on the cognition.node point, the built-in graphs on
// cognition.graph, the data types on cognition.type, validation, and the
// tables a run's trace is kept in. The engine that runs graphs is
// cognition/engine; cognitionservice serves them to the Cognition Editor.
package cognition

import "encoding/json"

// Graph kinds.
const (
	KindMode       = "mode"       // a chat mode, such as Personal
	KindAgent      = "agent"      // a persona callable from other graphs
	KindFunction   = "function"   // a sub-graph
	KindSummarizer = "summarizer" // a Chronicle custom type's summarizing mode
)

// Graph is a graph as the engine runs it and the editor draws it.
type Graph struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Version     int    `json:"version"` // the published version; a run records it
	Nodes       []Node `json:"nodes"`
	Edges       []Edge `json:"edges"`
}

// Node is one node of a graph.
type Node struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`            // a cognition.node contribution's ID
	Title    string          `json:"title,omitempty"` // shown in place of the type's title
	Position Position        `json:"position"`
	Config   json.RawMessage `json:"config,omitempty"`
}

// Position is a node's place on the canvas.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Edge joins an output pin to an input pin.
type Edge struct {
	From PinRef `json:"from"`
	To   PinRef `json:"to"`
}

// PinRef names a pin of a node.
type PinRef struct {
	Node string `json:"node"`
	Pin  string `json:"pin"`
}

// NodeByID is the graph's node with the given ID.
func (g Graph) NodeByID(id string) (Node, bool) {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}
