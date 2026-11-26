package game

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Doors and people (forge 375 S3): game.linkDoor sets the area a door
// piece leads to, so the player walks through the farmhouse's door into
// its kitchen, a separate area (open question 7); game.placeNpc,
// game.moveNpc and game.removeNpc stand people in an area, each with a
// dialogue to talk through. All are ops with inverses.

type areaDecide = func(d live.Decision, a GameArea, exists bool) ([]live.NewEvent, []live.Problem)

// NewNpcID is a new person's id, unique in its area.
func NewNpcID() string { return "n" + strconv.FormatUint(features.NewTSID(), 36) }

const npcSchema = `"name":{"type":"string","description":"What the player sees them called, at most 80 characters, e.g. The farmer"},
  "kind":{"type":"string","description":"Their look, e.g. farmer"},
  "x":{"type":"number","minimum":0,"description":"Metres from the area's west edge"},
  "z":{"type":"number","minimum":0,"description":"Metres from the area's north edge"},
  "facing":{"type":"number","description":"The way they face in radians: 0 north, turning clockwise"},
  "dialogue":{"type":"string","description":"What they say when talked to: a dialogue's id (game.dialogues lists them)"}`

func contributeAreaPeople(register func(live.Command, areaDecide), alive func(live.Decision, GameArea, bool) (*PieceSet, []live.Problem), missing func(live.Decision) []live.Problem) {
	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "game.linkDoor"}, Title: "Link a door",
		Description: "Sets the area a door piece leads to (the farmhouse's door to its kitchen, the kitchen's door back out), or with area empty, unlinks it. Walking through a linked door in play takes the player to that area, by its door that leads back.",
		Args: json.RawMessage(`{"type":"object","properties":{
  "id":{"type":"string","description":"The door piece's placement id in this area"},
  "area":{"type":"string","description":"The area it leads to; empty unlinks it"}},"required":["id","area"]}`),
		Examples: []json.RawMessage{json.RawMessage(`{"id":"p1","area":"a123"}`), json.RawMessage(`{"id":"p1","area":""}`)},
		Events:   []string{EventLinked},
	}, func(d live.Decision, a GameArea, exists bool) ([]live.NewEvent, []live.Problem) {
		set, ps := alive(d, a, exists)
		if ps != nil {
			return nil, ps
		}
		var in struct {
			ID   string `json:"id"`
			Area string `json:"area"`
		}
		if err := json.Unmarshal(d.Args, &in); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		pl, ok := a.Pieces[in.ID]
		if !ok {
			return nil, problem("piece_not_found", "args.id", fmt.Sprintf("the area has no piece %q", in.ID), "")
		}
		p, _ := set.Piece(pl.Piece)
		if !p.Door {
			return nil, problem("not_a_door", "args.id", fmt.Sprintf("the %s has no door", p.Title), "link a door piece: a farmhouse, a barn or a room's door")
		}
		if in.Area == pl.Link {
			d.Undo("")
			return nil, nil
		}
		if in.Area != "" {
			if in.Area == d.Target.ID || !live.ValidToken(in.Area) {
				return nil, problem("", "args.area", "a door leads to another area", "")
			}
			raw, err := d.Read(AreasProjector, live.Target{Area: live.AreaDefinitions, Type: AreaType, ID: in.Area})
			var to GameArea
			if err != nil || raw == nil || json.Unmarshal(raw, &to) != nil || to.Deleted {
				return nil, problem("area_not_found", "args.area", fmt.Sprintf("there is no area %q", in.Area), "list the areas with the game.areas query")
			}
		}
		d.Undo("", live.OpOf("game.linkDoor", d.Target.ID, map[string]any{"id": in.ID, "area": pl.Link}))
		return []live.NewEvent{{Type: EventLinked, Data: areaChange{ID: in.ID, Link: in.Area}}}, nil
	})

	checkNpc := func(set *PieceSet, a GameArea, n Npc) []live.Problem {
		if strings.TrimSpace(n.Name) == "" || len(n.Name) > 80 {
			return problem("", "args.name", "a person's name is 1 to 80 characters", "")
		}
		w, dp := float64(a.Width)*set.Cell, float64(a.Depth)*set.Cell
		if math.IsNaN(n.X) || math.IsNaN(n.Z) || n.X < 0 || n.Z < 0 || n.X > w || n.Z > dp {
			return problem("", "args", fmt.Sprintf("%s at %g, %g m is outside the %g × %g m area", n.Name, n.X, n.Z, w, dp), "stand them inside the area")
		}
		if n.Dialogue != "" {
			if _, ok := DialogueOf(n.Dialogue); !ok {
				return problem("", "args.dialogue", fmt.Sprintf("there is no dialogue %q", n.Dialogue), "use one game.dialogues lists")
			}
		}
		if n.Kind != "" && !live.ValidToken(n.Kind) {
			return problem("", "args.kind", "a kind is letters, digits, - and _", "")
		}
		return nil
	}
	asArgs := func(n Npc) map[string]any {
		return map[string]any{"id": n.ID, "name": n.Name, "kind": n.Kind, "x": n.X, "z": n.Z, "facing": n.Facing, "dialogue": n.Dialogue}
	}

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "game.placeNpc"}, Title: "Stand a person in an area",
		Description: "Stands a person in an area at a point in metres, facing a way, with what they say when the player talks to them. The person's id is made when not given and returned in the ack's events.",
		Args: json.RawMessage(`{"type":"object","properties":{
  "id":{"type":"string","description":"The person's id in the area; made when left out"},
  ` + npcSchema + `},"required":["name","x","z"]}`),
		Examples: []json.RawMessage{json.RawMessage(`{"name":"The farmer","kind":"farmer","x":6,"z":4,"facing":3.14,"dialogue":"farmer"}`)},
		Events:   []string{EventNpcPlaced},
	}, func(d live.Decision, a GameArea, exists bool) ([]live.NewEvent, []live.Problem) {
		set, ps := alive(d, a, exists)
		if ps != nil {
			return nil, ps
		}
		var n Npc
		if err := json.Unmarshal(d.Args, &n); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		if n.ID == "" {
			n.ID = NewNpcID()
		} else if !live.ValidToken(n.ID) {
			return nil, problem("", "args.id", "a person's id is letters, digits, - and _", "")
		} else if _, taken := a.Npcs[n.ID]; taken {
			return nil, problem("npc_exists", "args.id", fmt.Sprintf("the area already has a person %q", n.ID), "leave id out to make one, or move them with game.moveNpc")
		}
		n.Name = strings.TrimSpace(n.Name)
		if ps := checkNpc(set, a, n); ps != nil {
			return nil, ps
		}
		d.Undo("", live.OpOf("game.removeNpc", d.Target.ID, map[string]any{"id": n.ID}))
		return []live.NewEvent{{Type: EventNpcPlaced, Data: areaChange{Npc: &n}}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "game.moveNpc"}, Title: "Move or change a person",
		Description: "Moves a person in an area, turns them, renames them or changes what they say: fields left out keep their value.",
		Args: json.RawMessage(`{"type":"object","properties":{
  "id":{"type":"string","description":"The person's id in the area"},
  ` + npcSchema + `},"required":["id"]}`),
		Examples: []json.RawMessage{json.RawMessage(`{"id":"n1","x":8,"z":5}`)},
		Events:   []string{EventNpcMoved},
	}, func(d live.Decision, a GameArea, exists bool) ([]live.NewEvent, []live.Problem) {
		set, ps := alive(d, a, exists)
		if ps != nil {
			return nil, ps
		}
		var in struct {
			ID       string   `json:"id"`
			Name     *string  `json:"name"`
			Kind     *string  `json:"kind"`
			X        *float64 `json:"x"`
			Z        *float64 `json:"z"`
			Facing   *float64 `json:"facing"`
			Dialogue *string  `json:"dialogue"`
		}
		if err := json.Unmarshal(d.Args, &in); err != nil {
			return nil, problem("", "args", err.Error(), "")
		}
		old, ok := a.Npcs[in.ID]
		if !ok {
			return nil, problem("npc_not_found", "args.id", fmt.Sprintf("the area has no person %q", in.ID), "")
		}
		n := old
		if in.Name != nil {
			n.Name = strings.TrimSpace(*in.Name)
		}
		if in.Kind != nil {
			n.Kind = *in.Kind
		}
		if in.X != nil {
			n.X = *in.X
		}
		if in.Z != nil {
			n.Z = *in.Z
		}
		if in.Facing != nil {
			n.Facing = *in.Facing
		}
		if in.Dialogue != nil {
			n.Dialogue = *in.Dialogue
		}
		if n == old {
			d.Undo("")
			return nil, nil
		}
		if ps := checkNpc(set, a, n); ps != nil {
			return nil, ps
		}
		d.Undo("", live.OpOf("game.moveNpc", d.Target.ID, asArgs(old)))
		return []live.NewEvent{{Type: EventNpcMoved, Data: areaChange{Npc: &n}}}, nil
	})

	register(live.Command{
		Descriptor: extensions.Descriptor{ID: "game.removeNpc"}, Title: "Remove a person",
		Description: "Takes a person out of an area.",
		Args:        json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The person's id in the area"}},"required":["id"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"id":"n1"}`)},
		Events:      []string{EventNpcRemoved},
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
		old, ok := a.Npcs[in.ID]
		if !ok {
			d.Undo("")
			return nil, nil
		}
		d.Undo("", live.OpOf("game.placeNpc", d.Target.ID, asArgs(old)))
		return []live.NewEvent{{Type: EventNpcRemoved, Data: areaChange{ID: old.ID}}}, nil
	})
	_ = missing
}
