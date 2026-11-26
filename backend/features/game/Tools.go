package game

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// Description is what game.describeWorld returns: the definition and a
// summary of the terrain's edits.
type Description struct {
	World World `json:"world"`
	Edits struct {
		Total int `json:"total"`
		Digs  int `json:"digs"`
		Fills int `json:"fills"`
		// Cells is how many edit cells hold edits, Busiest the five with
		// the most, each {cell, edits, centre: [x, y, z]}
		Cells   int          `json:"cells"`
		Busiest []CellDigest `json:"busiest"`
		LastSeq uint64       `json:"lastSeq"`
	} `json:"edits"`
}

// CellDigest is one edit cell in a description.
type CellDigest struct {
	Cell   string     `json:"cell"`
	Edits  int        `json:"edits"`
	Centre [3]float64 `json:"centre"`
}

// Describe reads a world and summarises its edits.
func Describe(ctx context.Context, store live.Store, tenant uint64, id string) (*Description, error) {
	v, ok, err := store.Get(ctx, tenant, WorldsProjector, WorldType+"."+id)
	if err != nil {
		return nil, err
	}
	var d Description
	if !ok || json.Unmarshal(v.Value, &d.World) != nil || d.World.Deleted {
		return nil, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	entries, err := store.Watch(ctx, tenant, CellsProjector, id+".*")
	if err != nil {
		return nil, err
	}
	var cells []CellDigest
	for e := range entries {
		if e == nil {
			break
		}
		var c Cell
		if e.Deleted || json.Unmarshal(e.Value.Value, &c) != nil || len(c.Edits) == 0 {
			continue
		}
		var sx, sy, sz float64
		for _, ed := range c.Edits {
			d.Edits.Total++
			if ed.Kind == "fill" {
				d.Edits.Fills++
			} else {
				d.Edits.Digs++
			}
			d.Edits.LastSeq = max(d.Edits.LastSeq, ed.Seq)
			sx, sy, sz = sx+ed.X, sy+ed.Y, sz+ed.Z
		}
		n := float64(len(c.Edits))
		cells = append(cells, CellDigest{Cell: e.Key[len(id)+1:], Edits: len(c.Edits), Centre: [3]float64{round1(sx / n), round1(sy / n), round1(sz / n)}})
	}
	sort.Slice(cells, func(i, j int) bool {
		return cells[i].Edits > cells[j].Edits || cells[i].Edits == cells[j].Edits && cells[i].Cell < cells[j].Cell
	})
	d.Edits.Cells = len(cells)
	d.Edits.Busiest = cells[:min(5, len(cells))]
	return &d, nil
}

func round1(v float64) float64 { return float64(int64(v*10+0.5*sign(v))) / 10 }

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// contributeTools registers game.describeWorld, the model's read of a world
// (V1.7). Making and changing worlds and editing terrain are the commands'
// own tools; game.screenshot is the browser's, since only the viewport
// can draw the world.
func contributeTools(reg *extensions.Registry, deps func() Deps) {
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "game.describeWorld", Source: source}, Title: "Describe a game world", Effect: tools.EffectRead,
		Description: "Describes a game world: its definition (name and the moon generator's parameters) and a summary of its terrain edits (how many digs and fills, and the busiest 16 m edit cells with where their edits are centred, in metres from the moon's centre). Use game.screenshot to see it, game.setWorld to change it and game.editTerrain to dig or fill.",
		Input:       json.RawMessage(`{"type":"object","properties":{"world":{"type":"string","description":"The world's id"}},"required":["world"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			var a struct {
				World string `json:"world"`
			}
			if err := json.Unmarshal(call.Args, &a); err != nil || !live.ValidToken(a.World) {
				return tools.Result{}, fmt.Errorf("give the world's id")
			}
			d := deps()
			if d.Store == nil {
				return tools.Result{}, fmt.Errorf("games are not available")
			}
			res := Resource(call.Principal)
			if err := authz.Check(call.Context(), call.Principal, "game.read", res).Err("game.read", res); err != nil {
				return tools.Result{}, fmt.Errorf("you may not see games here")
			}
			desc, err := Describe(call.Context(), d.Store, call.Principal.TenantID, a.World)
			if err != nil {
				log.Error(call.Context(), err, "cannot describe world %s", a.World)
				return tools.Result{}, fmt.Errorf("the world cannot be read")
			}
			if desc == nil {
				return tools.Result{}, fmt.Errorf("there is no world %q", a.World)
			}
			g := desc.World.Generator
			summary := fmt.Sprintf("**%s**: a moon of radius %g m (seed %d, %g m voxels, %d craters); %d edits (%d digs, %d fills) in %d cells",
				desc.World.Name, g.Radius, g.Seed, g.VoxelSize, g.Craters, desc.Edits.Total, desc.Edits.Digs, desc.Edits.Fills, desc.Edits.Cells)
			raw, _ := json.Marshal(desc)
			var content map[string]any
			_ = json.Unmarshal(raw, &content)
			return tools.Result{Content: content, Summary: summary}, nil
		},
	})
}
