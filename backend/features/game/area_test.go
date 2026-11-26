package game_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/game"
	"github.com/SgtFlame/foundation/backend/features/tools"
)

func TestTurned(t *testing.T) {
	set, _ := game.PieceSetOf("farmland")
	barn, _ := set.Piece("barn") // 2 × 3, road at the south-west
	w, d, e := game.Turned(barn, 1)
	if w != 3 || d != 2 {
		t.Fatalf("turned once: %d × %d", w, d)
	}
	// South becomes west, read north to south: the road (south-west) is now at the north-west
	if e.W[0] != "road" || e.W[1] != "grass" || e.S[0] != "grass" {
		t.Fatalf("turned edges %+v", e)
	}
	_, _, e4 := game.Turned(barn, 4)
	if strings.Join(e4.S, ",") != strings.Join(barn.Edges.S, ",") {
		t.Fatalf("four turns %+v", e4)
	}
}

func TestFit(t *testing.T) {
	set, _ := game.PieceSetOf("farmland")
	others := map[string]game.Placement{
		"house": {ID: "house", Piece: "farmhouse", X: 2, Z: 2}, // path at its south-east cell (3, 3)
	}
	for _, c := range []struct {
		pl   game.Placement
		want string
	}{
		{game.Placement{ID: "r", Piece: "road", X: 3, Z: 4}, ""},                          // meets the path
		{game.Placement{ID: "r", Piece: "road", X: 2, Z: 4}, "does not meet"},             // its end meets the house's grass
		{game.Placement{ID: "r", Piece: "grass", X: 3, Z: 4}, "does not meet"},            // grass meets the path
		{game.Placement{ID: "r", Piece: "tree", X: 3, Z: 3}, "would cover the Farmhouse"}, // on the house
		{game.Placement{ID: "r", Piece: "barn", X: 9, Z: 0}, "does not fit inside"},       // past the east edge
		{game.Placement{ID: "r", Piece: "roadEnd", X: 3, Z: 4, Rot: 0}, ""},               // open to the north
		{game.Placement{ID: "r", Piece: "roadEnd", X: 3, Z: 4, Rot: 2}, "does not meet"},  // open to the south
		{game.Placement{ID: "house", Piece: "farmhouse", X: 2, Z: 2, Rot: 1}, ""},         // a move skips itself
		{game.Placement{ID: "r", Piece: "castle", X: 0, Z: 0}, "there is no piece"},
	} {
		got := game.Fit(set, 10, 10, others, c.pl)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%+v: %q, want %q", c.pl, got, c.want)
		}
	}
}

func TestAreaOps(t *testing.T) {
	st := newStack(t)
	res := st.ok("game.createArea", "", map[string]any{"name": "Sheaflands farmstead", "width": 10, "depth": 8})
	id := res.Target
	read := func(seq uint64) game.GameArea {
		var a game.GameArea
		if !st.read(game.AreasProjector, game.AreaType+"."+id, seq, &a) {
			t.Fatal("no area")
		}
		return a
	}
	if a := read(res.Seq); a.PieceSet != "farmland" || a.Width != 10 || len(a.Pieces) != 0 {
		t.Fatalf("area %+v", a)
	}
	st.ok("game.placePiece", id, map[string]any{"id": "house", "piece": "farmhouse", "x": 2, "z": 2})
	res = st.ok("game.placePiece", id, map[string]any{"piece": "road", "x": 3, "z": 4})
	a := read(res.Seq)
	if len(a.Pieces) != 2 {
		t.Fatalf("pieces %+v", a.Pieces)
	}
	r := st.refused("game.placePiece", id, map[string]any{"piece": "road", "x": 2, "z": 4})
	if len(r.Problems) == 0 || r.Problems[0].Code != "piece_misfit" {
		t.Fatalf("refusal %+v", r.Problems)
	}
	st.refused("game.placePiece", id, map[string]any{"piece": "tree", "x": 3, "z": 3})
	st.refused("game.placePiece", id, map[string]any{"id": "house", "piece": "tree", "x": 8, "z": 0})
	// Turning the house moves its path: refused while the road meets it
	st.refused("game.movePiece", id, map[string]any{"id": "house", "rot": 2})
	st.refused("game.setArea", id, map[string]any{"width": 3})
	res = st.ok("game.setArea", id, map[string]any{"name": "Farmstead", "width": 12})
	if a := read(res.Seq); a.Name != "Farmstead" || a.Width != 12 || a.Depth != 8 {
		t.Fatalf("after setArea %+v", a)
	}
	res = st.ok("game.removePiece", id, map[string]any{"id": "house"})
	if a := read(res.Seq); len(a.Pieces) != 1 {
		t.Fatalf("after remove %+v", a.Pieces)
	}

	// The model's description
	var tool *tools.Tool
	for _, x := range tools.Tools.All(st.reg) {
		if x.ID == "game.describeArea" {
			tool = &x
		}
	}
	out, err := tool.Run(tools.Call{ServiceArgs: userArgs(1), Name: tool.ID, Args: json.RawMessage(`{"area":"` + id + `"}`)})
	if err != nil {
		t.Fatal(err)
	}
	m := out.Content["map"].([]any)
	if len(m) != 8 || m[4].(string) != "...A........" {
		t.Fatalf("map %v", m)
	}
	t.Log(out.Summary)
}
