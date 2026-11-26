package game

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// EventEdited is a terrain edit decided by game.editTerrain:
// {kind, x, y, z, radius, material?}.
const EventEdited = "edited"

// Edit is one terrain edit as the projections keep it. Seq, its event's
// stream sequence, is the order every client applies edits in.
type Edit struct {
	Seq      uint64  `json:"seq"`
	Kind     string  `json:"kind"` // dig or fill
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Z        float64 `json:"z"`
	Radius   float64 `json:"radius"`
	Material *int    `json:"material,omitempty"`
	By       uint64  `json:"by,string,omitempty"` // the user who made it
	Command  string  `json:"cmd,omitempty"`       // the command's id, so its sender knows its own edit
}

// Cell is one edit cell's edits, in order.
type Cell struct {
	Edits []Edit `json:"edits"`
}

// Terrain is a world's terrain summary, the edit command's state.
type Terrain struct {
	Edits     int       `json:"edits"`
	LastSeq   uint64    `json:"lastSeq"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// CellOf is the key part of the edit cell holding a point.
func CellOf(x, y, z float64) string {
	c := func(v float64) int { return int(math.Floor(v / EditCell)) }
	return fmt.Sprintf("%d_%d_%d", c(x), c(y), c(z))
}

// CellKey is an edit cell's key in the game.terrainCells projection.
func CellKey(world string, x, y, z float64) string { return world + "." + CellOf(x, y, z) }

func editOf(e live.Event) (Edit, error) {
	var ed Edit
	if err := json.Unmarshal(e.Data, &ed); err != nil {
		return ed, err
	}
	ed.Seq, ed.By, ed.Command = e.Seq, e.UserID, e.CommandID
	return ed, nil
}

func contributeTerrain(reg *extensions.Registry) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: TerrainProjector, Source: source}, Title: "Terrain summaries",
		Description: "Each world's terrain edits in brief, keyed terrain.<worldId>: {edits, lastSeq, updatedAt}",
		Area:        Area, Types: []string{TerrainType},
		Fold: func(f live.Fold) (json.RawMessage, error) {
			var t Terrain
			if f.State != nil {
				if err := json.Unmarshal(f.State, &t); err != nil {
					return nil, err
				}
			}
			if f.Event.Type != EventEdited {
				return f.State, nil
			}
			t.Edits++
			t.LastSeq = f.Event.Seq
			t.UpdatedAt = f.Event.Time
			return json.Marshal(t)
		},
	})
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: CellsProjector, Source: source}, Title: "Terrain edits per cell",
		Description: fmt.Sprintf("Each world's terrain edits, kept in the %g m edit cell holding each edit's centre, keyed <worldId>.<cx>_<cy>_<cz>: {edits: [{seq, kind, x, y, z, radius, material, by, cmd}]}", EditCell),
		Area:        Area, Types: []string{TerrainType},
		KeyOf: func(e live.Event) string {
			if e.Type != EventEdited {
				return ""
			}
			ed, err := editOf(e)
			if err != nil {
				return ""
			}
			return CellKey(e.Target.ID, ed.X, ed.Y, ed.Z)
		},
		Fold: func(f live.Fold) (json.RawMessage, error) {
			var c Cell
			if f.State != nil {
				if err := json.Unmarshal(f.State, &c); err != nil {
					return nil, err
				}
			}
			ed, err := editOf(f.Event)
			if err != nil {
				return nil, err
			}
			c.Edits = append(c.Edits, ed)
			return json.Marshal(c)
		},
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "game.terrainEdits", Source: source}, Title: "A world's terrain edits",
		Description: "A world's terrain edits by edit cell, as a map from <worldId>.<cx>_<cy>_<cz> to {edits: [{seq, kind, x, y, z, radius, material, by, cmd}]}, then each cell's new list as edits arrive. Apply every edit in seq order over the world's generator.",
		Params:      json.RawMessage(`{"type":"object","properties":{"world":{"type":"string","description":"The world's id"}},"required":["world"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"world":"w123"}`)},
		Action:      "game.read",
		Resource:    func(p features.Principal, _ json.RawMessage) string { return Resource(p) },
		Run: live.ListOf(CellsProjector, func(f live.Feed) (string, error) {
			var p struct {
				World string `json:"world"`
			}
			if err := json.Unmarshal(f.Params(), &p); err != nil || !live.ValidToken(p.World) {
				return "", fmt.Errorf("game.terrainEdits needs a world's id")
			}
			return p.World + ".*", nil
		}),
	})
}
