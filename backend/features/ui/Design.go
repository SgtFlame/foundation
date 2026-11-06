package ui

import (
	"encoding/json"

	"github.com/SgtFlame/foundation/backend/features/extensions"
)

// DesignPageID is the page the Prefab Editor's Canvas opens (forge 04c).
const DesignPageID = "prefabs/design"

// DesignPage is a prefab's draft as the Prefab Editor's Canvas draws it
// (forge 04c question 1), with params {prefab}: its preview data as props,
// changing as commands land, and each node tagged with the key path of the
// template node it was drawn from (Node.Src), so a click can select it.
// The page is dry: its handlers and logic run in their graphs' drafts, and
// their commands come back as dryRun actions, never sent.
func DesignPage() Page {
	return Page{
		Descriptor:  extensions.Descriptor{ID: DesignPageID, Source: "ui"},
		App:         "prefabs",
		Title:       "Prefab design",
		Description: "A prefab's draft, drawn with its preview data for the Prefab Editor's Canvas",
		Action:      "prefabs.read",
		Dry:         true,
		prefab:      DesignPageID, // no tree overlay
		Subject: func(params json.RawMessage) Subject {
			id := designed(params)
			return Subject{Type: "prefab", ID: id, Label: id}
		},
		Render: func(r *Render) *Node {
			id := designed(r.s.params)
			e := &evaluator{s: r.s, draft: id, samples: true, design: true}
			return e.instance(r, &scope{}, &Node{Type: PrefabPrefix + id, Key: "root"}, nil, "")
		},
	}
}

// designed is the prefab a design page's params name.
func designed(params json.RawMessage) string {
	var p struct {
		Prefab string `json:"prefab"`
	}
	_ = json.Unmarshal(params, &p)
	return p.Prefab
}

// tag sets each template node's Src to its key path (root/row/title):
// children and slot content alike, as the prefab.* commands find them.
func tag(n *Node, parent string) {
	if n == nil {
		return
	}
	n.Src = n.Key
	if parent != "" {
		n.Src = parent + "/" + n.Key
	}
	for _, c := range n.Children {
		tag(c, n.Src)
	}
	for _, list := range n.Slots {
		for _, c := range list {
			tag(c, n.Src)
		}
	}
}
