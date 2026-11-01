package ui

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Components is the ui.component point: the primitive components. The
// browser registers a renderer under the same id on its ui.renderer point.
var Components = extensions.NewPoint[Component]("ui.component", "Primitive UI components a server-described tree may use, with their props schema and events")

// Component describes one primitive. Its Descriptor's ID is the type, e.g.
// "mui.button".
type Component struct {
	extensions.Descriptor
	Title       string
	Description string // for a model: what it shows and when to use it
	Category    string // e.g. "Input", "Layout", "Graph"
	Tags        []string
	Props       json.RawMessage // the props' JSON Schema, an object
	Events      []Event
	Slots       []string // named slots it fills
	Children    bool     // takes children
	Examples    []json.RawMessage
}

// Event is one event a component emits, and what its payload holds.
type Event struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// RegisterComponent checks a component's metadata and registers it; a
// component without its title, description, category, props schema or an
// example fails (forge Architecture rule 4).
func RegisterComponent(reg *extensions.Registry, c Component) error {
	switch {
	case c.ID == "":
		return Log.Errorf("a component needs an id")
	case c.Title == "" || c.Description == "" || c.Category == "":
		return Log.Errorf("component %s needs a title, a description and a category", c.ID)
	case len(c.Props) == 0 || !json.Valid(c.Props):
		return Log.Errorf("component %s needs a props schema", c.ID)
	case len(c.Examples) == 0:
		return Log.Errorf("component %s needs an example", c.ID)
	}
	for _, e := range c.Events {
		if e.Name == "" || e.Description == "" {
			return Log.Errorf("component %s: every event needs a name and a description", c.ID)
		}
	}
	Components.Register(reg, c)
	return nil
}

// Problem is what is wrong with a tree, at a key path.
type Problem struct {
	Path    []string `json:"path"`
	Message string   `json:"message"`
	Fix     string   `json:"fix,omitempty"`
}

// Catalog indexes the components by type.
func Catalog(reg *extensions.Registry) map[string]Component {
	out := map[string]Component{}
	for _, c := range Components.All(reg) {
		out[c.ID] = c
	}
	return out
}

// Validate reports a tree's problems: unknown components, invalid props,
// missing or repeated keys, and children or slots a component does not take.
func Validate(catalog map[string]Component, root *Node) []Problem {
	var problems []Problem
	var check func(path []string, n *Node)
	check = func(path []string, n *Node) {
		c, ok := catalog[n.Type]
		if !ok {
			problems = append(problems, Problem{Path: path, Message: fmt.Sprintf("there is no component %q", n.Type), Fix: "use one ui.listComponents lists"})
			return
		}
		props := map[string]any{}
		for k, v := range n.Props {
			props[k] = v
		}
		for k := range n.Bind {
			props[k] = nil // present, but its value is whatever the binding gives
		}
		raw, _ := json.Marshal(props)
		if err := tools.CheckArgs(c.Props, raw); err != nil {
			problems = append(problems, Problem{Path: path, Message: fmt.Sprintf("%s props: %v", n.Type, err)})
		}
		if len(n.Children) > 0 && !c.Children {
			problems = append(problems, Problem{Path: path, Message: fmt.Sprintf("%s takes no children", n.Type)})
		}
		for slot := range n.Slots {
			if !slices.Contains(c.Slots, slot) {
				problems = append(problems, Problem{Path: path, Message: fmt.Sprintf("%s has no slot %q", n.Type, slot)})
			}
		}
		for event := range n.On {
			if !slices.ContainsFunc(c.Events, func(e Event) bool { return e.Name == event }) {
				problems = append(problems, Problem{Path: path, Message: fmt.Sprintf("%s emits no event %q", n.Type, event)})
			}
		}
		seen := map[string]bool{}
		for _, ch := range n.Children {
			p := append(append([]string(nil), path...), ch.Key)
			if ch.Key == "" {
				problems = append(problems, Problem{Path: p, Message: "every node needs a key", Fix: "give it a key unique among its siblings"})
			} else if seen[ch.Key] {
				problems = append(problems, Problem{Path: p, Message: fmt.Sprintf("the key %q is used twice among siblings", ch.Key)})
			}
			seen[ch.Key] = true
			check(p, ch)
		}
		for _, list := range n.Slots {
			for _, ch := range list {
				check(append(append([]string(nil), path...), ch.Key), ch)
			}
		}
	}
	check(nil, root)
	return problems
}

// ErrorNode stands in for a node that failed validation, so the browser
// shows an inline error box rather than nothing.
func ErrorNode(key string, problems []Problem) *Node {
	msgs := make([]string, len(problems))
	for i, p := range problems {
		msgs[i] = p.Message
	}
	return N("ui.error", key).P("messages", msgs)
}
