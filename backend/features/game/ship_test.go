package game_test

import (
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/game"
)

// V3: a ship is started from a design, its blocks placed, then flown by one pilot at a time
func TestShips(t *testing.T) {
	d, ok := game.DesignOf("skiff")
	if !ok || d.Check() != "" || d.Blocks[0].Type != "cockpit" {
		t.Fatalf("skiff %+v", d)
	}
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Ship moon"}).Target
	q := []float64{0, 0, 0, 1}
	res := st.ok("game.startShip", "s1", map[string]any{"world": world, "design": "skiff", "x": 0, "y": 104, "z": 0, "q": q})
	read := func(seq uint64) game.Ship {
		var s game.Ship
		if !st.read(game.ShipsProjector, game.ShipType+".s1", seq, &s) {
			t.Fatal("no ship")
		}
		return s
	}
	if s := read(res.Seq); s.World != world || s.Blocks != len(d.Blocks) || len(s.Placed) != 0 {
		t.Fatalf("started %+v", s)
	}
	var byWorld game.Ship
	if !st.read(game.WorldShipsProjector, world+".s1", res.Seq, &byWorld) || byWorld.Design != "skiff" {
		t.Fatalf("by world %+v", byWorld)
	}
	st.refused("game.startShip", "s1", map[string]any{"world": world, "design": "skiff", "x": 0, "y": 104, "z": 0, "q": q})
	st.refused("game.startShip", "s2", map[string]any{"world": world, "design": "dreadnought", "x": 0, "y": 104, "z": 0, "q": q})
	st.refused("game.startShip", "s2", map[string]any{"world": world, "design": "skiff", "x": 0, "y": 9000, "z": 0, "q": q})

	// Flying needs the cockpit
	if r := st.refused("game.flyShip", "s1", map[string]any{"x": 0, "y": 105, "z": 0, "q": q, "pilot": true}); !strings.Contains(r.Common.Message, "cockpit") {
		t.Fatalf("fly before the cockpit: %q", r.Common.Message)
	}
	st.ok("game.placeBlock", "s1", map[string]any{"index": 5})
	res = st.ok("game.placeBlock", "s1", map[string]any{"index": 0})
	if s := read(res.Seq); len(s.Placed) != 2 || s.Placed[0] != 0 || s.Placed[1] != 5 {
		t.Fatalf("placed %+v", s.Placed)
	}
	st.refused("game.placeBlock", "s1", map[string]any{"index": 0})
	st.refused("game.placeBlock", "s1", map[string]any{"index": len(d.Blocks)})

	res = st.ok("game.flyShip", "s1", map[string]any{"x": 1, "y": 106, "z": 2, "q": q, "v": []float64{0, 1, 0}, "pilot": true})
	if s := read(res.Seq); s.Pilot != "1" || s.Y != 106 || s.V[1] != 1 {
		t.Fatalf("flown %+v", s)
	}
	// One pilot at a time
	if r := st.send(2, "game.flyShip", "s1", map[string]any{"x": 1, "y": 106, "z": 2, "q": q, "pilot": true}); r.Common.Code < 300 || !strings.Contains(r.Common.Message, "someone else") {
		t.Fatalf("a second pilot: %d %q", r.Common.Code, r.Common.Message)
	}
	st.ok("game.flyShip", "s1", map[string]any{"x": 1, "y": 106, "z": 2, "q": q, "pilot": false})
	if r := st.send(2, "game.flyShip", "s1", map[string]any{"x": 1, "y": 107, "z": 2, "q": q, "pilot": true}); r.Common.Code >= 300 {
		t.Fatalf("after leaving: %q", r.Common.Message)
	}
}

// V4: a prospector's hold takes what the pack dug, and its refinery turns rock into iron for charge
func TestProspector(t *testing.T) {
	d, ok := game.DesignOf("prospector")
	if !ok || d.Check() != "" || d.Blocks[28].Type != "cargo" || d.Blocks[29].Type != "refinery" {
		t.Fatalf("prospector %+v", d)
	}
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Ore moon"}).Target
	q := []float64{0, 0, 0, 1}
	st.ok("game.startShip", "p1", map[string]any{"world": world, "design": "prospector", "x": 0, "y": 104, "z": 0, "q": q})
	read := func(seq uint64) game.Ship {
		var s game.Ship
		if !st.read(game.ShipsProjector, game.ShipType+".p1", seq, &s) {
			t.Fatal("no ship")
		}
		return s
	}
	pack := func(seq uint64) game.Pack {
		var p game.Pack
		st.read(game.PacksProjector, game.PackKey(world, 1), seq, &p)
		return p
	}
	if r := st.refused("game.loadShip", "p1", map[string]any{}); !strings.Contains(r.Common.Message, "cargo hold") {
		t.Fatalf("no hold: %q", r.Common.Message)
	}
	st.ok("game.placeBlock", "p1", map[string]any{"index": 28})
	if r := st.refused("game.loadShip", "p1", map[string]any{}); !strings.Contains(r.Common.Message, "pack is empty") {
		t.Fatalf("empty pack: %q", r.Common.Message)
	}
	// dig 1 dust and 1.5 rock into the pack, then load it
	st.ok("game.editTerrain", world, map[string]any{"kind": "dig", "x": 0, "y": 100, "z": 0, "radius": 1, "took": map[string]any{"dust": 1, "rock": 1.5}})
	res := st.ok("game.loadShip", "p1", map[string]any{})
	if s := read(res.Seq); s.Cargo.Dust != 1 || s.Cargo.Rock != 1.5 {
		t.Fatalf("loaded %+v", s.Cargo)
	}
	if p := pack(res.Seq); p.Dust != 0 || p.Rock != 0 {
		t.Fatalf("pack after loading %+v", p)
	}
	// refining needs the refinery, nobody flying, charge, and rock
	if r := st.refused("game.refineShip", "p1", map[string]any{}); !strings.Contains(r.Common.Message, "no refinery") {
		t.Fatalf("no refinery: %q", r.Common.Message)
	}
	st.ok("game.placeBlock", "p1", map[string]any{"index": 29})
	st.ok("game.placeBlock", "p1", map[string]any{"index": 0})
	st.ok("game.flyShip", "p1", map[string]any{"x": 0, "y": 104, "z": 0, "q": q, "pilot": true, "charge": 0.5})
	if r := st.refused("game.refineShip", "p1", map[string]any{}); !strings.Contains(r.Common.Message, "leave the seat") {
		t.Fatalf("flying: %q", r.Common.Message)
	}
	res = st.ok("game.flyShip", "p1", map[string]any{"x": 0, "y": 104, "z": 0, "q": q, "pilot": false})
	if s := read(res.Seq); s.Charge != 0.5 {
		t.Fatalf("charge kept %+v", s.Charge)
	}
	res = st.ok("game.refineShip", "p1", map[string]any{})
	if s := read(res.Seq); s.Cargo.Rock != 0.5 || s.Cargo.Iron != 0.5 || s.Charge != 0.4 {
		t.Fatalf("refined %+v charge %v", s.Cargo, s.Charge)
	}
	st.ok("game.refineShip", "p1", map[string]any{})
	if r := st.refused("game.refineShip", "p1", map[string]any{}); !strings.Contains(r.Common.Message, "no rock") {
		t.Fatalf("no rock left: %q", r.Common.Message)
	}
}

// V5: a crash breaks blocks, reported by the pilot; they go back to the outline and are placed again
func TestShipDamage(t *testing.T) {
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Crash moon"}).Target
	q := []float64{0, 0, 0, 1}
	st.ok("game.startShip", "c1", map[string]any{"world": world, "design": "skiff", "x": 0, "y": 104, "z": 0, "q": q})
	for _, i := range []int{0, 4, 5, 6} {
		st.ok("game.placeBlock", "c1", map[string]any{"index": i})
	}
	read := func(seq uint64) game.Ship {
		var s game.Ship
		st.read(game.ShipsProjector, game.ShipType+".c1", seq, &s)
		return s
	}
	if r := st.refused("game.damageShip", "c1", map[string]any{"blocks": []int{4}}); !strings.Contains(r.Common.Message, "pilot") {
		t.Fatalf("not flying: %q", r.Common.Message)
	}
	st.ok("game.flyShip", "c1", map[string]any{"x": 0, "y": 104, "z": 0, "q": q, "pilot": true})
	st.refused("game.damageShip", "c1", map[string]any{"blocks": []int{20}})
	res := st.ok("game.damageShip", "c1", map[string]any{"blocks": []int{4, 5, 20}})
	if s := read(res.Seq); len(s.Placed) != 2 || s.Placed[0] != 0 || s.Placed[1] != 6 || s.Pilot != "1" || s.Blocks != 27 {
		t.Fatalf("damaged %+v", s)
	}
	st.ok("game.placeBlock", "c1", map[string]any{"index": 4})
	res = st.ok("game.damageShip", "c1", map[string]any{"blocks": []int{0}})
	if s := read(res.Seq); s.Pilot != "" || len(s.Placed) != 2 {
		t.Fatalf("cockpit broken %+v", s)
	}
}

// V6: contracts handed in with a ship at the outpost pay a skill once
func TestOutpost(t *testing.T) {
	if c := game.ContractsOf(); len(c.Contracts) != 2 || c.Giver.Name == "" {
		t.Fatalf("contracts %+v", c)
	}
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Outpost moon"}).Target
	q := []float64{0, 0, 0, 1}
	// one ship far from the pole, one by the beacon (radius 100, hills 6)
	st.ok("game.startShip", "far", map[string]any{"world": world, "design": "skiff", "x": 104, "y": 0, "z": 0, "q": q})
	st.ok("game.startShip", "o1", map[string]any{"world": world, "design": "skiff", "x": 0, "y": 104, "z": 5, "q": q})
	if r := st.refused("game.handIn", "far", map[string]any{"contract": "first-flight"}); !strings.Contains(r.Common.Message, "within 40 m") {
		t.Fatalf("far: %q", r.Common.Message)
	}
	if r := st.refused("game.handIn", "o1", map[string]any{"contract": "first-flight"}); !strings.Contains(r.Common.Message, "blocks to place") {
		t.Fatalf("unfinished: %q", r.Common.Message)
	}
	for i := 0; i < 27; i++ {
		st.ok("game.placeBlock", "o1", map[string]any{"index": i})
	}
	res := st.ok("game.handIn", "o1", map[string]any{"contract": "first-flight"})
	var p game.Player
	if !st.read(game.PlayersProjector, game.PackKey(world, 1), res.Seq, &p) || !p.Done["first-flight"] || p.XP["piloting"] != 100 || p.Levels["piloting"] != 2 {
		t.Fatalf("player %+v", p)
	}
	if r := st.refused("game.handIn", "o1", map[string]any{"contract": "first-flight"}); !strings.Contains(r.Common.Message, "already") {
		t.Fatalf("twice: %q", r.Common.Message)
	}
	if r := st.refused("game.handIn", "o1", map[string]any{"contract": "beacon-iron"}); !strings.Contains(r.Common.Message, "iron") {
		t.Fatalf("no iron: %q", r.Common.Message)
	}
}
