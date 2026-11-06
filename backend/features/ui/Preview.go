package ui

import (
	"maps"
	"slices"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// Preview evaluates a prefab once (forge 04b › Preview): its draft, or its
// last published version, with props (its preview data when none), and
// gives the evaluated tree and its problems with their key paths. Its
// queries are read once and no logic runs. o needs Catalog and Subs.
func Preview(args features.ServiceArgs, o Options, id string, props map[string]any, draft bool) (*Node, []Problem, *features.ServiceResponse) {
	page := Page{Descriptor: extensions.Descriptor{ID: "prefab/" + id}, Title: id, prefab: id, Render: func(r *Render) *Node {
		e := &evaluator{s: r.s, samples: props == nil}
		if draft {
			e.draft = id
		}
		return e.root(r, id, props)
	}}
	var tree *Node
	o.Send = func(f any) {
		if t, ok := f.(TreeFrame); ok {
			tree = t.Tree
		}
	}
	o.Graphs, o.Store = nil, nil
	s, fail := Open(args, "preview-"+newID(), page, nil, nil, o)
	if fail != nil {
		return nil, nil, fail
	}
	s.Close(false)
	return tree, TreeProblems(tree), nil
}

// TreeProblems are the ui.error nodes of an evaluated tree, at their key
// paths from the root.
func TreeProblems(root *Node) []Problem {
	out := []Problem{}
	var walk func(path []string, n *Node)
	walk = func(path []string, n *Node) {
		if n.Type == "ui.error" {
			msgs, _ := n.Props["messages"].([]string)
			for _, m := range msgs {
				out = append(out, Problem{Path: path, Message: m})
			}
		}
		for _, c := range n.Children {
			walk(append(slices.Clone(path), c.Key), c)
		}
		for _, s := range slices.Sorted(maps.Keys(n.Slots)) {
			for _, c := range n.Slots[s] {
				walk(append(slices.Clone(path), c.Key), c)
			}
		}
	}
	if root != nil {
		walk([]string{root.Key}, root)
	}
	return out
}
