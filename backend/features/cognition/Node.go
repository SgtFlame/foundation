package cognition

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/cognition/tmpl"
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
	// Editable built-ins are defaults: the first command on one in a
	// tenant starts the tenant's draft from it, and its published version
	// takes the built-in's place there. Others are read-only, and are
	// changed by duplicating them.
	Editable bool
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
	// Example is how the node is used, for a model and the Palette, e.g.
	// "Branch on Is admin? to show the admin tools only to admins".
	Example string
	// Examples are minimal correct uses as JSON, {type, config}, for a
	// model (forge 06d); empty derives one from the config schema's
	// defaults (ExamplesOf). A config with required fields needs one.
	Examples []json.RawMessage
	// Tags are added to those derived from its category, source and kinds
	// (TagsOf).
	Tags []string
	// Kinds are the graph kinds it may be used in; empty allows every kind
	// (cognition.Kind.Allows).
	Kinds []string
	// Effects is set on nodes with side effects (model calls, commands,
	// writes), which pure kinds such as data.query refuse.
	Effects bool
	// PureIn are kinds a node with Effects is still allowed in, because
	// there its effect is part of what the run decides: Enqueue job in a
	// command graph only adds to the commit's outbox (forge 07a).
	PureIn []string
	// External is set on nodes that reach outside the tenant's data: a
	// model, a tool, the object store, mail. A sandboxed run (forge.test,
	// forge 06f) does not run them: it gives their Sample and lists them.
	External bool
	// Sample is what a stubbed External node gives, by output pin; a data
	// output left out gives nothing.
	Sample Values
	// SampleFor, when set, gives the Sample from the node's config, for a
	// node whose outputs follow from it: Call connector's operation's
	// result example (forge 07b).
	SampleFor func(config json.RawMessage) Values
	// Volatile pure nodes are evaluated every time they are pulled, not
	// once per execution context, e.g. Get variable.
	Volatile bool
	// Normalize, when set, completes a node's config as it is added or
	// changed in a draft, e.g. Call graph copies the called graph's
	// signature into its config, so its pins follow from it.
	Normalize func(n Normalizing) (json.RawMessage, error)
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

// Normalizing is what Normalize gets: the node's config and a way to read
// the tenant's other graphs.
type Normalizing struct {
	features.ServiceArgs
	Config json.RawMessage
	Graph  func(id string) (Graph, bool)
	// Signature is a graph's inputs and outputs (SignatureOf).
	Signature func(g Graph) (in, out []Pin)
}

// Exec is what a node sees of the run while it runs.
type Exec struct {
	// ServiceArgs are the run's, with Meta naming the run and the node, so
	// the usage a node causes is attributed to it.
	features.ServiceArgs
	RunID uint64
	Node  Node
	// Host is what the run acts on, given by whoever started it: a UI
	// session's handle for a ui.handler, a command's decision for a
	// command graph; nil for a chat mode.
	Host any
	// GraphID and Owner are the running graph's id and owner (app:<appId>
	// for an app's graph), for nodes that name the app's other graphs.
	GraphID, Owner string
	Vars           Values // the run's variables, by ID
	// MaxIterations is how many times a loop may run its body (the
	// cognition section's maxLoopIterations).
	MaxIterations int
	result        Values
	Hooks         ExecHooks
}

// ExecHooks are the engine's, for flow nodes.
type ExecHooks struct {
	// Flow runs the exec chain leaving the node by pin to its end, in a new
	// execution context named frame when it is not empty (a loop's
	// iteration), and returns when the chain ends.
	Flow func(pin, frame string) error
	// Set publishes the node's outputs before it returns, so a loop's body
	// reads the current item.
	Set func(out Values)
	// Pull evaluates a data input again, in the execution context named
	// frame when it is not empty, so a loop's condition sees the iteration.
	Pull func(pin, frame string) (any, error)
	// Call runs another graph from its On call event and returns its
	// outputs.
	Call func(graphID string, inputs Values) (Values, error)
	// Setting reads a path of a configuration section as the run's
	// principal sees it ("builder", "maxSteps"); an empty path is the
	// whole section (forge 06i).
	Setting func(section, path string) (any, error)
}

// NewExec is the Exec of a node in run runID; the engine makes them.
func NewExec(args features.ServiceArgs, runID uint64, node Node, result Values) *Exec {
	args.Meta.RunID, args.Meta.NodeID = runID, node.ID
	return &Exec{ServiceArgs: args, RunID: runID, Node: node, result: result, Vars: Values{}}
}

// Flow runs the chain leaving by pin (see ExecHooks.Flow).
func (x *Exec) Flow(pin, frame string) error {
	if x.Hooks.Flow == nil {
		return fmt.Errorf("%s needs the engine to run its %s chain", x.Node.ID, pin)
	}
	return x.Hooks.Flow(pin, frame)
}

// Set publishes outputs before the node returns (see ExecHooks.Set).
func (x *Exec) Set(out Values) {
	if x.Hooks.Set != nil {
		x.Hooks.Set(out)
	}
}

// Pull evaluates an input again (see ExecHooks.Pull).
func (x *Exec) Pull(pin, frame string) (any, error) {
	if x.Hooks.Pull == nil {
		return nil, fmt.Errorf("%s needs the engine to pull %s", x.Node.ID, pin)
	}
	return x.Hooks.Pull(pin, frame)
}

// Call runs another graph (see ExecHooks.Call).
func (x *Exec) Call(graphID string, inputs Values) (Values, error) {
	if x.Hooks.Call == nil {
		return nil, fmt.Errorf("%s needs the engine to call graph %s", x.Node.ID, graphID)
	}
	return x.Hooks.Call(graphID, inputs)
}

// Setting reads a configuration setting (see ExecHooks.Setting).
func (x *Exec) Setting(section, path string) (any, error) {
	if x.Hooks.Setting == nil {
		return nil, fmt.Errorf("%s cannot read settings here", x.Node.ID)
	}
	return x.Hooks.Setting(section, path)
}

// Env is what the templates a node renders reach: the run's settings.
func (x *Exec) Env() tmpl.Env {
	return tmpl.Env{Setting: x.Setting}
}

// Render renders a Go template with data (package tmpl).
func (x *Exec) Render(text string, data any) (string, error) {
	return tmpl.Render(text, data, x.Env())
}

// Config decodes the node's config into v; an empty config leaves v as it is.
func (x *Exec) Config(v any) error {
	if len(x.Node.Config) == 0 {
		return nil
	}
	if err := json.Unmarshal(x.Node.Config, v); err != nil {
		return fmt.Errorf("the node's config is not valid: %w", err)
	}
	return nil
}

// Return sets the run's outputs, as a Return node does.
func (x *Exec) Return(out Values) {
	for k, v := range out {
		x.result[k] = v
	}
}
