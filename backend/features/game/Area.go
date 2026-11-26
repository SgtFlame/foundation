package game

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Areas (forge 375 S1): a story game's places, laid out from a piece set's
// pieces on a grid of 10 × 10 m cells. An area is a definition on the def
// stream (type gameArea, projector game.areas); placing, moving and
// removing a piece are ops with inverses, each checked by Fit, so the
// server refuses a piece that covers another or whose edges do not meet
// its neighbours'.

const (
	// AreaType is an area's target type on the def stream.
	AreaType = "gameArea"
	// AreasProjector folds each area, keyed gameArea.<id>.
	AreasProjector = "game.areas"
	// MaxAreaCells bounds an area's width and depth in cells.
	MaxAreaCells = 64
)

// Events of an area, beside created, changed, deleted and restored.
const (
	EventPlaced  = "placed"  // {placement}
	EventMoved   = "moved"   // {placement}
	EventRemoved = "removed" // {id}
)

// GameArea is an area definition as the projection keeps it.
type GameArea struct {
	ID        string               `json:"id"`
	Name      string               `json:"name"`
	PieceSet  string               `json:"pieceSet"`
	Width     int                  `json:"width"`
	Depth     int                  `json:"depth"`
	Pieces    map[string]Placement `json:"pieces"`
	Deleted   bool                 `json:"deleted,omitempty"`
	Version   uint64               `json:"version"`
	CreatedAt time.Time            `json:"createdAt"`
	UpdatedAt time.Time            `json:"updatedAt"`
}

const areaFields = `{id, name, pieceSet, width, depth (cells of 10 m), pieces: {<id>: {id, piece, x, z, rot}}, deleted, version, createdAt, updatedAt}`

type areaChange struct {
	Area      *GameArea  `json:"area,omitempty"`
	Name      string     `json:"name,omitempty"`
	Width     int        `json:"width,omitempty"`
	Depth     int        `json:"depth,omitempty"`
	Placement *Placement `json:"placement,omitempty"`
	ID        string     `json:"id,omitempty"`
}

func foldArea(f live.Fold) (json.RawMessage, error) {
	var a GameArea
	if f.State != nil {
		if err := json.Unmarshal(f.State, &a); err != nil {
			return nil, err
		}
	}
	var c areaChange
	if err := json.Unmarshal(f.Event.Data, &c); err != nil {
		return nil, err
	}
	if a.Pieces == nil {
		a.Pieces = map[string]Placement{}
	}
	switch f.Event.Type {
	case EventCreated:
		if c.Area == nil {
			return f.State, nil
		}
		a = *c.Area
		if a.Pieces == nil {
			a.Pieces = map[string]Placement{}
		}
		a.CreatedAt = f.Event.Time
	case EventChanged:
		if c.Name != "" {
			a.Name = c.Name
		}
		if c.Width > 0 {
			a.Width = c.Width
		}
		if c.Depth > 0 {
			a.Depth = c.Depth
		}
	case EventPlaced, EventMoved:
		if c.Placement == nil {
			return f.State, nil
		}
		a.Pieces[c.Placement.ID] = *c.Placement
	case EventRemoved:
		delete(a.Pieces, c.ID)
	case EventDeleted:
		a.Deleted = true
	case EventRestored:
		a.Deleted = false
	default:
		return f.State, nil
	}
	a.Version++
	a.UpdatedAt = f.Event.Time
	return json.Marshal(a)
}

// NewAreaID is a new area's id.
func NewAreaID() string { return "a" + strconv.FormatUint(features.NewTSID(), 10) }

// NewPieceID is a new placement's id, unique in its area.
func NewPieceID() string { return "p" + strconv.FormatUint(features.NewTSID(), 36) }

const placementSchema = `"piece":{"type":"string","description":"The piece's id in the area's piece set, e.g. farmhouse (game.pieceSet lists them)"},
  "x":{"type":"integer","minimum":0,"description":"The cell of the footprint's north-west corner after turning, from the west edge"},
  "z":{"type":"integer","minimum":0,"description":"That cell from the north edge"},
  "rot":{"type":"integer","minimum":0,"maximum":3,"description":"Turns of 90° clockwise seen from above (0 to 3)"}`

func contributeAreas(reg *extensions.Registry) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: AreasProjector, Source: source}, Title: "Game areas",
		Description: "Each game area definition, keyed gameArea.<id>: " + areaFields,
		Area:        live.AreaDefinitions, Types: []string{AreaType}, Fold: foldArea,
	})
	register := func(c live.Command, decide func(d live.Decision, a GameArea, exists bool) ([]live.NewEvent, []live.Problem)) {
		c.Source = source
		c.Area, c.TargetType, c.Projection = live.AreaDefinitions, AreaType, AreasProjector
		c.Action = "game.write"
		c.Resource = func(p features.Principal, _ live.Target) string { return Resource(p) }
		c.Decide = func(d live.Decision) ([]live.NewEvent, []live.Problem) {
			var a GameArea
			exists := d.State != nil
			if exists {
				if err := json.Unmarshal(d.State, &a); err != nil {
					return nil, problem("", "", "the area cannot be read: "+err.Error(), "")
				}
			}
			return decide(d, a, exists)
		}
		live.Commands.Register(reg, c)
	}
	missing := func(d live.Decision) []live.Problem {
		return problem("area_not_found", "target", fmt.Sprintf("there is no area %q", d.Target.ID), "list the areas with the game.areas query")
	}
	alive := func(d live.Decision, a GameArea, exists bool) (*PieceSet, []live.Problem) {
		if !exists || a.Deleted {
			return nil, missing(d)
		}
		set, ok := PieceSetOf(a.PieceSet)
		if !ok {
			return nil, problem("", "target", fmt.Sprintf("the area's piece set %q is not known", a.PieceSet), "")
		}
		return set, nil
	}
	misfit := func(msg string) []live.Problem {
		return problem("piece_misfit", "args", msg, "place it on free cells where its edges meet its neighbours' (game.describeArea shows the area), or turn it with rot")
	}
	sizeOK := func(path string, n int) []live.Problem {
		if n < 1 || n > MaxAreaCells {
			return problem("", path, fmt.Sprintf("an area is 1 to %d cells across", MaxAreaCells), "")
		}
		return nil
	}

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "game.createArea"}, Title: "Make an area",
		Description: "Makes a game area: a place laid out from a piece set's pieces on a grid of 10 × 10 m cells, width cells east to west and depth north to south. Empty at first; place pieces with game.placePiece.",
		Args: json.RawMessage(`{"type":"object","properties":{
  "name":{"type":"string","description":"The area's name, at most 80 characters, e.g. Sheaflands farmstead"},
  "pieceSet":{"type":"string","description":"The piece set it is laid out from (farmland by default; game.pieceSets lists them)"},
  "width":{"type":"integer","minimum":1,"maximum":64,"description":"Cells from west to east (16 by default)"},
  "depth":{"type":"integer","minimum":1,"maximum":64,"description":"Cells from north to south (16 by default)"}},"required":["name"]}`),
		Examples:  []json.RawMessage{json.RawMessage(`{"name":"Sheaflands farmstead","pieceSet":"farmland","width":12,"depth":10}`)},
		Events:    []string{EventCreated},
		NewTarget: NewAreaID,
	}, func(d live.Decision, _ GameArea, exists bool) ([]live.NewEvent, []live.Problem) {
		if exists {
			return nil, problem("area_exists", "target", fmt.Sprintf("the area %q exists", d.Target.ID), "change it with game.setArea")
		}
		var a struct {
			Name     string `json:"name"`
			PieceSet string `json:"pieceSet"`
			Width    int    `json:"width"`
			Depth    int    `json:"depth"`
		}
		if err := json.Unmarshal(d.Args, &a); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		name, ps := checkName(a.Name)
		if ps != nil {
			return nil, ps
		}
		if a.PieceSet == "" {
			a.PieceSet = "farmland"
		}
		if _, ok := PieceSetOf(a.PieceSet); !ok {
			return nil, problem("", "args.pieceSet", fmt.Sprintf("there is no piece set %q", a.PieceSet), "use one game.pieceSets lists")
		}
		if a.Width == 0 {
			a.Width = 16
		}
		if a.Depth == 0 {
			a.Depth = 16
		}
		if ps := sizeOK("args.width", a.Width); ps != nil {
			return nil, ps
		}
		if ps := sizeOK("args.depth", a.Depth); ps != nil {
			return nil, ps
		}
		d.Undo("", live.OpOf("game.deleteArea", d.Target.ID, map[string]any{}))
		return []live.NewEvent{{Type: EventCreated, Data: areaChange{Area: &GameArea{ID: d.Target.ID, Name: name, PieceSet: a.PieceSet, Width: a.Width, Depth: a.Depth, Pieces: map[string]Placement{}}}}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "game.setArea"}, Title: "Change an area",
		Description: "Renames an area or changes its size in cells; it cannot shrink past a placed piece.",
		Args: json.RawMessage(`{"type":"object","properties":{
  "name":{"type":"string","description":"The area's new name"},
  "width":{"type":"integer","minimum":1,"maximum":64,"description":"Cells from west to east"},
  "depth":{"type":"integer","minimum":1,"maximum":64,"description":"Cells from north to south"}}}`),
		Examples: []json.RawMessage{json.RawMessage(`{"name":"Farmstead at night"}`), json.RawMessage(`{"width":20,"depth":14}`)},
		Events:   []string{EventChanged},
	}, func(d live.Decision, a GameArea, exists bool) ([]live.NewEvent, []live.Problem) {
		set, ps := alive(d, a, exists)
		if ps != nil {
			return nil, ps
		}
		var in struct {
			Name  *string `json:"name"`
			Width int     `json:"width"`
			Depth int     `json:"depth"`
		}
		if err := json.Unmarshal(d.Args, &in); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		c := areaChange{}
		undo := map[string]any{}
		if in.Name != nil {
			name, ps := checkName(*in.Name)
			if ps != nil {
				return nil, ps
			}
			if name != a.Name {
				c.Name, undo["name"] = name, a.Name
			}
		}
		if in.Width != 0 && in.Width != a.Width {
			if ps := sizeOK("args.width", in.Width); ps != nil {
				return nil, ps
			}
			c.Width, undo["width"] = in.Width, a.Width
		}
		if in.Depth != 0 && in.Depth != a.Depth {
			if ps := sizeOK("args.depth", in.Depth); ps != nil {
				return nil, ps
			}
			c.Depth, undo["depth"] = in.Depth, a.Depth
		}
		if len(undo) == 0 {
			d.Undo("")
			return nil, nil
		}
		w, dp := a.Width, a.Depth
		if c.Width > 0 {
			w = c.Width
		}
		if c.Depth > 0 {
			dp = c.Depth
		}
		for _, pl := range a.Pieces {
			if p, ok := set.Piece(pl.Piece); ok {
				pw, pd, _ := Turned(p, pl.Rot)
				if pl.X+pw > w || pl.Z+pd > dp {
					return nil, problem("area_shrinks_past_piece", "args", fmt.Sprintf("the %s at cell %d, %d would be outside a %d × %d area", p.Title, pl.X, pl.Z, w, dp), "remove or move it first")
				}
			}
		}
		d.Undo("", live.OpOf("game.setArea", d.Target.ID, undo))
		return []live.NewEvent{{Type: EventChanged, Data: c}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "game.placePiece"}, Title: "Place a piece",
		Description: "Places a piece of the area's piece set at a cell, turned in steps of 90°. Refused when it leaves the area, covers another piece, or an edge does not meet its neighbour's (a road's end must meet a road). The piece's id in the area is made when not given and returned in the ack's events.",
		Args: json.RawMessage(`{"type":"object","properties":{
  "id":{"type":"string","description":"The placement's id in the area; made when left out"},
  ` + placementSchema + `},"required":["piece","x","z"]}`),
		Examples: []json.RawMessage{json.RawMessage(`{"piece":"farmhouse","x":2,"z":3,"rot":0}`), json.RawMessage(`{"piece":"road","x":3,"z":5}`)},
		Events:   []string{EventPlaced},
	}, func(d live.Decision, a GameArea, exists bool) ([]live.NewEvent, []live.Problem) {
		set, ps := alive(d, a, exists)
		if ps != nil {
			return nil, ps
		}
		var pl Placement
		if err := json.Unmarshal(d.Args, &pl); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		if pl.ID == "" {
			pl.ID = NewPieceID()
		} else if !live.ValidToken(pl.ID) {
			return nil, problem("", "args.id", "a placement's id is letters, digits, - and _", "")
		} else if _, taken := a.Pieces[pl.ID]; taken {
			return nil, problem("piece_exists", "args.id", fmt.Sprintf("the area already has a piece %q", pl.ID), "leave id out to make one, or move it with game.movePiece")
		}
		pl.Rot = ((pl.Rot % 4) + 4) % 4
		if msg := Fit(set, a.Width, a.Depth, a.Pieces, pl); msg != "" {
			return nil, misfit(msg)
		}
		d.Undo("", live.OpOf("game.removePiece", d.Target.ID, map[string]any{"id": pl.ID}))
		return []live.NewEvent{{Type: EventPlaced, Data: areaChange{Placement: &pl}}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "game.movePiece"}, Title: "Move a piece",
		Description: "Moves or turns a placed piece: fields left out keep their value. Refused as game.placePiece would be.",
		Args: json.RawMessage(`{"type":"object","properties":{
  "id":{"type":"string","description":"The placement's id in the area"},
  "x":{"type":"integer","minimum":0,"description":"The new cell from the west edge"},
  "z":{"type":"integer","minimum":0,"description":"The new cell from the north edge"},
  "rot":{"type":"integer","minimum":0,"maximum":3,"description":"The new turn (0 to 3)"}},"required":["id"]}`),
		Examples: []json.RawMessage{json.RawMessage(`{"id":"p1","rot":1}`)},
		Events:   []string{EventMoved},
	}, func(d live.Decision, a GameArea, exists bool) ([]live.NewEvent, []live.Problem) {
		set, ps := alive(d, a, exists)
		if ps != nil {
			return nil, ps
		}
		var in struct {
			ID  string `json:"id"`
			X   *int   `json:"x"`
			Z   *int   `json:"z"`
			Rot *int   `json:"rot"`
		}
		if err := json.Unmarshal(d.Args, &in); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		old, ok := a.Pieces[in.ID]
		if !ok {
			return nil, problem("piece_not_found", "args.id", fmt.Sprintf("the area has no piece %q", in.ID), "")
		}
		pl := old
		if in.X != nil {
			pl.X = *in.X
		}
		if in.Z != nil {
			pl.Z = *in.Z
		}
		if in.Rot != nil {
			pl.Rot = ((*in.Rot % 4) + 4) % 4
		}
		if pl == old {
			d.Undo("")
			return nil, nil
		}
		if msg := Fit(set, a.Width, a.Depth, a.Pieces, pl); msg != "" {
			return nil, misfit(msg)
		}
		d.Undo("", live.OpOf("game.movePiece", d.Target.ID, map[string]any{"id": old.ID, "x": old.X, "z": old.Z, "rot": old.Rot}))
		return []live.NewEvent{{Type: EventMoved, Data: areaChange{Placement: &pl}}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "game.removePiece"}, Title: "Remove a piece",
		Description: "Removes a placed piece from an area.",
		Args:        json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The placement's id in the area"}},"required":["id"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"id":"p1"}`)},
		Events:      []string{EventRemoved},
	}, func(d live.Decision, a GameArea, exists bool) ([]live.NewEvent, []live.Problem) {
		if _, ps := alive(d, a, exists); ps != nil {
			return nil, ps
		}
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(d.Args, &in); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		old, ok := a.Pieces[in.ID]
		if !ok {
			d.Undo("") // already gone
			return nil, nil
		}
		d.Undo("", live.OpOf("game.placePiece", d.Target.ID, map[string]any{"id": old.ID, "piece": old.Piece, "x": old.X, "z": old.Z, "rot": old.Rot}))
		return []live.NewEvent{{Type: EventRemoved, Data: areaChange{ID: old.ID}}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "game.deleteArea"}, Title: "Delete an area",
		Description: "Deletes an area; game.restoreArea brings it back with its pieces.",
		Args:        json.RawMessage(`{"type":"object","properties":{}}`), Examples: []json.RawMessage{json.RawMessage(`{}`)},
		Events: []string{EventDeleted},
	}, func(d live.Decision, a GameArea, exists bool) ([]live.NewEvent, []live.Problem) {
		if !exists {
			return nil, missing(d)
		}
		if a.Deleted {
			d.Undo("")
			return nil, nil
		}
		d.Undo("", live.OpOf("game.restoreArea", d.Target.ID, map[string]any{}))
		return []live.NewEvent{{Type: EventDeleted, Data: areaChange{}}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "game.restoreArea"}, Title: "Restore an area",
		Description: "Brings back a deleted area.",
		Args:        json.RawMessage(`{"type":"object","properties":{}}`), Examples: []json.RawMessage{json.RawMessage(`{}`)},
		Events: []string{EventRestored},
	}, func(d live.Decision, a GameArea, exists bool) ([]live.NewEvent, []live.Problem) {
		if !exists {
			return nil, missing(d)
		}
		if !a.Deleted {
			d.Undo("")
			return nil, nil
		}
		d.Undo("", live.OpOf("game.deleteArea", d.Target.ID, map[string]any{}))
		return []live.NewEvent{{Type: EventRestored, Data: areaChange{}}}, nil
	})

	areaQueries(reg)
}
