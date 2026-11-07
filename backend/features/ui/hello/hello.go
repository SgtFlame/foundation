// Package hello is the Hello app, which proves the live core, the
// server-driven UI (forge 01-ServerUI) and blueprints (forge 02-Blueprints):
// a shared counter and a shared graph, each one aggregate with its
// projector, and two pages, the app's own tab and an item opened as its
// own top-level tab. Its handlers, the counter's command and its query are
// graphs (graphs/*.json), built in and editable: a tenant's published
// versions replace them. The shared graph's commands and query, and the
// main and item pages' render functions, stay Go until applications (05).
// The task list (tasks.go) reads and writes records (forge 03i), and its
// page is the Task list prefab, made by prefab.* commands (prefabs.go,
// forge 04d).
package hello

import (
	"embed"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/ui"
)

// AppID is the Hello app's id.
const AppID = "hello"

var area = live.AppArea(AppID)

// Counter is the hello.counter aggregate's state.
type Counter struct {
	Value int `json:"value"`
}

// Position is where a graph node is.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// GraphNode is one node of the shared graph.
type GraphNode struct {
	ID       string              `json:"id"`
	Label    string              `json:"label"`
	Position Position            `json:"position"`
	Ports    map[string][]string `json:"ports"`
}

// Edge joins two ports.
type Edge struct {
	ID           string `json:"id"`
	Source       string `json:"source"`
	SourceHandle string `json:"sourceHandle,omitempty"`
	Target       string `json:"target"`
	TargetHandle string `json:"targetHandle,omitempty"`
}

// Graph is the hello.graph aggregate's state.
type Graph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []Edge      `json:"edges"`
}

// NewGraph is a graph before anyone changed it.
func NewGraph() Graph {
	ports := func(in, out bool) map[string][]string {
		p := map[string][]string{}
		if in {
			p["in"] = []string{"in"}
		}
		if out {
			p["out"] = []string{"out"}
		}
		return p
	}
	return Graph{Nodes: []GraphNode{
		{ID: "start", Label: "Start", Position: Position{0, 80}, Ports: ports(false, true)},
		{ID: "greet", Label: "Greet", Position: Position{220, 0}, Ports: ports(true, true)},
		{ID: "count", Label: "Count", Position: Position{220, 160}, Ports: ports(true, true)},
		{ID: "end", Label: "End", Position: Position{440, 80}, Ports: ports(true, false)},
	}, Edges: []Edge{}}
}

func graphOf(state json.RawMessage) Graph {
	if state == nil {
		return NewGraph()
	}
	var g Graph
	if json.Unmarshal(state, &g) != nil {
		return NewGraph()
	}
	if g.Edges == nil {
		g.Edges = []Edge{}
	}
	return g
}

func (g Graph) node(id string) *GraphNode {
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return &g.Nodes[i]
		}
	}
	return nil
}

// EdgeID is the id of the edge joining two ports.
func EdgeID(e Edge) string {
	return fmt.Sprintf("%s.%s-%s.%s", e.Source, e.SourceHandle, e.Target, e.TargetHandle)
}

// ContributeAccess declares the hello resource type.
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "hello", Source: "hello", Order: 95}, Title: "Hello app", Parent: "tenant",
		Actions: []authz.Action{
			{Name: "read", Title: "Open the Hello app", Class: authz.ClassRead},
			{Name: "write", Title: "Change its counter and graph", Class: authz.ClassWrite},
		},
	})
}

func idParam(params json.RawMessage) (string, error) {
	var p struct{ ID string }
	_ = json.Unmarshal(params, &p)
	if p.ID == "" {
		p.ID = "main"
	}
	if !live.ValidToken(p.ID) {
		return "", fmt.Errorf("%q is not a valid id", p.ID)
	}
	return p.ID, nil
}

var idSchema = json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The object's id; main when left out"}}}`)

// Contribute registers the Hello app: its aggregates, pages and editor
// kinds.
func Contribute(reg *extensions.Registry) {
	ui.Apps.Register(reg, ui.App{Descriptor: extensions.Descriptor{ID: AppID, Source: "hello"}, Title: "Hello", Icon: "waving_hand",
		Description: "A small demonstration app: a shared counter, docked sub-tabs and a shared graph, live for everyone who has it open."})

	// The counter
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: "hello.counter", Source: "hello"}, Title: "Hello counters", Description: "Each counter's value",
		Area: area, Types: []string{"counter"},
		Fold: func(f live.Fold) (json.RawMessage, error) {
			var c Counter
			if f.State != nil {
				_ = json.Unmarshal(f.State, &c)
			}
			if f.Event.Type == "incremented" {
				var by struct{ By int }
				_ = json.Unmarshal(f.Event.Data, &by)
				c.Value += by.By
			}
			return json.Marshal(c)
		},
	})
	// The counter's command (hello.increment) and query (hello.counter)
	// are graphs, as are the pages' handlers (graphs/*.json)
	contributeGraphs(reg)

	// The graph
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: "hello.graph", Source: "hello"}, Title: "Hello graphs", Description: "Each graph's nodes and edges",
		Area: area, Types: []string{"graph"},
		Fold: func(f live.Fold) (json.RawMessage, error) {
			g := graphOf(f.State)
			switch f.Event.Type {
			case "connected":
				var e Edge
				_ = json.Unmarshal(f.Event.Data, &e)
				g.Edges = append(g.Edges, e)
			case "disconnected":
				var d struct{ ID string }
				_ = json.Unmarshal(f.Event.Data, &d)
				g.Edges = slices.DeleteFunc(g.Edges, func(e Edge) bool { return e.ID == d.ID })
			case "nodeMoved":
				var m struct {
					ID       string
					Position Position
				}
				_ = json.Unmarshal(f.Event.Data, &m)
				if n := g.node(m.ID); n != nil {
					n.Position = m.Position
				}
			}
			return json.Marshal(g)
		},
	})
	graphCommand := func(id, title, description, args string, events []string, decide func(g Graph, args json.RawMessage) ([]live.NewEvent, []live.Problem)) {
		live.Commands.Register(reg, live.Command{
			Descriptor: extensions.Descriptor{ID: id, Source: "hello"}, Title: title, Description: description, Args: json.RawMessage(args),
			Events: events, Area: area, TargetType: "graph", Projection: "hello.graph", Action: "hello.write",
			Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) { return decide(graphOf(d.State), d.Args) },
		})
	}
	graphCommand("hello.connect", "Connect two nodes", "Joins an output port of one node of a Hello graph to an input port of another.",
		`{"type":"object","properties":{"source":{"type":"string"},"sourceHandle":{"type":"string"},"target":{"type":"string"},"targetHandle":{"type":"string"}},"required":["source","target"]}`,
		[]string{"connected"}, func(g Graph, raw json.RawMessage) ([]live.NewEvent, []live.Problem) {
			var e Edge
			_ = json.Unmarshal(raw, &e)
			if g.node(e.Source) == nil || g.node(e.Target) == nil {
				return nil, []live.Problem{{Path: "args", Message: "both nodes must exist", Fix: "use the ids of the graph's nodes"}}
			}
			if e.Source == e.Target {
				return nil, []live.Problem{{Path: "args.target", Message: "a node cannot be joined to itself"}}
			}
			e.ID = EdgeID(e)
			if slices.ContainsFunc(g.Edges, func(x Edge) bool { return x.ID == e.ID }) {
				return nil, nil // already joined: nothing to commit
			}
			return []live.NewEvent{{Type: "connected", Data: e}}, nil
		})
	graphCommand("hello.disconnect", "Disconnect an edge", "Removes one edge of a Hello graph by its id.",
		`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`,
		[]string{"disconnected"}, func(g Graph, raw json.RawMessage) ([]live.NewEvent, []live.Problem) {
			var d struct{ ID string }
			_ = json.Unmarshal(raw, &d)
			if !slices.ContainsFunc(g.Edges, func(x Edge) bool { return x.ID == d.ID }) {
				return nil, nil
			}
			return []live.NewEvent{{Type: "disconnected", Data: map[string]string{"id": d.ID}}}, nil
		})
	graphCommand("hello.moveNode", "Move a node", "Moves a node of a Hello graph to a position.",
		`{"type":"object","properties":{"id":{"type":"string"},"position":{"type":"object","properties":{"x":{"type":"number"},"y":{"type":"number"}},"required":["x","y"]}},"required":["id","position"]}`,
		[]string{"nodeMoved"}, func(g Graph, raw json.RawMessage) ([]live.NewEvent, []live.Problem) {
			var m struct {
				ID       string
				Position Position
			}
			_ = json.Unmarshal(raw, &m)
			if g.node(m.ID) == nil {
				return nil, []live.Problem{{Path: "args.id", Message: fmt.Sprintf("there is no node %q", m.ID)}}
			}
			return []live.NewEvent{{Type: "nodeMoved", Data: m}}, nil
		})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "hello.graph", Source: "hello"}, Title: "A Hello graph",
		Description: "A Hello graph's nodes and edges: {nodes: [{id, label, position, ports}], edges: [{id, source, sourceHandle, target, targetHandle}]}.",
		Params:      idSchema, Examples: []json.RawMessage{json.RawMessage(`{"id":"main"}`)}, Action: "hello.read",
		Run: func(f live.Feed) error {
			empty, _ := json.Marshal(NewGraph())
			return live.ValueOf("hello.graph", func(f live.Feed) (string, error) {
				id, err := idParam(f.Params())
				return "graph." + id, err
			}, empty)(f)
		},
	})

	contributePages(reg)
}

//go:embed graphs/*.json
var graphFiles embed.FS

// contributeGraphs registers the app's graphs as editable built-ins.
func contributeGraphs(reg *extensions.Registry) {
	entries, err := graphFiles.ReadDir("graphs")
	if err != nil {
		panic(err)
	}
	for i, e := range entries {
		raw, err := graphFiles.ReadFile("graphs/" + e.Name())
		if err != nil {
			panic(err)
		}
		var g cognition.Graph
		if err := json.Unmarshal(raw, &g); err != nil {
			panic(fmt.Sprintf("hello graph %s: %v", e.Name(), err))
		}
		cognition.Graphs.Register(reg, cognition.BuiltIn{Descriptor: extensions.Descriptor{ID: g.ID, Order: 500 + i, Source: "hello"}, Graph: g, Editable: true})
	}
}
