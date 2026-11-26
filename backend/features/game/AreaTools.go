package game

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

// AreaDescription is what game.describeArea returns.
type AreaDescription struct {
	Area   GameArea      `json:"area"`
	Map    []string      `json:"map"`    // a row of characters per cell row, north first: . is empty
	Legend []LegendEntry `json:"legend"` // which character is which placement
	Pieces []PieceDigest `json:"pieces"` // the piece set's pieces, to place
}

// LegendEntry names a map character's placement.
type LegendEntry struct {
	Char  string `json:"char"`
	ID    string `json:"id"`
	Piece string `json:"piece"`
	X     int    `json:"x"`
	Z     int    `json:"z"`
	Rot   int    `json:"rot"`
}

// PieceDigest is a piece of the set in brief: its footprint and edges unturned.
type PieceDigest struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Size  [2]int `json:"size"`
	Edges Edges  `json:"edges"`
}

const mapChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// DescribeArea reads an area and draws its map.
func DescribeArea(ctx context.Context, store live.Store, tenant uint64, id string) (*AreaDescription, error) {
	v, ok, err := store.Get(ctx, tenant, AreasProjector, AreaType+"."+id)
	if err != nil {
		return nil, err
	}
	var d AreaDescription
	if !ok || json.Unmarshal(v.Value, &d.Area) != nil || d.Area.Deleted {
		return nil, nil
	}
	set, ok := PieceSetOf(d.Area.PieceSet)
	if !ok {
		return nil, fmt.Errorf("unknown piece set %q", d.Area.PieceSet)
	}
	rows := make([][]byte, d.Area.Depth)
	for z := range rows {
		rows[z] = []byte(strings.Repeat(".", d.Area.Width))
	}
	ids := make([]string, 0, len(d.Area.Pieces))
	for pid := range d.Area.Pieces {
		ids = append(ids, pid)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := d.Area.Pieces[ids[i]], d.Area.Pieces[ids[j]]
		return a.Z < b.Z || a.Z == b.Z && (a.X < b.X || a.X == b.X && a.ID < b.ID)
	})
	for i, pid := range ids {
		pl := d.Area.Pieces[pid]
		ch := byte('#')
		if i < len(mapChars) {
			ch = mapChars[i]
		}
		d.Legend = append(d.Legend, LegendEntry{Char: string(ch), ID: pl.ID, Piece: pl.Piece, X: pl.X, Z: pl.Z, Rot: pl.Rot})
		p, ok := set.Piece(pl.Piece)
		if !ok {
			continue
		}
		w, dp, _ := Turned(p, pl.Rot)
		for z := pl.Z; z < pl.Z+dp && z < d.Area.Depth; z++ {
			for x := pl.X; x < pl.X+w && x < d.Area.Width; x++ {
				rows[z][x] = ch
			}
		}
	}
	for _, r := range rows {
		d.Map = append(d.Map, string(r))
	}
	for _, p := range set.Pieces {
		d.Pieces = append(d.Pieces, PieceDigest{ID: p.ID, Title: p.Title, Size: p.Size, Edges: p.Edges})
	}
	return &d, nil
}

func contributeAreaTools(reg *extensions.Registry, deps func() Deps) {
	tools.Tools.Register(reg, tools.Tool{
		Descriptor: extensions.Descriptor{ID: "game.describeArea", Source: source}, Title: "Describe a game area", Effect: tools.EffectRead,
		Description: "Describes a game area: its definition, a map with a character per 10 × 10 m cell (north row first, west first; . is empty) and a legend naming each character's placement, and its piece set's pieces with their footprints [width, depth] and edge types per side unturned (n and s west to east, e and w north to south; a turn of 90° clockwise takes n to e). Two pieces may touch only where their edge types are the same, so a road's end must meet a road. Lay an area out with game.placePiece, game.movePiece and game.removePiece.",
		Input:       json.RawMessage(`{"type":"object","properties":{"area":{"type":"string","description":"The area's id"}},"required":["area"]}`),
		Run: func(call tools.Call) (tools.Result, error) {
			var a struct {
				Area string `json:"area"`
			}
			if err := json.Unmarshal(call.Args, &a); err != nil || !live.ValidToken(a.Area) {
				return tools.Result{}, fmt.Errorf("give the area's id")
			}
			d := deps()
			if d.Store == nil {
				return tools.Result{}, fmt.Errorf("games are not available")
			}
			res := Resource(call.Principal)
			if err := authz.Check(call.Context(), call.Principal, "game.read", res).Err("game.read", res); err != nil {
				return tools.Result{}, fmt.Errorf("you may not see games here")
			}
			desc, err := DescribeArea(call.Context(), d.Store, call.Principal.TenantID, a.Area)
			if err != nil {
				log.Error(call.Context(), err, "cannot describe area %s", a.Area)
				return tools.Result{}, fmt.Errorf("the area cannot be read")
			}
			if desc == nil {
				return tools.Result{}, fmt.Errorf("there is no area %q", a.Area)
			}
			summary := fmt.Sprintf("**%s**: %d × %d cells of the %s set, %d pieces\n\n```\n%s\n```", desc.Area.Name, desc.Area.Width, desc.Area.Depth, desc.Area.PieceSet, len(desc.Area.Pieces), strings.Join(desc.Map, "\n"))
			raw, _ := json.Marshal(desc)
			var content map[string]any
			_ = json.Unmarshal(raw, &content)
			return tools.Result{Content: content, Summary: summary}, nil
		},
	})
}
