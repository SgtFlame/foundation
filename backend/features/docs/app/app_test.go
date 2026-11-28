package docsapp_test

import (
	"encoding/json"
	"slices"
	"testing"

	docsapp "github.com/SgtFlame/foundation/backend/features/docs/app"
)

// The manifest names only parts the app holds, and every graph is the app's.
func TestDocument(t *testing.T) {
	d, err := docsapp.Document()
	if err != nil {
		t.Fatal(err)
	}
	var graphs, types []string
	for _, g := range d.Parts.Graphs {
		graphs = append(graphs, g.ID)
		if g.Graph.Owner != "app:"+docsapp.AppID {
			t.Errorf("graph %s is owned by %q", g.ID, g.Graph.Owner)
		}
	}
	for _, e := range d.Parts.Entities {
		types = append(types, e.ID)
	}
	m := d.Manifest
	for _, e := range m.Entities {
		if !slices.Contains(types, e) {
			t.Errorf("entity %s has no file", e)
		}
	}
	need := []string{}
	for _, c := range m.Commands {
		need = append(need, c.Graph)
	}
	for _, tr := range m.Triggers {
		need = append(need, tr.Graph)
	}
	for _, tl := range m.Tools {
		need = append(need, tl.Graph)
	}
	for _, q := range m.Queries {
		need = append(need, q.Graph)
	}
	for _, g := range need {
		if !slices.Contains(graphs, g) {
			t.Errorf("graph %s has no file", g)
		}
	}
	// and the jobs its graphs enqueue
	for _, g := range d.Parts.Graphs {
		for _, n := range g.Graph.Nodes {
			var c struct{ Graph string }
			if n.Type == "jobs.enqueue" && json.Unmarshal(n.Config, &c) == nil {
				need = append(need, c.Graph)
			}
		}
	}
	for _, g := range graphs {
		if !slices.Contains(need, g) {
			t.Errorf("graph %s is not used", g)
		}
	}
}
