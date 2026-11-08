// Package drafts keeps Cognition graphs editable by many people at once
// (documentation/Cognition.md › Built in slice 14): a graph's draft is an
// aggregate on the def stream, changed only by bp.* commands and folded
// into the cognition.graph projection; publishing commits the draft as a
// version, which the cognition.published projection keeps and the engine
// runs. The cognition.graph and cognition.graphs queries are what the
// editors and the Explorer subscribe to.
package drafts

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/SgtFlame/foundation/backend/features/cognition"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// TargetType is a graph's target type on the def stream.
const TargetType = "graph"

// Projector IDs.
const (
	DraftProjector     = "cognition.graph"     // each graph's draft
	PublishedProjector = "cognition.published" // each graph's published version
	TrashProjector     = "cognition.trash"     // each deleted graph's draft and published version, until restored
)

// Draft is the cognition.graph projection's value: a graph as it is being
// edited.
type Draft struct {
	Graph     cognition.Graph `json:"graph"`
	Rev       int             `json:"rev"`       // events folded; bp.publish names the one its sender saw
	Published int             `json:"published"` // the last published version, 0 before the first
	// PublishedRev is the Rev the last version was published at, so the
	// editor can tell whether the draft has changed since.
	PublishedRev int       `json:"publishedRev"`
	UpdatedBy    uint64    `json:"updatedBy,string"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Events a draft's commands commit.
const (
	EventCreated         = "created" // {graph}: made, imported, duplicated, or started from an editable built-in
	EventNodeAdded       = "nodeAdded"
	EventNodesMoved      = "nodesMoved"
	EventNodeChanged     = "nodeChanged"
	EventConnected       = "connected"
	EventDisconnected    = "disconnected"
	EventRemoved         = "removed"
	EventPasted          = "pasted"
	EventVariableSet     = "variableSet"
	EventVariableRemoved = "variableRemoved"
	EventInfoSet         = "infoSet"
	EventPublished       = "published"
	EventDeleted         = "deleted"  // Trash: what bp.restore brings back
	EventRestored        = "restored" // Trash
)

// Trash is a deleted graph as its deleted event keeps it: its draft and
// its published version (nil when it had none), which bp.restore brings
// back (forge 06b).
type Trash struct {
	Draft     Draft            `json:"draft"`
	Published *cognition.Graph `json:"published,omitempty"`
}

// Move is one node's new position.
type Move struct {
	Node     string             `json:"node"`
	Position cognition.Position `json:"position"`
}

// NodeChange changes a node's config and title; nil leaves them.
type NodeChange struct {
	Node   string          `json:"node"`
	Config json.RawMessage `json:"config,omitempty"`
	Title  *string         `json:"title,omitempty"`
}

// Info changes a graph's name, description, owner and spec; nil leaves them.
type Info struct {
	Name        *string         `json:"name,omitempty"`
	Description *string         `json:"description,omitempty"`
	Owner       *string         `json:"owner,omitempty"`
	Spec        json.RawMessage `json:"spec,omitempty"`
}

// Removal removes nodes, with their edges, and edges.
type Removal struct {
	Nodes []string         `json:"nodes,omitempty"`
	Edges []cognition.Edge `json:"edges,omitempty"`
}

// Fragment is nodes and the edges between them, as copied and pasted.
type Fragment struct {
	Nodes []cognition.Node `json:"nodes"`
	Edges []cognition.Edge `json:"edges"`
}

// Publication is a published version.
type Publication struct {
	Version int             `json:"version"`
	Graph   cognition.Graph `json:"graph"`
}

func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	err := json.Unmarshal(raw, &v)
	return v, err
}

// Apply folds one event into a draft; the browser applies the same events
// to show a change before it is committed (draft.ts).
func Apply(d *Draft, e live.Event) error {
	g := &d.Graph
	switch e.Type {
	case EventCreated:
		v, err := decode[struct{ Graph cognition.Graph }](e.Data)
		if err != nil {
			return err
		}
		// A draft started from a built-in counts the built-in's version
		*d = Draft{Graph: v.Graph, Published: max(d.Published, v.Graph.Version)}
		d.Graph.Version = d.Published
	case EventNodeAdded:
		n, err := decode[struct{ Node cognition.Node }](e.Data)
		if err != nil {
			return err
		}
		g.Nodes = append(g.Nodes, n.Node)
	case EventNodesMoved:
		v, err := decode[struct{ Moves []Move }](e.Data)
		if err != nil {
			return err
		}
		for _, m := range v.Moves {
			if i := nodeIndex(*g, m.Node); i >= 0 {
				g.Nodes[i].Position = m.Position
			}
		}
	case EventNodeChanged:
		c, err := decode[NodeChange](e.Data)
		if err != nil {
			return err
		}
		if i := nodeIndex(*g, c.Node); i >= 0 {
			if c.Config != nil {
				g.Nodes[i].Config = c.Config
			}
			if c.Title != nil {
				g.Nodes[i].Title = *c.Title
			}
		}
	case EventConnected:
		v, err := decode[struct{ Edge cognition.Edge }](e.Data)
		if err != nil {
			return err
		}
		if !slices.Contains(g.Edges, v.Edge) {
			g.Edges = append(g.Edges, v.Edge)
		}
	case EventDisconnected:
		v, err := decode[struct{ Edge cognition.Edge }](e.Data)
		if err != nil {
			return err
		}
		g.Edges = slices.DeleteFunc(g.Edges, func(x cognition.Edge) bool { return x == v.Edge })
	case EventRemoved:
		r, err := decode[Removal](e.Data)
		if err != nil {
			return err
		}
		g.Nodes = slices.DeleteFunc(g.Nodes, func(n cognition.Node) bool { return slices.Contains(r.Nodes, n.ID) })
		g.Edges = slices.DeleteFunc(g.Edges, func(x cognition.Edge) bool {
			return slices.Contains(r.Edges, x) || slices.Contains(r.Nodes, x.From.Node) || slices.Contains(r.Nodes, x.To.Node)
		})
	case EventPasted:
		f, err := decode[Fragment](e.Data)
		if err != nil {
			return err
		}
		g.Nodes = append(g.Nodes, f.Nodes...)
		g.Edges = append(g.Edges, f.Edges...)
	case EventVariableSet:
		v, err := decode[struct{ Variable cognition.Variable }](e.Data)
		if err != nil {
			return err
		}
		if i := slices.IndexFunc(g.Variables, func(x cognition.Variable) bool { return x.ID == v.Variable.ID }); i >= 0 {
			g.Variables[i] = v.Variable
		} else {
			g.Variables = append(g.Variables, v.Variable)
		}
	case EventVariableRemoved:
		v, err := decode[struct{ ID string }](e.Data)
		if err != nil {
			return err
		}
		g.Variables = slices.DeleteFunc(g.Variables, func(x cognition.Variable) bool { return x.ID == v.ID })
	case EventInfoSet:
		in, err := decode[Info](e.Data)
		if err != nil {
			return err
		}
		if in.Name != nil {
			g.Name = *in.Name
		}
		if in.Description != nil {
			g.Description = *in.Description
		}
		if in.Owner != nil {
			g.Owner = *in.Owner
		}
		if in.Spec != nil {
			g.Spec = in.Spec
		}
	case EventPublished:
		p, err := decode[Publication](e.Data)
		if err != nil {
			return err
		}
		d.Published, d.PublishedRev, g.Version = p.Version, d.Rev+1, p.Version
	case EventRestored:
		t, err := decode[Trash](e.Data)
		if err != nil {
			return err
		}
		rev := max(d.Rev, t.Draft.Rev)
		*d = t.Draft
		d.Rev = rev
	default:
		return fmt.Errorf("unknown event %s", e.Type)
	}
	d.Rev++
	d.UpdatedBy, d.UpdatedAt = e.UserID, e.Time
	if g.Nodes == nil {
		g.Nodes = []cognition.Node{}
	}
	if g.Edges == nil {
		g.Edges = []cognition.Edge{}
	}
	return nil
}

func nodeIndex(g cognition.Graph, id string) int {
	return slices.IndexFunc(g.Nodes, func(n cognition.Node) bool { return n.ID == id })
}

// fold is the cognition.graph projector's fold.
func fold(f live.Fold) (json.RawMessage, error) {
	if f.Event.Type == EventDeleted {
		return nil, nil
	}
	var d Draft
	if f.State != nil {
		if err := json.Unmarshal(f.State, &d); err != nil {
			return nil, err
		}
	}
	d.Graph.ID = f.Event.Target.ID
	if err := Apply(&d, f.Event); err != nil {
		return nil, err
	}
	return json.Marshal(d)
}

// foldPublished is the cognition.published projector's fold: the last
// published version's graph.
func foldPublished(f live.Fold) (json.RawMessage, error) {
	switch f.Event.Type {
	case EventPublished:
		p, err := decode[Publication](f.Event.Data)
		if err != nil {
			return nil, err
		}
		p.Graph.ID, p.Graph.Version = f.Event.Target.ID, p.Version
		return json.Marshal(p.Graph)
	case EventDeleted:
		return nil, nil
	case EventRestored:
		t, err := decode[Trash](f.Event.Data)
		if err != nil || t.Published == nil {
			return nil, err
		}
		return json.Marshal(t.Published)
	}
	return f.State, nil
}

// foldTrash is the cognition.trash projector's fold: a deleted graph's
// draft and published version, until it is restored or made again.
func foldTrash(f live.Fold) (json.RawMessage, error) {
	switch f.Event.Type {
	case EventDeleted:
		var t Trash
		if err := json.Unmarshal(f.Event.Data, &t); err != nil || t.Draft.Graph.ID == "" {
			return nil, err // deleted before slice 39 kept it
		}
		return f.Event.Data, nil
	case EventRestored, EventCreated:
		return nil, nil
	}
	return f.State, nil
}
