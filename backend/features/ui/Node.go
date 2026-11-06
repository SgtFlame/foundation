// Package ui is the server-driven UI library (documentation/ServerUI.md):
// the component tree, the ui.component, ui.page, ui.app and ui.editorKind
// points, UI sessions with binding tracking, and the patches that keep a
// browser's copy of a tree current.
package ui

import (
	"maps"
	"reflect"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/logging"
)

// Log is the ui feature's logging and error builder.
var Log = logging.In("ui")

// Node is one component in a tree.
type Node struct {
	Type  string         `json:"type"`
	Key   string         `json:"key"`
	Props map[string]any `json:"props,omitempty"`
	// Bind reads props by path: "state.<path>" (view state),
	// "query:<name>[.<path>]" (a query the page holds) or "local.<path>"
	// (the browser's own state). The server resolves the first two into
	// Props; the browser resolves local ones.
	Bind     map[string]string  `json:"bind,omitempty"`
	On       map[string]On      `json:"on,omitempty"`
	Children []*Node            `json:"children,omitempty"`
	Slots    map[string][]*Node `json:"slots,omitempty"`
	// Src is the key path of the template node it was drawn from, in a
	// design session (forge 04c): the Prefab Editor's Canvas selects by it.
	Src string `json:"src,omitempty"`
}

// On maps a component event to a handler. Payload values that are strings
// starting with "local." or "event." are filled in by the browser.
type On struct {
	Handler string         `json:"handler"`
	Payload map[string]any `json:"payload,omitempty"`
}

// N makes a node: ui.N("mui.button", "save").P("label", "Save").
func N(typ, key string, children ...*Node) *Node {
	return &Node{Type: typ, Key: key, Children: children}
}

// P sets props, in name/value pairs.
func (n *Node) P(kv ...any) *Node {
	if n.Props == nil {
		n.Props = map[string]any{}
	}
	for i := 0; i+1 < len(kv); i += 2 {
		n.Props[kv[i].(string)] = kv[i+1]
	}
	return n
}

// B binds a prop to a path.
func (n *Node) B(prop, path string) *Node {
	if n.Bind == nil {
		n.Bind = map[string]string{}
	}
	n.Bind[prop] = path
	return n
}

// E maps an event to a handler, with a payload in name/value pairs.
func (n *Node) E(event, handler string, payload ...any) *Node {
	if n.On == nil {
		n.On = map[string]On{}
	}
	o := On{Handler: handler}
	for i := 0; i+1 < len(payload); i += 2 {
		if o.Payload == nil {
			o.Payload = map[string]any{}
		}
		o.Payload[payload[i].(string)] = payload[i+1]
	}
	n.On[event] = o
	return n
}

// C appends children.
func (n *Node) C(children ...*Node) *Node {
	n.Children = append(n.Children, children...)
	return n
}

// S fills a named slot.
func (n *Node) S(slot string, children ...*Node) *Node {
	if n.Slots == nil {
		n.Slots = map[string][]*Node{}
	}
	n.Slots[slot] = append(n.Slots[slot], children...)
	return n
}

// Clone copies the node and its subtree.
func (n *Node) Clone() *Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Props = maps.Clone(n.Props)
	c.Bind = maps.Clone(n.Bind)
	c.On = maps.Clone(n.On)
	c.Children = make([]*Node, len(n.Children))
	for i, ch := range n.Children {
		c.Children[i] = ch.Clone()
	}
	if n.Children == nil {
		c.Children = nil
	}
	if n.Slots != nil {
		c.Slots = map[string][]*Node{}
		for k, v := range n.Slots {
			for _, ch := range v {
				c.Slots[k] = append(c.Slots[k], ch.Clone())
			}
		}
	}
	return &c
}

// Child is the child with key, or nil.
func (n *Node) Child(key string) *Node {
	for _, c := range n.Children {
		if c.Key == key {
			return c
		}
	}
	return nil
}

// At finds the node at a key path below n; the empty path is n.
func (n *Node) At(path []string) *Node {
	cur := n
	for _, k := range path {
		if cur == nil {
			return nil
		}
		cur = cur.Child(k)
	}
	return cur
}

// Walk visits n and its children, depth first, with each one's key path.
func (n *Node) Walk(fn func(path []string, node *Node)) {
	var walk func(path []string, node *Node)
	walk = func(path []string, node *Node) {
		fn(path, node)
		for _, c := range node.Children {
			walk(append(append([]string(nil), path...), c.Key), c)
		}
	}
	walk(nil, n)
}

// PathString joins a key path for indexing.
func PathString(path []string) string { return strings.Join(path, "\x00") }

func equalJSON(a, b any) bool { return reflect.DeepEqual(a, b) }
