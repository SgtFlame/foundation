package ui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Tree overlays (forge 02-Blueprints) let commands add nodes to a page a
// render function draws, such as the chat adding a button to the Hello
// app, until pages are prefabs (forge 04-Prefabs). A page's overlay is an
// aggregate on the def stream, its target the page id with "-" for "/";
// every session of the page holds the ui.overlay query and draws the page
// with its inserts.

// OverlayType is an overlay's target type.
const OverlayType = "pageTree"

// overlayQuery is the name a session holds its page's overlay under.
const overlayQuery = "__overlay"

// Insert is one node an overlay adds: under the node at Parent (a key path
// from the root, joined by "/"), at Index (the end when negative).
type Insert struct {
	Parent string `json:"parent"`
	Index  int    `json:"index"`
	Node   *Node  `json:"node"`
}

// Overlay is a page's overlay: its inserts, in order.
type Overlay struct {
	Inserts []Insert `json:"inserts"`
}

// OverlayID is the overlay target id of a page.
func OverlayID(page string) string { return strings.ReplaceAll(page, "/", "-") }

// ContributeOverlays registers the ui.overlay projector and query and the
// ui.insertNode and ui.removeNode commands.
func ContributeOverlays(reg *extensions.Registry) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: "ui.overlay", Source: "ui"}, Title: "Page overlays",
		Description: "Each page's inserted nodes: {inserts: [{parent, index, node}]}",
		Area:        live.AreaDefinitions, Types: []string{OverlayType},
		Fold: func(f live.Fold) (json.RawMessage, error) {
			var o Overlay
			if f.State != nil {
				_ = json.Unmarshal(f.State, &o)
			}
			switch f.Event.Type {
			case "inserted":
				var in Insert
				if err := json.Unmarshal(f.Event.Data, &in); err != nil {
					return nil, err
				}
				o.Inserts = append(o.Inserts, in)
			case "removed":
				var r struct{ Key string }
				_ = json.Unmarshal(f.Event.Data, &r)
				o.Inserts = slices.DeleteFunc(o.Inserts, func(in Insert) bool { return in.Node != nil && in.Node.Key == r.Key })
			}
			return json.Marshal(o)
		},
	})
	pageOf := func(id string) (Page, bool) {
		for _, p := range Pages.All(reg) {
			if OverlayID(p.ID) == id {
				return p, true
			}
		}
		return Page{}, false
	}
	overlayOf := func(state json.RawMessage) Overlay {
		var o Overlay
		if state != nil {
			_ = json.Unmarshal(state, &o)
		}
		return o
	}
	live.Commands.Register(reg, live.Command{
		Descriptor: extensions.Descriptor{ID: "ui.insertNode", Source: "ui"}, Title: "Add a node to a page",
		Description: "Adds a component node to a page's tree, under the node at parent (a key path from the root joined by /, e.g. root/top/counter/row) at index (the end when left out); everyone with the page open sees it. Its events name handlers: a ui.handler graph's id. Check a tree with ui.previewTree first. The target is the page id with - for /, e.g. hello-main.",
		Args:        json.RawMessage(`{"type":"object","properties":{"parent":{"type":"string"},"index":{"type":"integer"},"node":{"type":"object","description":"A ui.Node: {type, key, props, bind, on: {event: {handler, payload}}, children}"}},"required":["parent","node"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"parent":"root/top/counter/row","node":{"type":"mui.button","key":"reset","props":{"label":"Reset"},"on":{"click":{"handler":"hello-onReset"}}}}`)},
		Events:      []string{"inserted"}, Area: live.AreaDefinitions, TargetType: OverlayType, Projection: "ui.overlay", Action: "ui.design",
		Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
			if _, ok := pageOf(d.Target.ID); !ok {
				return nil, []live.Problem{{Path: "target", Message: fmt.Sprintf("there is no page %q", d.Target.ID), Fix: "use a page id with - for /, e.g. hello-main"}}
			}
			var in Insert
			in.Index = -1
			if err := json.Unmarshal(d.Args, &in); err != nil || in.Node == nil {
				return nil, []live.Problem{{Path: "args.node", Message: "the node is missing or not valid"}}
			}
			if problems := Validate(Catalog(reg), in.Node); len(problems) > 0 {
				p := problems[0]
				return nil, []live.Problem{{Path: "args.node." + strings.Join(p.Path, "."), Message: p.Message, Fix: "describe the component with ui.describeComponent"}}
			}
			if slices.ContainsFunc(overlayOf(d.State).Inserts, func(x Insert) bool { return x.Node.Key == in.Node.Key }) {
				return nil, []live.Problem{{Path: "args.node.key", Message: fmt.Sprintf("the page already has an added node %q", in.Node.Key), Fix: "remove it with ui.removeNode, or choose another key"}}
			}
			return []live.NewEvent{{Type: "inserted", Data: in}}, nil
		},
	})
	live.Commands.Register(reg, live.Command{
		Descriptor: extensions.Descriptor{ID: "ui.removeNode", Source: "ui"}, Title: "Remove an added node",
		Description: "Removes a node ui.insertNode added to a page, by its key.",
		Args:        json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"}},"required":["key"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"key":"reset"}`)},
		Events:      []string{"removed"}, Area: live.AreaDefinitions, TargetType: OverlayType, Projection: "ui.overlay", Action: "ui.design",
		Decide: func(d live.Decision) ([]live.NewEvent, []live.Problem) {
			var r struct{ Key string }
			_ = json.Unmarshal(d.Args, &r)
			if !slices.ContainsFunc(overlayOf(d.State).Inserts, func(x Insert) bool { return x.Node.Key == r.Key }) {
				return nil, nil
			}
			return []live.NewEvent{{Type: "removed", Data: r}}, nil
		},
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "ui.overlay", Source: "ui"}, Title: "A page's added nodes",
		Description: "The nodes ui.insertNode added to a page: {inserts: [{parent, index, node}]}.",
		Params:      json.RawMessage(`{"type":"object","properties":{"page":{"type":"string","description":"The page id, e.g. hello/main"}},"required":["page"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"page":"hello/main"}`)},
		Action:      "ui.read",
		Run: func(f live.Feed) error {
			return live.ValueOf("ui.overlay", func(f live.Feed) (string, error) {
				var p struct{ Page string }
				_ = json.Unmarshal(f.Params(), &p)
				id := OverlayID(p.Page)
				if !live.ValidToken(id) {
					return "", fmt.Errorf("%q is not a page", p.Page)
				}
				return OverlayType + "." + id, nil
			}, json.RawMessage(`{"inserts":[]}`))(f)
		},
	})
}

// applyOverlay adds an overlay's inserts to a rendered tree.
func applyOverlay(root *Node, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	var o Overlay
	if json.Unmarshal(raw, &o) != nil {
		return
	}
	for _, in := range o.Inserts {
		if in.Node == nil {
			continue
		}
		parts := strings.Split(strings.Trim(in.Parent, "/"), "/")
		if len(parts) > 0 && parts[0] == root.Key {
			parts = parts[1:]
		}
		parent := root
		if len(parts) > 0 && parts[0] != "" {
			parent = root.At(parts)
		}
		if parent == nil || slices.ContainsFunc(parent.Children, func(c *Node) bool { return c.Key == in.Node.Key }) {
			continue
		}
		n := clone(in.Node)
		i := in.Index
		if i < 0 || i > len(parent.Children) {
			i = len(parent.Children)
		}
		parent.Children = slices.Insert(parent.Children, i, n)
	}
}

func clone(n *Node) *Node {
	raw, _ := json.Marshal(n)
	var out Node
	_ = json.Unmarshal(raw, &out)
	return &out
}
