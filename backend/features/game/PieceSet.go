package game

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
)

// Piece sets (forge 375 S1): the pieces an area is laid out from. Each
// piece covers a footprint of whole cells (10 × 10 m), turns in steps of
// 90°, and has an edge type on each cell-length segment of each side.
// Two pieces may touch only where their segments meet by the set's rules
// (the same type always meets itself). The browser checks a placement by
// the same rules (frontend/engine/src/area/Fit.ts) before sending it; the
// server's check here decides.
//
// Sides are n (z = 0 side), e (x = max), s (z = max) and w (x = 0); n and s
// segments run west to east, e and w north to south. x runs east and z
// south, so a turn of 90° clockwise seen from above takes n to e.

//go:embed piecesets/*.json
var pieceSetFiles embed.FS

// Edges are a piece's edge types per side.
type Edges struct {
	N []string `json:"n"`
	E []string `json:"e"`
	S []string `json:"s"`
	W []string `json:"w"`
}

// Piece is one piece of a set.
type Piece struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Size        [2]int `json:"size"` // [width (x), depth (z)] in cells, unturned
	Model       string `json:"model"`
	Edges       Edges  `json:"edges"`
	// Boundary pieces (doors and windows of an interior) stand on the area's edge
	Boundary bool `json:"boundary,omitempty"`
	// Door pieces lead to another area once game.linkDoor links them
	Door bool `json:"door,omitempty"`
}

// PieceSet is a set of pieces that fit together.
type PieceSet struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Cell        float64           `json:"cell"`
	EdgeTypes   map[string]string `json:"edgeTypes"`
	Meets       [][2]string       `json:"meets"` // pairs of different types that may touch
	Pieces      []Piece           `json:"pieces"`
	// Interior sets (S3) are rooms: walls round the area's edge, a floor
	Interior bool `json:"interior,omitempty"`
}

// Piece is the set's piece of that id.
func (s *PieceSet) Piece(id string) (Piece, bool) {
	for _, p := range s.Pieces {
		if p.ID == id {
			return p, true
		}
	}
	return Piece{}, false
}

// MeetOK reports whether two edge types may touch.
func (s *PieceSet) MeetOK(a, b string) bool {
	if a == b {
		return true
	}
	for _, m := range s.Meets {
		if (m[0] == a && m[1] == b) || (m[0] == b && m[1] == a) {
			return true
		}
	}
	return false
}

var pieceSets = loadPieceSets()

func loadPieceSets() map[string]*PieceSet {
	out := map[string]*PieceSet{}
	entries, err := pieceSetFiles.ReadDir("piecesets")
	if err != nil {
		panic(err)
	}
	for _, e := range entries {
		raw, err := pieceSetFiles.ReadFile("piecesets/" + e.Name())
		if err != nil {
			panic(err)
		}
		var s PieceSet
		if err := json.Unmarshal(raw, &s); err != nil {
			panic(fmt.Sprintf("piece set %s: %v", e.Name(), err))
		}
		if err := s.check(); err != nil {
			panic(fmt.Sprintf("piece set %s: %v", e.Name(), err))
		}
		out[s.ID] = &s
	}
	return out
}

func (s *PieceSet) check() error {
	for _, p := range s.Pieces {
		w, d := p.Size[0], p.Size[1]
		if w < 1 || d < 1 {
			return fmt.Errorf("piece %s: a footprint is at least 1 × 1", p.ID)
		}
		if len(p.Edges.N) != w || len(p.Edges.S) != w || len(p.Edges.E) != d || len(p.Edges.W) != d {
			return fmt.Errorf("piece %s: a side has one edge type per cell", p.ID)
		}
		for _, side := range [][]string{p.Edges.N, p.Edges.E, p.Edges.S, p.Edges.W} {
			for _, t := range side {
				if _, ok := s.EdgeTypes[t]; !ok {
					return fmt.Errorf("piece %s: edge type %q is not the set's", p.ID, t)
				}
			}
		}
	}
	return nil
}

// PieceSetOf is the built-in piece set of that id.
func PieceSetOf(id string) (*PieceSet, bool) {
	s, ok := pieceSets[id]
	return s, ok
}

// PieceSetIDs are the built-in piece sets' ids, sorted.
func PieceSetIDs() []string {
	var ids []string
	for id := range pieceSets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Placement is a piece placed in an area: its cell (the footprint's
// north-west corner after turning) and its turn (0 to 3, steps of 90°
// clockwise).
type Placement struct {
	ID    string `json:"id"`
	Piece string `json:"piece"`
	X     int    `json:"x"`
	Z     int    `json:"z"`
	Rot   int    `json:"rot"`
	// Link is the area a door piece leads to (game.linkDoor, S3)
	Link string `json:"link,omitempty"`
}

func reverse(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[len(s)-1-i] = v
	}
	return out
}

// Turned is a piece's footprint and edges after rot clockwise turns.
func Turned(p Piece, rot int) (w, d int, e Edges) {
	w, d, e = p.Size[0], p.Size[1], p.Edges
	for i := 0; i < ((rot%4)+4)%4; i++ {
		// Clockwise: west becomes north, north east, east south, south west
		e = Edges{N: reverse(e.W), E: e.N, S: reverse(e.E), W: e.S}
		w, d = d, w
	}
	return w, d, e
}

// side is one cell's side in an area: the cell and n, e, s or w.
type side struct {
	x, z int
	dir  byte
}

// boundary is a placement's edge segments by the cell and side they lie on.
func boundary(p Piece, pl Placement) map[side]string {
	w, d, e := Turned(p, pl.Rot)
	out := map[side]string{}
	for i := 0; i < w; i++ {
		out[side{pl.X + i, pl.Z, 'n'}] = e.N[i]
		out[side{pl.X + i, pl.Z + d - 1, 's'}] = e.S[i]
	}
	for j := 0; j < d; j++ {
		out[side{pl.X + w - 1, pl.Z + j, 'e'}] = e.E[j]
		out[side{pl.X, pl.Z + j, 'w'}] = e.W[j]
	}
	return out
}

var (
	opposite = map[byte]byte{'n': 's', 's': 'n', 'e': 'w', 'w': 'e'}
	step     = map[byte][2]int{'n': {0, -1}, 's': {0, 1}, 'e': {1, 0}, 'w': {-1, 0}}
	sideName = map[byte]string{'n': "north", 'e': "east", 's': "south", 'w': "west"}
)

// Fit checks a placement in an area of width × depth cells holding
// others (by id; the placement's own id is skipped, so a move is checked
// against everything else): inside the area, over no other piece, and
// every edge meeting its neighbour's by the set's rules. It gives the
// first problem's message, or "".
func Fit(set *PieceSet, width, depth int, others map[string]Placement, pl Placement) string {
	p, ok := set.Piece(pl.Piece)
	if !ok {
		return fmt.Sprintf("there is no piece %q in the %s set", pl.Piece, set.Title)
	}
	w, d, _ := Turned(p, pl.Rot)
	if pl.X < 0 || pl.Z < 0 || pl.X+w > width || pl.Z+d > depth {
		return fmt.Sprintf("the %s (%d × %d cells) at cell %d, %d does not fit inside the %d × %d area", p.Title, w, d, pl.X, pl.Z, width, depth)
	}
	if p.Boundary && pl.X != 0 && pl.Z != 0 && pl.X+w != width && pl.Z+d != depth {
		return fmt.Sprintf("the %s at cell %d, %d must stand on the area's edge", p.Title, pl.X, pl.Z)
	}
	// Who covers each cell, and each neighbour's edges
	cover := map[[2]int]string{}
	edges := map[side]string{}
	ids := make([]string, 0, len(others))
	for id := range others {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		o := others[id]
		if id == pl.ID {
			continue
		}
		op, ok := set.Piece(o.Piece)
		if !ok {
			continue
		}
		ow, od, _ := Turned(op, o.Rot)
		for i := 0; i < ow; i++ {
			for j := 0; j < od; j++ {
				cover[[2]int{o.X + i, o.Z + j}] = id
			}
		}
		for s, t := range boundary(op, o) {
			edges[s] = t
		}
	}
	for j := 0; j < d; j++ {
		for i := 0; i < w; i++ {
			if id, ok := cover[[2]int{pl.X + i, pl.Z + j}]; ok {
				o := others[id]
				op, _ := set.Piece(o.Piece)
				return fmt.Sprintf("the %s would cover the %s at cell %d, %d", p.Title, op.Title, pl.X+i, pl.Z+j)
			}
		}
	}
	mine := boundary(p, pl)
	keys := make([]side, 0, len(mine))
	for s := range mine {
		keys = append(keys, s)
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a].z != keys[b].z {
			return keys[a].z < keys[b].z
		}
		if keys[a].x != keys[b].x {
			return keys[a].x < keys[b].x
		}
		return keys[a].dir < keys[b].dir
	})
	for _, s := range keys {
		t := mine[s]
		st := step[s.dir]
		n := side{s.x + st[0], s.z + st[1], opposite[s.dir]}
		nt, ok := edges[n]
		if !ok {
			continue // the area's edge or an empty cell
		}
		if !set.MeetOK(t, nt) {
			o := others[cover[[2]int{n.x, n.z}]]
			op, _ := set.Piece(o.Piece)
			return fmt.Sprintf("the %s's %s edge (%s) at cell %d, %d does not meet the %s's %s edge (%s)", p.Title, sideName[s.dir], t, s.x, s.z, op.Title, sideName[n.dir], nt)
		}
	}
	return ""
}
