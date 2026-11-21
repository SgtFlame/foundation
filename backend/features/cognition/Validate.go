package cognition

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Problem severities: an error stops the graph from running; a warning does not.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Problem is something wrong with a graph, on a node and pin when it is
// about one.
type Problem struct {
	Severity string `json:"severity"`
	NodeID   string `json:"nodeId,omitempty"`
	Pin      string `json:"pin,omitempty"`
	Message  string `json:"message"`
}

// Resolved is a graph with each node's type and pins, as validation found
// them: what the engine runs and the editor draws.
type Resolved struct {
	Graph    Graph
	Types    map[string]NodeType // by node ID
	Pins     map[string]Pins     // by node ID
	Problems []Problem
	// Conversions are the implicit conversions made on edges, by the input
	// they feed.
	Conversions map[PinRef]Conversion
}

// Errors reports whether any problem is an error.
func (r Resolved) Errors() bool {
	return slices.ContainsFunc(r.Problems, func(p Problem) bool { return p.Severity == SeverityError })
}

// Resolve resolves a graph against node types alone, with no kinds and no
// conversions (Catalog.Resolve).
func Resolve(g Graph, types []NodeType) Resolved {
	return Catalog{Types: types}.Resolve(g)
}

// MetaProblems is what a node type lacks of the metadata a model needs
// (forge Architecture › self-describing metadata): a description, an
// example and a description of each fixed data pin.
func MetaProblems(t NodeType) []string {
	var out []string
	if t.Description == "" {
		out = append(out, "a description")
	}
	if t.Example == "" {
		out = append(out, "an example")
	}
	for _, list := range [][]Pin{t.Pins.In, t.Pins.Out} {
		for _, p := range list {
			if !p.Exec && p.Description == "" {
				out = append(out, "a description of pin "+p.ID)
			}
		}
	}
	return out
}

// Resolve finds each node's type and pins and validates the graph: unknown
// types, types without metadata, nodes the graph's kind does not allow,
// edges between pins that do not exist, run the wrong way, mix exec and
// data or carry types that neither match nor convert, inputs joined twice,
// required inputs left unconnected, cycles among pure nodes, no event, and
// exec nodes no event reaches (a warning).
func (c Catalog) Resolve(g Graph) Resolved {
	byID := map[string]NodeType{}
	for _, t := range c.Types {
		byID[t.ID] = t
	}
	r := Resolved{Graph: g, Types: map[string]NodeType{}, Pins: map[string]Pins{}, Conversions: map[PinRef]Conversion{}}
	fail := func(node, pin, format string, a ...any) {
		r.Problems = append(r.Problems, Problem{Severity: SeverityError, NodeID: node, Pin: pin, Message: fmt.Sprintf(format, a...)})
	}
	kind, hasKind := c.Kind(g.Kind)

	events := map[string]string{}
	for _, n := range g.Nodes {
		if _, dup := r.Types[n.ID]; dup {
			fail(n.ID, "", "two nodes have the ID %q", n.ID)
			continue
		}
		t, ok := byID[n.Type]
		if !ok {
			fail(n.ID, "", "there is no node type %q", n.Type)
			continue
		}
		if missing := MetaProblems(t); len(missing) > 0 {
			fail(n.ID, "", "node type %s lacks %s, so it cannot be used", t.ID, strings.Join(missing, ", "))
		}
		if hasKind && !kind.Allows(t) {
			fail(n.ID, "", "a %s graph cannot hold %s", kind.Title, t.Title)
		}
		pins, err := t.PinsOf(n.Config)
		if err != nil {
			fail(n.ID, "", "the node's config is not valid: %v", err)
			continue
		}
		r.Types[n.ID], r.Pins[n.ID] = t, pins
		if t.Event {
			if other, dup := events[t.ID]; dup {
				fail(n.ID, "", "%s is already the graph's %q event", other, t.Title)
			}
			events[t.ID] = n.ID
		}
	}
	if len(events) == 0 {
		fail("", "", "the graph has no event node, so nothing starts it")
	}

	into := map[PinRef]int{}
	execOut := map[PinRef]int{}
	next := map[string][]string{} // exec successors
	pull := map[string][]string{} // pure node → the pure nodes it pulls from
	for _, e := range g.Edges {
		from, okFrom := r.Pins[e.From.Node]
		to, okTo := r.Pins[e.To.Node]
		if !okFrom || !okTo {
			if _, ok := g.NodeByID(e.From.Node); !ok {
				fail(e.From.Node, e.From.Pin, "an edge starts at node %q, which does not exist", e.From.Node)
			}
			if _, ok := g.NodeByID(e.To.Node); !ok {
				fail(e.To.Node, e.To.Pin, "an edge ends at node %q, which does not exist", e.To.Node)
			}
			continue
		}
		out, ok := from.Find(e.From.Pin, false)
		if !ok {
			fail(e.From.Node, e.From.Pin, "the node has no output %q", e.From.Pin)
			continue
		}
		in, ok := to.Find(e.To.Pin, true)
		if !ok {
			fail(e.To.Node, e.To.Pin, "the node has no input %q", e.To.Pin)
			continue
		}
		switch {
		case out.Exec != in.Exec:
			fail(e.To.Node, e.To.Pin, "an exec pin is joined to a data pin")
		case out.Exec:
			if execOut[e.From]++; execOut[e.From] > 1 {
				fail(e.From.Node, e.From.Pin, "an exec output can lead to only one node")
			}
			next[e.From.Node] = append(next[e.From.Node], e.To.Node)
		default:
			if conv, ok := c.Conversion(out.Type, in.Type); ok {
				r.Conversions[e.To] = conv
			} else if !Assignable(out.Type, in.Type) {
				fail(e.To.Node, e.To.Pin, "%s carries a %s, but %s takes a %s", out.Name, out.Type, in.Name, in.Type)
			}
			if into[e.To]++; into[e.To] > 1 {
				fail(e.To.Node, e.To.Pin, "the input %s is joined to more than one output", in.Name)
			}
			if from.Pure() && to.Pure() {
				pull[e.To.Node] = append(pull[e.To.Node], e.From.Node)
			}
		}
	}

	for _, n := range g.Nodes {
		pins, ok := r.Pins[n.ID]
		if !ok {
			continue
		}
		for _, in := range pins.In {
			if !in.Exec && !in.Optional && into[PinRef{n.ID, in.ID}] == 0 {
				fail(n.ID, in.ID, "the input %s is not connected", in.Name)
			}
		}
	}

	// Cycles among pure nodes would pull forever
	state := map[string]int{} // 1 visiting, 2 done
	var visit func(id string) bool
	visit = func(id string) bool {
		switch state[id] {
		case 1:
			return true
		case 2:
			return false
		}
		state[id] = 1
		for _, src := range pull[id] {
			if visit(src) {
				return true
			}
		}
		state[id] = 2
		return false
	}
	for _, n := range g.Nodes {
		if state[n.ID] == 0 && visit(n.ID) {
			fail(n.ID, "", "the node is in a cycle of pure nodes")
		}
	}

	// Exec nodes no event reaches never run
	reached := map[string]bool{}
	var walk func(id string)
	walk = func(id string) {
		if reached[id] {
			return
		}
		reached[id] = true
		for _, n := range next[id] {
			walk(n)
		}
	}
	for _, id := range events {
		walk(id)
	}
	for _, n := range g.Nodes {
		if pins, ok := r.Pins[n.ID]; ok && !pins.Pure() && !reached[n.ID] {
			r.Problems = append(r.Problems, Problem{Severity: SeverityWarning, NodeID: n.ID, Message: "no event reaches this node, so it never runs"})
		}
	}
	return r
}

// Assignable reports whether an output of type from may feed an input of
// type to as it is; Catalog.Connects also allows implicit conversions.
func Assignable(from, to string) bool {
	return from == to || from == Any || to == Any
}

// EventCall is the On call event's node type, which starts function graphs
// and actions; its declared outputs are the graph's inputs.
const EventCall = "event.call"

// SignatureOf is a graph's inputs (its On call event's outputs) and outputs
// (its Return nodes' inputs), for Call graph.
func SignatureOf(g Graph, types []NodeType) (in, out []Pin) {
	byID := map[string]NodeType{}
	for _, t := range types {
		byID[t.ID] = t
	}
	seen := map[string]bool{}
	for _, n := range g.Nodes {
		t, ok := byID[n.Type]
		if !ok {
			continue
		}
		pins, err := t.PinsOf(n.Config)
		if err != nil {
			continue
		}
		switch n.Type {
		case EventCall:
			for _, p := range pins.Out {
				if !p.Exec {
					in = append(in, p)
				}
			}
		case "flow.return":
			for _, p := range pins.In {
				if !p.Exec && !seen[p.ID] {
					seen[p.ID] = true
					out = append(out, p)
				}
			}
		}
	}
	return in, out
}

// OutputsConfig is the config of a node whose data pins the user declares,
// such as Return's outputs.
type OutputsConfig struct {
	Outputs []Pin `json:"outputs"`
}

// DeclaredPins decodes an OutputsConfig into data pins.
func DeclaredPins(config json.RawMessage) ([]Pin, error) {
	var c OutputsConfig
	if len(config) > 0 {
		if err := json.Unmarshal(config, &c); err != nil {
			return nil, err
		}
	}
	for i, p := range c.Outputs {
		if p.ID == "" || p.Type == "" {
			return nil, fmt.Errorf("output %d needs an id and a type", i+1)
		}
		if c.Outputs[i].Name == "" {
			c.Outputs[i].Name = p.ID
		}
		c.Outputs[i].Exec = false
	}
	return c.Outputs, nil
}
