package drafts

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// undo sets a bp.* command's inverse (forge 06a): the ops that take the
// draft from after its events back to the state it was decided against.
// A graph's inverse is read from the difference between the two, item by
// item (nodes, edges, variables, info), so undoing it leaves the changes
// others made to other items since.
func (c *decision) undo(command string, events []live.NewEvent) {
	if c.Op == nil {
		return
	}
	// The events apply to a copy, since they change nodes in place
	var after Draft
	raw, _ := json.Marshal(c.draft)
	_ = json.Unmarshal(raw, &after)
	for _, e := range events {
		data, _ := json.Marshal(e.Data)
		if err := Apply(&after, live.Event{Type: e.Type, Target: c.Target, Data: data}); err != nil {
			c.NoUndo(appOf(c.draft.Graph.Owner), "its inverse cannot be read: "+err.Error())
			return
		}
	}
	app := appOf(after.Graph.Owner)
	if app == "" {
		app = appOf(c.draft.Graph.Owner)
	}
	id := c.Target.ID
	switch {
	case command == "bp.publish":
		c.NoUndo(app, "a published version stays; publish the draft again to replace it")
		return
	case command == "bp.delete":
		c.NoUndo(app, "a deleted graph cannot be brought back yet")
		return
	case !c.exists || len(c.prefix) > 0:
		// A new graph, or the tenant's first change to an editable built-in:
		// deleting it returns to nothing, or to the built-in
		c.Undo(app, live.OpOf("bp.delete", id, map[string]any{}))
		return
	case command == "bp.import":
		c.Undo(app, live.OpOf("bp.import", id, map[string]any{"graph": c.draft.Graph}))
		return
	}
	c.Undo(app, graphInverse(id, c.draft.Graph, after.Graph)...)
}

// graphInverse is the ops that take a graph from after back to before.
func graphInverse(id string, before, after cognition.Graph) []live.Op {
	var ops []live.Op
	has := func(g cognition.Graph, nodeID string) bool { return nodeIndex(g, nodeID) >= 0 }
	// What was added goes first, so what comes back finds its ids free
	var added Removal
	for _, n := range after.Nodes {
		if !has(before, n.ID) {
			added.Nodes = append(added.Nodes, n.ID)
		}
	}
	for _, e := range after.Edges {
		if !slices.Contains(before.Edges, e) && !slices.Contains(added.Nodes, e.From.Node) && !slices.Contains(added.Nodes, e.To.Node) {
			added.Edges = append(added.Edges, e)
		}
	}
	if len(added.Nodes)+len(added.Edges) > 0 {
		ops = append(ops, live.OpOf("bp.remove", id, added))
	}
	removed := Fragment{Nodes: []cognition.Node{}, Edges: []cognition.Edge{}}
	for _, n := range before.Nodes {
		if !has(after, n.ID) {
			removed.Nodes = append(removed.Nodes, n)
		}
	}
	for _, e := range before.Edges {
		if !slices.Contains(after.Edges, e) {
			removed.Edges = append(removed.Edges, e)
		}
	}
	if len(removed.Nodes)+len(removed.Edges) > 0 {
		ops = append(ops, live.OpOf("bp.paste", id, removed))
	}
	var moves []Move
	for _, b := range before.Nodes {
		i := nodeIndex(after, b.ID)
		if i < 0 {
			continue
		}
		a := after.Nodes[i]
		if b.Title != a.Title || string(b.Config) != string(a.Config) {
			change := NodeChange{Node: b.ID, Config: b.Config, Title: &b.Title}
			if change.Config == nil {
				change.Config = json.RawMessage(`{}`)
			}
			ops = append(ops, live.OpOf("bp.setConfig", id, change))
		}
		if b.Position != a.Position {
			moves = append(moves, Move{Node: b.ID, Position: b.Position})
		}
	}
	if len(moves) > 0 {
		ops = append(ops, live.OpOf("bp.moveNodes", id, map[string]any{"moves": moves}))
	}
	for _, v := range after.Variables {
		if _, ok := before.VariableByID(v.ID); !ok {
			ops = append(ops, live.OpOf("bp.removeVariable", id, map[string]any{"id": v.ID}))
		}
	}
	for _, v := range before.Variables {
		if w, ok := after.VariableByID(v.ID); !ok || !same(v, w) {
			ops = append(ops, live.OpOf("bp.addVariable", id, v))
		}
	}
	var in Info
	changed := false
	if before.Name != after.Name {
		in.Name, changed = &before.Name, true
	}
	if before.Description != after.Description {
		in.Description, changed = &before.Description, true
	}
	if before.Owner != after.Owner {
		in.Owner, changed = &before.Owner, true
	}
	if string(before.Spec) != string(after.Spec) {
		in.Spec, changed = before.Spec, true
		if in.Spec == nil {
			in.Spec = json.RawMessage(`{}`)
		}
	}
	if changed {
		ops = append(ops, live.OpOf("bp.setInfo", id, in))
	}
	return ops
}

// appOf is the app an owner names: app:<appId>.
func appOf(owner string) string {
	app, _ := strings.CutPrefix(owner, "app:")
	if app == owner {
		return ""
	}
	return app
}

func same(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
