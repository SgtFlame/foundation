package drafts

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/cognition/engine"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

var log = logging.In("cognition").Tags("drafts")

// Projections are what the drafts read: the projections, and waiting for
// a commit to be projected (live/project.Projections).
type Projections interface {
	live.Store
	WaitFor(ctx context.Context, tenant uint64, projector, key string, seq uint64, timeout time.Duration) (live.Projected, error)
}

// Deps are what the drafts' commands, queries and tools need, made after
// the registry.
type Deps struct {
	Engine      *engine.Engine
	Projections Projections
}

// Resource is a graph's authz resource.
func Resource(p features.Principal, id string) string {
	return authz.Tenant(p) + "/cognition/graph:" + id
}

// decision is one command's view of its draft.
type decision struct {
	live.Decision
	deps    Deps
	draft   Draft
	exists  bool
	prefix  []live.NewEvent // the created event of a draft started from an editable built-in
	catalog cognition.Catalog
}

func (c *decision) graph() *cognition.Graph { return &c.draft.Graph }

// problem is a rejection.
func problem(path, message, fix string) []live.Problem {
	return []live.Problem{{Path: path, Message: message, Fix: fix}}
}

// load reads the target's draft, or starts it from an editable built-in.
func load(d live.Decision, deps Deps, mayCreate bool) (*decision, []live.Problem) {
	c := &decision{Decision: d, deps: deps, catalog: deps.Engine.Catalog()}
	if d.State != nil {
		if err := json.Unmarshal(d.State, &c.draft); err != nil {
			return nil, problem("", "the draft cannot be read: "+err.Error(), "")
		}
		c.exists = true
		return c, nil
	}
	if mayCreate {
		return c, nil
	}
	b, ok := deps.Engine.BuiltIn(d.Target.ID)
	switch {
	case ok && b.Editable:
		// The built-in, or the version the tenant last published of it
		g, _ := deps.Engine.Graph(d.ServiceArgs, d.Target.ID)
		c.draft = Draft{Graph: g, Published: g.Version}
		c.exists = true
		c.prefix = []live.NewEvent{{Type: EventCreated, Data: map[string]any{"graph": g}}}
		return c, nil
	case ok:
		return nil, problem("target", fmt.Sprintf("%s is built in and read-only", b.Graph.Name), "duplicate it with bp.create {kind, name, from: \""+d.Target.ID+"\"} and edit the copy")
	}
	return nil, problem("target", fmt.Sprintf("there is no graph %q", d.Target.ID), "make it first with bp.create {kind, name}")
}

// lookup is a graph by ID as the tenant has it: its published version or
// built-in, else its draft.
func lookup(args features.ServiceArgs, deps Deps, id string) (cognition.Graph, bool) {
	if g, ok := deps.Engine.Graph(args, id); ok {
		return g, true
	}
	if deps.Projections == nil {
		return cognition.Graph{}, false
	}
	v, ok, err := deps.Projections.Get(args.Context(), args.Principal.TenantID, DraftProjector, TargetType+"."+id)
	if err != nil || !ok {
		return cognition.Graph{}, false
	}
	var d Draft
	if json.Unmarshal(v.Value, &d) != nil {
		return cognition.Graph{}, false
	}
	return d.Graph, true
}

// prepare checks a node against the draft's kind and completes its config.
func (c *decision) prepare(n *cognition.Node, path string) []live.Problem {
	t, ok := c.catalog.Type(n.Type)
	if !ok {
		return problem(path+".type", fmt.Sprintf("there is no node type %q", n.Type), "list the node types with bp.listNodeTypes {kind: \""+c.graph().Kind+"\"}")
	}
	if k, ok := c.catalog.Kind(c.graph().Kind); ok && !k.Allows(t) {
		return problem(path+".type", fmt.Sprintf("a %s graph cannot hold %s", k.Title, t.Title), "list the nodes it allows with bp.listNodeTypes {kind: \""+k.ID+"\"}")
	}
	if t.Normalize != nil {
		config, err := t.Normalize(cognition.Normalizing{
			ServiceArgs: c.ServiceArgs, Config: n.Config,
			Graph:     func(id string) (cognition.Graph, bool) { return lookup(c.ServiceArgs, c.deps, id) },
			Signature: func(g cognition.Graph) (in, out []cognition.Pin) { return cognition.SignatureOf(g, c.catalog.Types) },
		})
		if err != nil {
			return problem(path+".config", err.Error(), "")
		}
		n.Config = config
	}
	if _, err := t.PinsOf(n.Config); err != nil {
		return problem(path+".config", "the config is not valid: "+err.Error(), "describe the node type with bp.describeNodeType {type: \""+t.ID+"\"}")
	}
	return nil
}

// pins are a node's resolved pins.
func (c *decision) pins(nodeID string) (cognition.Pins, bool) {
	n, ok := c.graph().NodeByID(nodeID)
	if !ok {
		return cognition.Pins{}, false
	}
	t, ok := c.catalog.Type(n.Type)
	if !ok {
		return cognition.Pins{}, false
	}
	p, err := t.PinsOf(n.Config)
	return p, err == nil
}

// nextID is a node ID the draft does not use yet.
func nextID(g cognition.Graph) string {
	most := 0
	for _, n := range g.Nodes {
		if rest, ok := strings.CutPrefix(n.ID, "n"); ok {
			if k, err := strconv.Atoi(rest); err == nil && k > most {
				most = k
			}
		}
	}
	return "n" + strconv.Itoa(most+1)
}

// place is where a node added without a position goes: right of the
// rightmost node, so nodes the chat adds line up.
func place(g cognition.Graph) cognition.Position {
	if len(g.Nodes) == 0 {
		return cognition.Position{}
	}
	right := g.Nodes[0]
	for _, n := range g.Nodes[1:] {
		if n.Type != "note.comment" && n.Position.X > right.Position.X {
			right = n
		}
	}
	return cognition.Position{X: right.Position.X + 320, Y: right.Position.Y}
}

const pinRef = `{"type":"object","properties":{"node":{"type":"string","description":"The node's id"},"pin":{"type":"string","description":"The pin's id"}},"required":["node","pin"]}`
const position = `{"type":"object","properties":{"x":{"type":"number"},"y":{"type":"number"}},"required":["x","y"]}`
const nodeSchema = `{"type":"object","properties":{"id":{"type":"string"},"type":{"type":"string"},"title":{"type":"string"},"position":` + position + `,"config":{"type":"object"}},"required":["id","type"]}`
const edgeSchema = `{"type":"object","properties":{"from":` + pinRef + `,"to":` + pinRef + `},"required":["from","to"]}`

type spec struct {
	id, title, description, args, example string
	events                                []string
	action                                string
	create                                bool // may target a graph that does not exist yet
	decide                                func(c *decision) ([]live.NewEvent, []live.Problem)
}

// Contribute registers the drafts' projectors, commands, queries, call and
// tools; deps returns their dependencies once made.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: DraftProjector, Source: "cognition"}, Title: "Cognition graph drafts",
		Description: "Each Cognition graph's draft, as its bp.* commands left it: {graph, rev, published, publishedRev, updatedBy, updatedAt}",
		Area:        live.AreaDefinitions, Types: []string{TargetType}, Fold: fold,
	})
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: PublishedProjector, Source: "cognition"}, Title: "Published Cognition graphs",
		Description: "Each Cognition graph's last published version, the one that runs",
		Area:        live.AreaDefinitions, Types: []string{TargetType}, Fold: foldPublished,
	})

	for _, s := range commandSpecs() {
		s := s
		action := s.action
		if action == "" {
			action = "cognition.write"
		}
		live.Commands.Register(reg, live.Command{
			Descriptor: extensions.Descriptor{ID: s.id, Source: "cognition"}, Title: s.title, Description: s.description,
			Args: json.RawMessage(s.args), Examples: []json.RawMessage{json.RawMessage(s.example)}, Events: s.events,
			Area: live.AreaDefinitions, TargetType: TargetType, Projection: DraftProjector, Action: action,
			Resource: func(p features.Principal, t live.Target) string { return Resource(p, t.ID) },
			Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
				c, problems := load(d, deps(), s.create)
				if problems != nil {
					return nil, problems
				}
				events, problems := s.decide(c)
				if problems != nil || len(events) == 0 {
					return nil, problems
				}
				return append(c.prefix, events...), nil
			},
			Report: func(args features.ServiceArgs, t live.Target, seq uint64) any { return report(args, deps(), t.ID, seq) },
		})
	}
	contributeQueries(reg, deps)
	contributeTools(reg, deps)
}

// report is a draft's problems once a commit at seq is projected, for the
// commands' tools.
func report(args features.ServiceArgs, deps Deps, id string, seq uint64) any {
	v, err := deps.Projections.WaitFor(args.Context(), args.Principal.TenantID, DraftProjector, TargetType+"."+id, seq, 2*time.Second)
	if err != nil || v.Value == nil {
		return nil
	}
	var d Draft
	if json.Unmarshal(v.Value, &d) != nil {
		return nil
	}
	res := deps.Engine.Resolve(d.Graph)
	problems := res.Problems
	if problems == nil {
		problems = []cognition.Problem{}
	}
	return map[string]any{"rev": d.Rev, "published": d.Published, "problems": problems}
}

func decode2[T any](c *decision) (T, []live.Problem) {
	v, err := decode[T](c.Args)
	if err != nil {
		return v, problem("args", err.Error(), "")
	}
	return v, nil
}

func ev(typ string, data any) []live.NewEvent { return []live.NewEvent{{Type: typ, Data: data}} }

func commandSpecs() []spec {
	return []spec{
		{
			id: "bp.create", title: "Create a graph", create: true, events: []string{EventCreated},
			description: "Makes a new Cognition graph (a blueprint) with the target as its id: of a kind (bp.listKinds), with its name, or as a copy of another graph (from). A kind with one event starts with that event node.",
			args:        `{"type":"object","properties":{"kind":{"type":"string","description":"The graph kind, e.g. ui.handler"},"name":{"type":"string"},"description":{"type":"string"},"owner":{"type":"string","description":"What it belongs to, e.g. app:hello"},"spec":{"type":"object","description":"What the kind needs beyond the nodes (bp.listKinds)"},"from":{"type":"string","description":"A graph to copy"}}}`,
			example:     `{"kind":"ui.handler","name":"Reset the counter","owner":"app:hello"}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				if c.exists {
					return nil, problem("target", fmt.Sprintf("there is already a graph %q", c.Target.ID), "choose another id, or change it with the other bp.* commands")
				}
				if b, ok := c.deps.Engine.BuiltIn(c.Target.ID); ok {
					return nil, problem("target", fmt.Sprintf("%q is the id of the built-in graph %s", c.Target.ID, b.Graph.Name), "choose another id")
				}
				a, problems := decode2[struct {
					Kind, Name, Description, Owner, From string
					Spec                                 json.RawMessage
				}](c)
				if problems != nil {
					return nil, problems
				}
				var g cognition.Graph
				if a.From != "" {
					src, ok := lookup(c.ServiceArgs, c.deps, a.From)
					if !ok {
						return nil, problem("args.from", fmt.Sprintf("there is no graph %q to copy", a.From), "")
					}
					g = src
					if a.Name == "" {
						a.Name = "Copy of " + src.Name
					}
				} else {
					k, ok := c.catalog.Kind(a.Kind)
					if !ok {
						return nil, problem("args.kind", fmt.Sprintf("there is no graph kind %q", a.Kind), "list the kinds with bp.listKinds")
					}
					g = cognition.Graph{Kind: k.ID, Nodes: []cognition.Node{}, Edges: []cognition.Edge{}}
					if len(k.Events) == 1 {
						g.Nodes = append(g.Nodes, cognition.Node{ID: "event", Type: k.Events[0]})
					}
				}
				if a.Name == "" {
					return nil, problem("args.name", "a graph needs a name", "")
				}
				g.ID, g.Name, g.Version = c.Target.ID, a.Name, 0
				if a.Description != "" {
					g.Description = a.Description
				}
				if a.Owner != "" {
					g.Owner = a.Owner
				}
				if a.Spec != nil {
					g.Spec = a.Spec
				}
				if problems := checkSpec(c.catalog, g); problems != nil {
					return nil, problems
				}
				return ev(EventCreated, map[string]any{"graph": g}), nil
			},
		},
		{
			id: "bp.import", title: "Import a graph", create: true, events: []string{EventCreated},
			description: "Replaces the target graph's draft with a graph exported as JSON (its nodes, edges, variables and spec), or makes it. Publish it to run it.",
			args:        `{"type":"object","properties":{"graph":{"type":"object","description":"A graph as bp.* queries return it: {name, kind, nodes, edges, …}"}},"required":["graph"]}`,
			example:     `{"graph":{"name":"Greeting","kind":"function","nodes":[],"edges":[]}}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				if b, ok := c.deps.Engine.BuiltIn(c.Target.ID); ok && !b.Editable {
					return nil, problem("target", fmt.Sprintf("%s is built in and read-only", b.Graph.Name), "import it under another id")
				}
				a, problems := decode2[struct{ Graph cognition.Graph }](c)
				if problems != nil {
					return nil, problems
				}
				g := a.Graph
				if _, ok := c.catalog.Kind(g.Kind); !ok {
					return nil, problem("args.graph.kind", fmt.Sprintf("there is no graph kind %q", g.Kind), "list the kinds with bp.listKinds")
				}
				g.ID, g.Version = c.Target.ID, 0
				if g.Name == "" {
					g.Name = c.Target.ID
				}
				return ev(EventCreated, map[string]any{"graph": g}), nil
			},
		},
		{
			id: "bp.addNode", title: "Add a node", events: []string{EventNodeAdded},
			description: "Adds a node of a type (bp.listNodeTypes) to a graph's draft, with its config; without a position it is placed right of the others. Returns the graph's problems, such as inputs still to connect.",
			args:        `{"type":"object","properties":{"type":{"type":"string"},"id":{"type":"string","description":"Its id; one is chosen when left out"},"title":{"type":"string"},"config":{"type":"object"},"position":` + position + `},"required":["type"]}`,
			example:     `{"type":"ui.toast","config":{"severity":"success","message":"Counter reset"}}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := decode2[struct {
					Type, ID, Title string
					Config          json.RawMessage
					Position        *cognition.Position
				}](c)
				if problems != nil {
					return nil, problems
				}
				n := cognition.Node{ID: a.ID, Type: a.Type, Title: a.Title, Config: a.Config}
				if n.ID == "" {
					n.ID = nextID(*c.graph())
				} else if !live.ValidToken(n.ID) {
					return nil, problem("args.id", fmt.Sprintf("%q is not a valid node id", n.ID), "use letters, digits, '-' and '_'")
				} else if _, dup := c.graph().NodeByID(n.ID); dup {
					return nil, problem("args.id", fmt.Sprintf("there is already a node %q", n.ID), "leave the id out to get a new one")
				}
				if a.Position != nil {
					n.Position = *a.Position
				} else {
					n.Position = place(*c.graph())
				}
				if problems := c.prepare(&n, "args"); problems != nil {
					return nil, problems
				}
				return ev(EventNodeAdded, map[string]any{"node": n}), nil
			},
		},
		{
			id: "bp.setConfig", title: "Configure a node", events: []string{EventNodeChanged},
			description: "Sets a node's config (replacing it) and title; its pins may change with its config, and edges to pins it no longer has become problems.",
			args:        `{"type":"object","properties":{"node":{"type":"string"},"config":{"type":"object"},"title":{"type":"string"}},"required":["node"]}`,
			example:     `{"node":"n3","config":{"message":"Counter reset"}}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := decode2[NodeChange](c)
				if problems != nil {
					return nil, problems
				}
				i := nodeIndex(*c.graph(), a.Node)
				if i < 0 {
					return nil, problem("args.node", fmt.Sprintf("there is no node %q", a.Node), "")
				}
				if a.Config != nil {
					n := c.graph().Nodes[i]
					n.Config = a.Config
					if problems := c.prepare(&n, "args"); problems != nil {
						return nil, problems
					}
					a.Config = n.Config
				}
				return ev(EventNodeChanged, a), nil
			},
		},
		{
			id: "bp.moveNodes", title: "Move nodes", events: []string{EventNodesMoved},
			description: "Moves nodes of a graph's draft on the canvas.",
			args:        `{"type":"object","properties":{"moves":{"type":"array","items":{"type":"object","properties":{"node":{"type":"string"},"position":` + position + `},"required":["node","position"]}}},"required":["moves"]}`,
			example:     `{"moves":[{"node":"n3","position":{"x":640,"y":0}}]}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := decode2[struct{ Moves []Move }](c)
				if problems != nil {
					return nil, problems
				}
				a.Moves = slices.DeleteFunc(a.Moves, func(m Move) bool { return nodeIndex(*c.graph(), m.Node) < 0 })
				if len(a.Moves) == 0 {
					return nil, nil
				}
				return ev(EventNodesMoved, a), nil
			},
		},
		{
			id: "bp.connect", title: "Connect two pins", events: []string{EventConnected, EventDisconnected},
			description: "Joins an output pin to an input pin. A data input takes one edge and an exec output leads to one node, so an edge already there is replaced. Pins must both be exec or both data, of the same type, any, or a type that converts (shown on the edge).",
			args:        `{"type":"object","properties":{"from":` + pinRef + `,"to":` + pinRef + `},"required":["from","to"]}`,
			example:     `{"from":{"node":"event","pin":"then"},"to":{"node":"n2","pin":"then"}}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				e, problems := decode2[cognition.Edge](c)
				if problems != nil {
					return nil, problems
				}
				from, ok := c.pins(e.From.Node)
				if !ok {
					return nil, problem("args.from.node", fmt.Sprintf("there is no node %q", e.From.Node), "")
				}
				to, ok := c.pins(e.To.Node)
				if !ok {
					return nil, problem("args.to.node", fmt.Sprintf("there is no node %q", e.To.Node), "")
				}
				out, ok := from.Find(e.From.Pin, false)
				if !ok {
					return nil, problem("args.from.pin", fmt.Sprintf("%s has no output %q", e.From.Node, e.From.Pin), "its outputs are "+pinList(from.Out))
				}
				in, ok := to.Find(e.To.Pin, true)
				if !ok {
					return nil, problem("args.to.pin", fmt.Sprintf("%s has no input %q", e.To.Node, e.To.Pin), "its inputs are "+pinList(to.In))
				}
				if e.From.Node == e.To.Node {
					return nil, problem("args.to", "a node cannot be joined to itself", "")
				}
				if out.Exec != in.Exec {
					return nil, problem("args.to.pin", "an exec pin cannot be joined to a data pin", "join exec pins (then, true, body…) to exec pins and data pins to data pins")
				}
				if !out.Exec && !c.catalog.Connects(out.Type, in.Type) {
					return nil, problem("args.to.pin", fmt.Sprintf("%s carries a %s, but %s takes a %s", out.Name, out.Type, in.Name, in.Type), "feed it from a pin of type "+in.Type+", or convert the value first, e.g. with an Expression node")
				}
				if slices.Contains(c.graph().Edges, e) {
					return nil, nil
				}
				var events []live.NewEvent
				for _, old := range c.graph().Edges {
					if (out.Exec && old.From == e.From) || (!in.Exec && old.To == e.To) {
						events = append(events, live.NewEvent{Type: EventDisconnected, Data: map[string]any{"edge": old}})
					}
				}
				return append(events, live.NewEvent{Type: EventConnected, Data: map[string]any{"edge": e}}), nil
			},
		},
		{
			id: "bp.disconnect", title: "Disconnect an edge", events: []string{EventDisconnected},
			description: "Removes the edge between two pins.",
			args:        edgeSchema,
			example:     `{"from":{"node":"event","pin":"then"},"to":{"node":"n2","pin":"then"}}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				e, problems := decode2[cognition.Edge](c)
				if problems != nil {
					return nil, problems
				}
				if !slices.Contains(c.graph().Edges, e) {
					return nil, nil
				}
				return ev(EventDisconnected, map[string]any{"edge": e}), nil
			},
		},
		{
			id: "bp.remove", title: "Remove nodes and edges", events: []string{EventRemoved},
			description: "Removes nodes, with every edge to them, and edges.",
			args:        `{"type":"object","properties":{"nodes":{"type":"array","items":{"type":"string"}},"edges":{"type":"array","items":` + edgeSchema + `}}}`,
			example:     `{"nodes":["n3"]}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				r, problems := decode2[Removal](c)
				if problems != nil {
					return nil, problems
				}
				r.Nodes = slices.DeleteFunc(r.Nodes, func(id string) bool { return nodeIndex(*c.graph(), id) < 0 })
				r.Edges = slices.DeleteFunc(r.Edges, func(e cognition.Edge) bool { return !slices.Contains(c.graph().Edges, e) })
				if len(r.Nodes) == 0 && len(r.Edges) == 0 {
					return nil, nil
				}
				return ev(EventRemoved, r), nil
			},
		},
		{
			id: "bp.paste", title: "Paste nodes", events: []string{EventPasted},
			description: "Adds nodes and the edges between them at once, as copied from this or another graph; the nodes' ids must be new to the graph. Undoing a removal pastes what was removed.",
			args:        `{"type":"object","properties":{"nodes":{"type":"array","items":` + nodeSchema + `},"edges":{"type":"array","items":` + edgeSchema + `}},"required":["nodes"]}`,
			example:     `{"nodes":[{"id":"n7","type":"ui.toast","position":{"x":0,"y":200},"config":{"message":"Hi"}}],"edges":[]}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				f, problems := decode2[Fragment](c)
				if problems != nil {
					return nil, problems
				}
				ids := map[string]bool{}
				for i := range f.Nodes {
					n := &f.Nodes[i]
					if !live.ValidToken(n.ID) || ids[n.ID] || nodeIndex(*c.graph(), n.ID) >= 0 {
						return nil, problem(fmt.Sprintf("args.nodes.%d.id", i), fmt.Sprintf("%q is not a new, valid node id", n.ID), "give pasted nodes new ids")
					}
					ids[n.ID] = true
					if problems := c.prepare(n, fmt.Sprintf("args.nodes.%d", i)); problems != nil {
						return nil, problems
					}
				}
				for i, e := range f.Edges {
					known := func(id string) bool { return ids[id] || nodeIndex(*c.graph(), id) >= 0 }
					if !known(e.From.Node) || !known(e.To.Node) {
						return nil, problem(fmt.Sprintf("args.edges.%d", i), "an edge joins a node that is neither pasted nor in the graph", "")
					}
				}
				if f.Edges == nil {
					f.Edges = []cognition.Edge{}
				}
				return ev(EventPasted, f), nil
			},
		},
		{
			id: "bp.addVariable", title: "Add a variable", events: []string{EventVariableSet},
			description: "Adds a variable to a graph (or changes the one with this id): per-run state, read by Get variable and written by Set variable.",
			args:        `{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"},"type":{"type":"string"},"default":{},"description":{"type":"string"}},"required":["name","type"]}`,
			example:     `{"name":"Attempts","type":"number","default":0}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				v, problems := decode2[cognition.Variable](c)
				if problems != nil {
					return nil, problems
				}
				if v.ID == "" {
					v.ID = strings.ToLower(strings.Join(strings.Fields(v.Name), "_"))
				}
				if !live.ValidToken(v.ID) {
					return nil, problem("args.id", fmt.Sprintf("%q is not a valid variable id", v.ID), "use letters, digits, '-' and '_'")
				}
				return ev(EventVariableSet, map[string]any{"variable": v}), nil
			},
		},
		{
			id: "bp.removeVariable", title: "Remove a variable", events: []string{EventVariableRemoved},
			description: "Removes a graph's variable; the Get and Set variable nodes that use it stay, reading nothing.",
			args:        `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`,
			example:     `{"id":"attempts"}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				v, problems := decode2[struct{ ID string }](c)
				if problems != nil {
					return nil, problems
				}
				if _, ok := c.graph().VariableByID(v.ID); !ok {
					return nil, nil
				}
				return ev(EventVariableRemoved, map[string]any{"id": v.ID}), nil
			},
		},
		{
			id: "bp.setInfo", title: "Describe a graph", events: []string{EventInfoSet},
			description: "Sets a graph's name, description, owner and spec (what its kind needs, e.g. a command graph's command type; bp.listKinds).",
			args:        `{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},"owner":{"type":"string"},"spec":{"type":"object"}}}`,
			example:     `{"description":"Sets the counter to zero"}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				in, problems := decode2[Info](c)
				if problems != nil {
					return nil, problems
				}
				if in.Name != nil && *in.Name == "" {
					return nil, problem("args.name", "a graph needs a name", "")
				}
				if in.Spec != nil {
					g := *c.graph()
					g.Spec = in.Spec
					if problems := checkSpec(c.catalog, g); problems != nil {
						return nil, problems
					}
				}
				return ev(EventInfoSet, in), nil
			},
		},
		{
			id: "bp.publish", title: "Publish a graph", events: []string{EventPublished}, action: "cognition.publish",
			description: "Publishes the draft as a new version, which is what runs from then on, in open sessions at their next event. rev is the draft's rev the publisher last saw: a change made since is not published unseen. A draft with errors is not published.",
			args:        `{"type":"object","properties":{"rev":{"type":"integer","description":"The draft's rev as last seen (bp.* results and the cognition.graph query give it)"}},"required":["rev"]}`,
			example:     `{"rev":12}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				a, problems := decode2[struct{ Rev int }](c)
				if problems != nil {
					return nil, problems
				}
				// A draft started from a built-in in this command has rev 1 once made
				if rev := c.draft.Rev + len(c.prefix); a.Rev != rev {
					return nil, problem("args.rev", fmt.Sprintf("the draft changed since rev %d: it is at rev %d", a.Rev, rev), "look at the changes, then publish rev "+strconv.Itoa(rev))
				}
				res := c.deps.Engine.Resolve(*c.graph())
				var errs []live.Problem
				for _, p := range res.Problems {
					if p.Severity == cognition.SeverityError {
						path := "graph"
						if p.NodeID != "" {
							path += "." + p.NodeID
							if p.Pin != "" {
								path += "." + p.Pin
							}
						}
						errs = append(errs, live.Problem{Path: path, Message: p.Message})
					}
				}
				if errs != nil {
					return nil, errs
				}
				g := *c.graph()
				g.Version = c.draft.Published + 1
				return ev(EventPublished, Publication{Version: g.Version, Graph: g}), nil
			},
		},
		{
			id: "bp.delete", title: "Delete a graph", events: []string{EventDeleted},
			description: "Deletes a graph's draft and published versions; an editable built-in returns to its built-in version.",
			args:        `{"type":"object","properties":{}}`,
			example:     `{}`,
			decide: func(c *decision) ([]live.NewEvent, []live.Problem) {
				if len(c.prefix) > 0 {
					return nil, nil // nothing of the tenant's to delete
				}
				return ev(EventDeleted, map[string]any{}), nil
			},
		},
	}
}

// checkSpec checks a graph's spec against its kind's schema.
func checkSpec(cat cognition.Catalog, g cognition.Graph) []live.Problem {
	k, ok := cat.Kind(g.Kind)
	if !ok || len(k.Spec) == 0 {
		return nil
	}
	spec := g.Spec
	if spec == nil {
		spec = json.RawMessage(`{}`)
	}
	if err := tools.CheckArgs(k.Spec, spec); err != nil {
		return problem("args.spec", "the spec is not valid: "+err.Error(), "describe the kind with bp.listKinds")
	}
	if k.IDFor != nil {
		ids, err := k.IDFor(spec, g.Owner)
		if err != nil {
			return problem("args.spec", err.Error(), "")
		}
		if !slices.Contains(ids, g.ID) {
			return problem("target", fmt.Sprintf("a %s graph with this spec must have the id %q", k.Title, ids[0]), fmt.Sprintf("create it with the target %q", ids[0]))
		}
	}
	return nil
}

func pinList(pins []cognition.Pin) string {
	ids := make([]string, len(pins))
	for i, p := range pins {
		ids[i] = p.ID
	}
	return strings.Join(ids, ", ")
}
