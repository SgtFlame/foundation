package game_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/game"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Forge 375 S3: the kitchen interior, doors and people.

func TestKitchenFit(t *testing.T) {
	set, ok := game.PieceSetOf("kitchen")
	if !ok || !set.Interior || set.Cell != 2 {
		t.Fatalf("kitchen set %+v", set)
	}
	none := map[string]game.Placement{}
	// a hearth against the north wall fits; in the middle of the floor it does not
	if why := game.Fit(set, 6, 5, none, game.Placement{ID: "h", Piece: "hearth", X: 2, Z: 0}); why != "" {
		t.Fatalf("hearth on the wall: %s", why)
	}
	if why := game.Fit(set, 6, 5, none, game.Placement{ID: "h", Piece: "hearth", X: 2, Z: 2}); !strings.Contains(why, "edge") {
		t.Fatalf("hearth in the middle: %q", why)
	}
	// a table may stand anywhere
	if why := game.Fit(set, 6, 5, none, game.Placement{ID: "t", Piece: "table", X: 2, Z: 2}); why != "" {
		t.Fatalf("table: %s", why)
	}
	door, _ := set.Piece("door")
	if !door.Door || !door.Boundary {
		t.Fatalf("door %+v", door)
	}
}

func TestDialogues(t *testing.T) {
	d, ok := game.DialogueOf("farmer")
	if !ok || d.Start == "" || d.Nodes[d.Start].Text == "" {
		t.Fatalf("farmer dialogue %+v", d)
	}
	if err := d.Check(); err != "" {
		t.Fatal(err)
	}
	bad := d
	bad.Nodes = map[string]game.DialogueNode{"greet": {Text: "Hello", Choices: []game.DialogueChoice{{Text: "Go on", Next: "nowhere"}}}}
	bad.Start = "greet"
	if bad.Check() == "" {
		t.Fatal("a choice leading nowhere passed the check")
	}
}

func TestDoorsAndPeople(t *testing.T) {
	st := newStack(t)
	farm := st.ok("game.createArea", "", map[string]any{"name": "Farmstead", "width": 10, "depth": 8}).Target
	kitchen := st.ok("game.createArea", "", map[string]any{"name": "Kitchen", "pieceSet": "kitchen", "width": 6, "depth": 5}).Target
	read := func(id string, seq uint64) game.GameArea {
		var a game.GameArea
		if !st.read(game.AreasProjector, game.AreaType+"."+id, seq, &a) {
			t.Fatalf("no area %s", id)
		}
		return a
	}
	st.ok("game.placePiece", farm, map[string]any{"id": "house", "piece": "farmhouse", "x": 2, "z": 2})
	st.ok("game.placePiece", farm, map[string]any{"id": "tree", "piece": "tree", "x": 7, "z": 1})
	st.ok("game.placePiece", kitchen, map[string]any{"id": "door", "piece": "door", "x": 2, "z": 4})

	res := st.ok("game.linkDoor", farm, map[string]any{"id": "house", "area": kitchen})
	if a := read(farm, res.Seq); a.Pieces["house"].Link != kitchen {
		t.Fatalf("house link %+v", a.Pieces["house"])
	}
	st.ok("game.linkDoor", kitchen, map[string]any{"id": "door", "area": farm})
	if r := st.refused("game.linkDoor", farm, map[string]any{"id": "tree", "area": kitchen}); !strings.Contains(r.Common.Message+" "+fmtProblems(r.Problems), "door") {
		t.Fatalf("tree link: %s %+v", r.Common.Message, r.Problems)
	}
	st.refused("game.linkDoor", farm, map[string]any{"id": "house", "area": "a404"})
	st.refused("game.linkDoor", farm, map[string]any{"id": "house", "area": farm})

	res = st.ok("game.placeNpc", kitchen, map[string]any{"id": "farmer", "name": "The farmer", "kind": "farmer", "x": 5, "z": 3, "dialogue": "farmer"})
	if a := read(kitchen, res.Seq); a.Npcs["farmer"].Name != "The farmer" || a.Npcs["farmer"].X != 5 {
		t.Fatalf("farmer %+v", a.Npcs)
	}
	st.refused("game.placeNpc", kitchen, map[string]any{"name": "Out", "x": 50, "z": 3})
	st.refused("game.placeNpc", kitchen, map[string]any{"name": "Mute", "x": 1, "z": 1, "dialogue": "nobody"})
	st.refused("game.placeNpc", kitchen, map[string]any{"id": "farmer", "name": "Twice", "x": 1, "z": 1})
	made := st.ok("game.placeNpc", kitchen, map[string]any{"name": "The farmer's wife", "x": 2, "z": 2})
	res = st.ok("game.moveNpc", kitchen, map[string]any{"id": "farmer", "x": 4, "facing": 1.5})
	a := read(kitchen, res.Seq)
	if f := a.Npcs["farmer"]; f.X != 4 || f.Z != 3 || f.Facing != 1.5 || f.Dialogue != "farmer" {
		t.Fatalf("moved farmer %+v", f)
	}
	if len(a.Npcs) != 2 || made.Seq == 0 {
		t.Fatalf("people %+v", a.Npcs)
	}
	res = st.ok("game.removeNpc", kitchen, map[string]any{"id": "farmer"})
	if a := read(kitchen, res.Seq); len(a.Npcs) != 1 {
		t.Fatalf("after removing %+v", a.Npcs)
	}
	st.ok("game.removeNpc", kitchen, map[string]any{"id": "farmer"}) // already gone: nothing to do
}

func fmtProblems(ps []live.Problem) string { return fmt.Sprint(ps) }
