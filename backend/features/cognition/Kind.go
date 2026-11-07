package cognition

import (
	"encoding/json"
	"slices"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Kinds is the cognition.kind point: what a graph is for (Blueprints
// question 1). A kind sets which event nodes start its graphs, which nodes
// they may hold and what they return.
var Kinds = extensions.NewPoint[Kind](
	"cognition.kind",
	"Graph kinds: which events start a graph, which nodes it may hold and what it returns",
)

// Kind is one kind of graph; its Descriptor's ID is the kind, such as
// "mode" or "ui.handler".
type Kind struct {
	extensions.Descriptor
	Title       string
	Description string // for a model: what graphs of this kind do and when they run
	Returns     string // what a run gives back, in words
	// Events are the event node types that may start its graphs; empty
	// allows every event.
	Events []string
	// Pure kinds allow no node with Effects: a data.query reads, a command
	// decides.
	Pure bool
	// Spec is the JSON Schema of the graph's Spec, such as a command
	// graph's command type and args; empty when the kind has none.
	Spec json.RawMessage
	// IDFor, when set, is the graph ids the spec allows, the first the
	// one to use: a command graph's id follows from its command type, so
	// the bus finds it. A graph owned by an app (app:<appId>) may also
	// have the id of <appId>.<type>, so two apps can define one command
	// type (forge 05b question 3).
	IDFor func(spec json.RawMessage, owner string) ([]string, error)
	// Host makes what a test run's nodes act on (a UI session, a command's
	// decision) and reports what they did; nil runs tests with none.
	Host func(args features.ServiceArgs, g Graph, inputs Values) (host any, report func() any)
}

// Allows reports whether a graph of the kind may hold a node of type t.
func (k Kind) Allows(t NodeType) bool {
	if len(t.Kinds) > 0 && !slices.Contains(t.Kinds, k.ID) {
		return false
	}
	if t.Event && len(k.Events) > 0 && !slices.Contains(k.Events, t.ID) {
		return false
	}
	return !(k.Pure && t.Effects)
}

// Conversions is the cognition.conversion point: the safe implicit
// conversions an edge may make between two data types.
var Conversions = extensions.NewPoint[Conversion](
	"cognition.conversion",
	"Implicit conversions between Cognition data types, made on an edge",
)

// Conversion turns a value of type From into one of type To; its
// Descriptor's ID is "<from>→<to>".
type Conversion struct {
	extensions.Descriptor
	From, To string
	Convert  func(v any) (any, error)
}

// Catalog is what a graph is resolved against: the node types, kinds and
// conversions of a registry.
type Catalog struct {
	Types       []NodeType
	Kinds       []Kind
	Conversions []Conversion
}

// CatalogOf is the registry's catalog.
func CatalogOf(reg *extensions.Registry) Catalog {
	return Catalog{Types: Nodes.All(reg), Kinds: Kinds.All(reg), Conversions: Conversions.All(reg)}
}

// Kind is the catalog's kind with the given ID.
func (c Catalog) Kind(id string) (Kind, bool) {
	for _, k := range c.Kinds {
		if k.ID == id {
			return k, true
		}
	}
	return Kind{}, false
}

// Type is the catalog's node type with the given ID.
func (c Catalog) Type(id string) (NodeType, bool) {
	for _, t := range c.Types {
		if t.ID == id {
			return t, true
		}
	}
	return NodeType{}, false
}

// Conversion is the conversion from one type to another; ok is false when
// none is needed (the types are assignable) or none exists.
func (c Catalog) Conversion(from, to string) (Conversion, bool) {
	if Assignable(from, to) {
		return Conversion{}, false
	}
	for _, conv := range c.Conversions {
		if conv.From == from && conv.To == to {
			return conv, true
		}
	}
	return Conversion{}, false
}

// Connects reports whether an output of type from may feed an input of
// type to, as it is or through a conversion.
func (c Catalog) Connects(from, to string) bool {
	if Assignable(from, to) {
		return true
	}
	_, ok := c.Conversion(from, to)
	return ok
}
