package cognition

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Nodes is the cognition.node point: the node types graphs are built from.
var Nodes = extensions.NewPoint[NodeType](
	"cognition.node",
	"Node types of the Cognition Engine, with their pins and how they run",
)

// Graphs is the cognition.graph point: built-in graphs, read-only, such as
// Chronicle's Personal mode.
var Graphs = extensions.NewPoint[BuiltIn](
	"cognition.graph",
	"Built-in Cognition graphs, read-only, such as the Personal chat mode",
)

// Types is the cognition.type point: the data types pins carry, with the
// colour the editor draws them in.
var Types = extensions.NewPoint[DataType](
	"cognition.type",
	"Data types carried by Cognition pins, with their colours",
)

// BuiltIn is a built-in graph; its Descriptor's ID is the graph's.
type BuiltIn struct {
	extensions.Descriptor
	Graph Graph
}

// DataType is a type a data pin carries; its Descriptor's ID is the type's
// name, as pins name it ("string", "Entry[]").
type DataType struct {
	extensions.Descriptor
	Color       string // a CSS colour
	Description string
}

// Any is the type that accepts, and is accepted by, every other.
const Any = "any"

// NodeType is a kind of node; its Descriptor's ID is the type's ID, such as
// "chronicle.match".
type NodeType struct {
	extensions.Descriptor
	Title       string
	Category    string // the Palette's group: Events, Flow, Chronicle, …
	Description string
	Color       string          // the header's CSS colour; empty takes the category's
	Event       bool            // starts runs: its outputs are the run's inputs
	Config      json.RawMessage // the config's schema, in the config schema language; empty when it has none
	// Pins are the node's pins. PinsFor, when set, gives them from the
	// node's config instead, e.g. a Return node's outputs.
	Pins    Pins
	PinsFor func(config json.RawMessage) (Pins, error)
	// Run runs the node. An exec node returns the exec pin it leaves by
	// (none ends the run); a pure node returns "". Event nodes have none:
	// the engine gives them the run's inputs.
	Run func(x *Exec, in Values) (Values, string, error)
}

// PinsOf is the node type's pins for a node's config.
func (t NodeType) PinsOf(config json.RawMessage) (Pins, error) {
	if t.PinsFor != nil {
		return t.PinsFor(config)
	}
	return t.Pins, nil
}

// Pins are a node's inputs and outputs, in the order they are drawn.
type Pins struct {
	In  []Pin `json:"in"`
	Out []Pin `json:"out"`
}

// Pure reports whether pins have no exec pin: such a node runs when a
// downstream node pulls one of its outputs.
func (p Pins) Pure() bool {
	for _, list := range [][]Pin{p.In, p.Out} {
		for _, pin := range list {
			if pin.Exec {
				return false
			}
		}
	}
	return true
}

// Find is the pin with the given ID among the inputs (in) or the outputs.
func (p Pins) Find(id string, in bool) (Pin, bool) {
	list := p.Out
	if in {
		list = p.In
	}
	for _, pin := range list {
		if pin.ID == id {
			return pin, true
		}
	}
	return Pin{}, false
}

// Pin is one input or output.
type Pin struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Exec        bool   `json:"exec,omitempty"` // an exec pin orders side effects; otherwise it carries Type
	Type        string `json:"type,omitempty"`
	Optional    bool   `json:"optional,omitempty"` // an input that may be left unconnected
	Description string `json:"description,omitempty"`
}

// ExecPin is an exec pin with the given ID.
func ExecPin(id, name string) Pin {
	return Pin{ID: id, Name: name, Exec: true}
}

// DataPin is a data pin.
func DataPin(id, name, typ, description string) Pin {
	return Pin{ID: id, Name: name, Type: typ, Description: description}
}

// Then is the exec pin in and out of a node that runs in line, by
// convention "then" for both.
const Then = "then"

// Values are a node's inputs or outputs by pin ID, as Go values.
type Values map[string]any

// Get is the value of pin id as a T: as it is when it already is one, the
// zero value when it is absent, or else converted through JSON (an event's
// inputs, for example, may come decoded from JSON).
func Get[T any](v Values, id string) (T, error) {
	var out T
	raw, ok := v[id]
	if !ok || raw == nil {
		return out, nil
	}
	if t, ok := raw.(T); ok {
		return t, nil
	}
	b, err := json.Marshal(raw)
	if err == nil {
		err = json.Unmarshal(b, &out)
	}
	if err != nil {
		return out, fmt.Errorf("pin %q holds a %s, not a %s", id, reflect.TypeOf(raw), reflect.TypeOf(out))
	}
	return out, nil
}

// Exec is what a node sees of the run while it runs.
type Exec struct {
	// ServiceArgs are the run's, with Meta naming the run and the node, so
	// the usage a node causes is attributed to it.
	features.ServiceArgs
	RunID  uint64
	Node   Node
	result Values
}

// NewExec is the Exec of a node in run runID; the engine makes them.
func NewExec(args features.ServiceArgs, runID uint64, node Node, result Values) *Exec {
	args.Meta.RunID, args.Meta.NodeID = runID, node.ID
	return &Exec{ServiceArgs: args, RunID: runID, Node: node, result: result}
}

// Return sets the run's outputs, as a Return node does.
func (x *Exec) Return(out Values) {
	for k, v := range out {
		x.result[k] = v
	}
}
