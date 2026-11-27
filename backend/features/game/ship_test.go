package game_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/game"
	"github.com/SgtFlame/foundation/backend/features/live/bus"
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
	dug := st.ok("game.editTerrain", world, map[string]any{"kind": "dig", "x": 0, "y": 100, "z": 0, "radius": 1, "took": map[string]any{"dust": 1, "rock": 1.5}})
	pack(dug.Seq) // the hold reads the pack's projection, which folds on its own
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
	if c := game.ContractsOf(); len(c.Contracts) != 11 || c.Giver.Name == "" {
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

// V7: a crew member boards a finished ship, refines from the crew seat
// while it is flown, and leaves it
func TestShipCrew(t *testing.T) {
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Crew moon"}).Target
	q := []float64{0, 0, 0, 1}
	st.ok("game.startShip", "k1", map[string]any{"world": world, "design": "prospector", "x": 0, "y": 104, "z": 0, "q": q})
	read := func(seq uint64) game.Ship {
		var s game.Ship
		st.read(game.ShipsProjector, game.ShipType+".k1", seq, &s)
		return s
	}
	if r := st.refused("game.crewShip", "k1", map[string]any{"aboard": true}); !strings.Contains(r.Common.Message, "finish") {
		t.Fatalf("unfinished: %q", r.Common.Message)
	}
	for i := 0; i < 32; i++ {
		st.ok("game.placeBlock", "k1", map[string]any{"index": i})
	}
	st.refused("game.crewShip", "k1", map[string]any{"aboard": false})
	res := st.ok("game.crewShip", "k1", map[string]any{"aboard": true})
	if s := read(res.Seq); len(s.Crew) != 1 || s.Crew[0] != "1" {
		t.Fatalf("boarded %+v", s.Crew)
	}
	if r := st.refused("game.crewShip", "k1", map[string]any{"aboard": true}); !strings.Contains(r.Common.Message, "already") {
		t.Fatalf("twice: %q", r.Common.Message)
	}
	st.ok("game.crewShip", "k1", map[string]any{"aboard": false})
	// the pilot cannot crew, and taking the seat leaves the crew seat
	st.ok("game.crewShip", "k1", map[string]any{"aboard": true})
	res = st.ok("game.flyShip", "k1", map[string]any{"x": 0, "y": 104, "z": 0, "q": q, "pilot": true})
	if s := read(res.Seq); len(s.Crew) != 0 || s.Pilot != "1" {
		t.Fatalf("seated %+v", s)
	}
	if r := st.refused("game.crewShip", "k1", map[string]any{"aboard": true}); !strings.Contains(r.Common.Message, "flying") {
		t.Fatalf("pilot crews: %q", r.Common.Message)
	}
	if r := st.refused("game.refineShip", "k1", map[string]any{}); !strings.Contains(r.Common.Message, "crew seat") {
		t.Fatalf("refine while flown: %q", r.Common.Message)
	}
}

// V8: where a player stands is saved per world; the first save is their landing
func TestStandAt(t *testing.T) {
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Landing moon"}).Target
	st.refused("game.standAt", world, map[string]any{"x": 0, "y": 5000, "z": 0})
	st.refused("game.standAt", "nowhere", map[string]any{"x": 0, "y": 100, "z": 0})
	res := st.ok("game.standAt", world, map[string]any{"x": 1.234, "y": 101.5, "z": -3})
	var p game.Player
	if !st.read(game.PlayersProjector, game.PackKey(world, 1), res.Seq, &p) || p.At == nil || p.At[0] != 1.23 || p.At[1] != 101.5 {
		t.Fatalf("stood %+v", p)
	}
}

// V8: ore dug on an asteroid rides the pack and the hold; cobalt for the
// shipyard unlocks the hauler, and the haul needs one
func TestOreAndHauler(t *testing.T) {
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Ore moon"}).Target
	q := []float64{0, 0, 0, 1}
	st.ok("game.startShip", "p1", map[string]any{"world": world, "design": "prospector", "x": 0, "y": 104, "z": 5, "q": q})
	for i := 0; i < 32; i++ {
		st.ok("game.placeBlock", "p1", map[string]any{"index": i})
	}
	if r := st.refused("game.startShip", "h0", map[string]any{"world": world, "design": "hauler", "x": 0, "y": 104, "z": -8, "q": q}); !strings.Contains(r.Common.Message, "cobalt") {
		t.Fatalf("locked hauler: %q", r.Common.Message)
	}
	res := st.ok("game.editTerrain", world, map[string]any{"kind": "dig", "x": 0, "y": 200, "z": 0, "radius": 1.5, "took": map[string]any{"dust": 0.2, "rock": 0.3, "ore": 2.5}})
	var pk game.Pack
	if !st.read(game.PacksProjector, game.PackKey(world, 1), res.Seq, &pk) || pk.Ore != 2.5 {
		t.Fatalf("pack %+v", pk)
	}
	res = st.ok("game.loadShip", "p1", map[string]any{})
	var s game.Ship
	if !st.read(game.ShipsProjector, game.ShipType+".p1", res.Seq, &s) || s.Cargo.Ore != 2.5 {
		t.Fatalf("hold %+v", s.Cargo)
	}
	if r := st.refused("game.handIn", "p1", map[string]any{"contract": "ore-haul"}); !strings.Contains(r.Common.Message, "hauler") {
		t.Fatalf("haul in a prospector: %q", r.Common.Message)
	}
	res = st.ok("game.handIn", "p1", map[string]any{"contract": "cobalt-shipyard"})
	var p game.Player
	if !st.read(game.PlayersProjector, game.PackKey(world, 1), res.Seq, &p) || !p.Unlocked["hauler"] || p.XP["mining"] != 100 {
		t.Fatalf("unlocked %+v", p)
	}
	res = st.ok("game.startShip", "h1", map[string]any{"world": world, "design": "hauler", "x": 0, "y": 104, "z": -8, "q": q})
	if !st.read(game.ShipsProjector, game.ShipType+".h1", res.Seq, &s) || s.Blocks != 58 {
		t.Fatalf("hauler %+v", s)
	}
	if !st.read(game.ShipsProjector, game.ShipType+".p1", 0, &s) || s.Cargo.Ore != 0.5 {
		t.Fatalf("hold after %+v", s.Cargo)
	}
	if r := st.refused("game.handIn", "h1", map[string]any{"contract": "ore-haul"}); !strings.Contains(r.Common.Message, "12") {
		t.Fatalf("empty hauler: %q", r.Common.Message)
	}
}

// V11: the job board's jobs are done again and again, alone teach Trade,
// and Trade 3 unlocks the freighter
func TestJobBoard(t *testing.T) {
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Job moon"}).Target
	q := []float64{0, 0, 0, 1}
	st.ok("game.startShip", "p1", map[string]any{"world": world, "design": "prospector", "x": 0, "y": 104, "z": 5, "q": q})
	for i := 0; i < 32; i++ {
		st.ok("game.placeBlock", "p1", map[string]any{"index": i})
	}
	if r := st.refused("game.startShip", "f0", map[string]any{"world": world, "design": "freighter", "x": 0, "y": 104, "z": -8, "q": q}); !strings.Contains(r.Common.Message, "Trade") {
		t.Fatalf("locked freighter: %q", r.Common.Message)
	}
	if r := st.refused("game.handIn", "p1", map[string]any{"contract": "bulk-ore"}); !strings.Contains(r.Common.Message, "Trade level 2") {
		t.Fatalf("bulk at trade 1: %q", r.Common.Message)
	}
	var p game.Player
	for i := 0; i < 5; i++ {
		dug := st.ok("game.editTerrain", world, map[string]any{"kind": "dig", "x": 0, "y": 200, "z": 0, "radius": 1.5, "took": map[string]any{"dust": 0, "rock": 3.2, "ore": 0}})
		var pk game.Pack
		if !st.read(game.PacksProjector, game.PackKey(world, 1), dug.Seq, &pk) || pk.Rock < 3 {
			t.Fatalf("pack %+v", pk)
		}
		st.ok("game.loadShip", "p1", map[string]any{})
		res := st.ok("game.handIn", "p1", map[string]any{"contract": "pad-rock"})
		p = game.Player{}
		st.read(game.PlayersProjector, game.PackKey(world, 1), res.Seq, &p)
	}
	if p.Jobs["pad-rock"] != 5 || p.XP["trade"] != 200 || p.Levels["trade"] != 3 || !p.Unlocked["freighter"] || p.Done["pad-rock"] {
		t.Fatalf("after five jobs %+v", p)
	}
	if r := st.refused("game.handIn", "p1", map[string]any{"contract": "pad-rock"}); !strings.Contains(r.Common.Message, "rock") {
		t.Fatalf("empty hold: %q", r.Common.Message)
	}
	res := st.ok("game.startShip", "f1", map[string]any{"world": world, "design": "freighter", "x": 0, "y": 104, "z": -8, "q": q})
	var s game.Ship
	if !st.read(game.ShipsProjector, game.ShipType+".f1", res.Seq, &s) || s.Blocks != 82 {
		t.Fatalf("freighter %+v", s)
	}
}

// V13: a cabin is a building on the planet; sleeping in its bed heals and makes it home
func TestCabin(t *testing.T) {
	d, ok := game.DesignOf("cabin")
	if !ok || !d.Static || d.Check() != "" || d.Blocks[73].Type != "bed" || d.Blocks[74].Type != "bench" || d.Blocks[76].Type != "fabricator" || d.Blocks[77].Type != "solar" || len(d.Blocks) != 78 {
		t.Fatalf("cabin %+v", d)
	}
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Cabin moon"}).Target
	q := []float64{0, 0, 0, 1}
	if r := st.refused("game.startShip", "c1", map[string]any{"world": world, "design": "cabin", "x": 0, "y": 104, "z": 0, "q": q}); !strings.Contains(r.Common.Message, "planet") {
		t.Fatalf("cabin on the moon: %q", r.Common.Message)
	}
	st.ok("game.startShip", "c1", map[string]any{"world": world, "design": "cabin", "x": 1600, "y": 142, "z": 0, "q": q})
	st.ok("game.startShip", "s1", map[string]any{"world": world, "design": "skiff", "x": 0, "y": 104, "z": 0, "q": q})
	st.refused("game.sleep", "s1", map[string]any{})
	if r := st.refused("game.sleep", "c1", map[string]any{}); !strings.Contains(r.Common.Message, "bed") {
		t.Fatalf("sleep before the bed: %q", r.Common.Message)
	}
	st.ok("game.standAt", world, map[string]any{"x": 1600, "y": 142, "z": 0, "needs": map[string]any{"air": 100, "fed": 50, "health": 20}})
	st.ok("game.placeBlock", "c1", map[string]any{"index": 73})
	res := st.ok("game.sleep", "c1", map[string]any{})
	var p game.Player
	if !st.read(game.PlayersProjector, game.PackKey(world, 1), res.Seq, &p) || p.Home != "c1" || p.Needs == nil || p.Needs.Health != 100 || p.Needs.Fed != 50 {
		t.Fatalf("player %+v %+v", p, p.Needs)
	}
}

// V14: a hand drill crafted at the cabin's bench from ore and rock
func TestCraftDrill(t *testing.T) {
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Drill moon"}).Target
	q := []float64{0, 0, 0, 1}
	st.ok("game.startShip", "c1", map[string]any{"world": world, "design": "cabin", "x": 1600, "y": 142, "z": 0, "q": q})
	if r := st.refused("game.craft", "c1", map[string]any{"item": "drill"}); !strings.Contains(r.Common.Message, "bench") {
		t.Fatalf("craft before the bench: %q", r.Common.Message)
	}
	st.ok("game.placeBlock", "c1", map[string]any{"index": 74})
	if r := st.refused("game.craft", "c1", map[string]any{"item": "drill"}); !strings.Contains(r.Common.Message, "ore") {
		t.Fatalf("craft with an empty pack: %q", r.Common.Message)
	}
	if r := st.refused("game.craft", "c1", map[string]any{"item": "rocket"}); !strings.Contains(r.Common.Message, "two recipes") {
		t.Fatalf("unknown recipe: %q", r.Common.Message)
	}
	dug := st.ok("game.editTerrain", world, map[string]any{"kind": "dig", "x": 0, "y": 200, "z": 0, "radius": 1.5, "took": map[string]any{"dust": 0, "rock": 1.2, "ore": 2.5}})
	var pack game.Pack
	st.read(game.PacksProjector, game.PackKey(world, 1), dug.Seq, &pack) // the bench reads the pack's projection, which folds on its own
	res := st.ok("game.craft", "c1", map[string]any{"item": "drill"})
	var p game.Player
	if !st.read(game.PlayersProjector, game.PackKey(world, 1), res.Seq, &p) || !p.Tools["drill"] {
		t.Fatalf("player %+v", p)
	}
	if !st.read(game.PacksProjector, game.PackKey(world, 1), res.Seq, &pack) || pack.Ore != 0.5 || pack.Rock != 0.2 {
		t.Fatalf("pack %+v", pack)
	}
	if r := st.refused("game.craft", "c1", map[string]any{"item": "drill"}); !strings.Contains(r.Common.Message, "already") {
		t.Fatalf("a second drill: %q", r.Common.Message)
	}
}

// V17: a sentry turret crafted at the bench and set down on the moon
func TestTurret(t *testing.T) {
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Turret moon"}).Target
	q := []float64{0, 0, 0, 1}
	if r := st.refused("game.placeTurret", world, map[string]any{"x": 0, "y": 104, "z": 0}); !strings.Contains(r.Common.Message, "craft a turret") {
		t.Fatalf("a turret before crafting one: %q", r.Common.Message)
	}
	st.ok("game.startShip", "c1", map[string]any{"world": world, "design": "cabin", "x": 1600, "y": 142, "z": 0, "q": q})
	st.ok("game.placeBlock", "c1", map[string]any{"index": 74})
	dug := st.ok("game.editTerrain", world, map[string]any{"kind": "dig", "x": 0, "y": 200, "z": 0, "radius": 1.5, "took": map[string]any{"dust": 0, "rock": 1, "ore": 2}})
	var pack game.Pack
	st.read(game.PacksProjector, game.PackKey(world, 1), dug.Seq, &pack)
	if r := st.refused("game.craft", "c1", map[string]any{"item": "turret"}); !strings.Contains(r.Common.Message, "1.5 m³ of rock") {
		t.Fatalf("a turret from too little: %q", r.Common.Message)
	}
	dug = st.ok("game.editTerrain", world, map[string]any{"kind": "dig", "x": 0, "y": 200, "z": 3, "radius": 1.5, "took": map[string]any{"dust": 0, "rock": 0.5, "ore": 0.25}})
	st.read(game.PacksProjector, game.PackKey(world, 1), dug.Seq, &pack)
	res := st.ok("game.craft", "c1", map[string]any{"item": "turret"})
	var p game.Player
	if !st.read(game.PlayersProjector, game.PackKey(world, 1), res.Seq, &p) || !p.Tools["turret"] || p.Tools["drill"] {
		t.Fatalf("player %+v", p)
	}
	if !st.read(game.PacksProjector, game.PackKey(world, 1), res.Seq, &pack) || pack.Ore != 0.25 || pack.Rock != 0 {
		t.Fatalf("pack %+v", pack)
	}
	if r := st.refused("game.placeTurret", world, map[string]any{"x": 1600, "y": 142, "z": 0}); !strings.Contains(r.Common.Message, "moon") {
		t.Fatalf("a turret on the planet: %q", r.Common.Message)
	}
	st.ok("game.placeTurret", world, map[string]any{"x": 0, "y": 104, "z": 0})
	res = st.ok("game.placeTurret", world, map[string]any{"x": 2, "y": 103, "z": 1})
	var s game.Surges
	if !st.read(game.SurgesProjector, game.TerrainType+"."+world, res.Seq, &s) || len(s.Turrets) != 1 || s.Turrets["1"].X != 2 {
		t.Fatalf("surges %+v", s)
	}
}

// V18: a wreck's manual teaches the bench the powered drill
func TestPowerDrill(t *testing.T) {
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Manual moon"}).Target
	q := []float64{0, 0, 0, 1}
	st.ok("game.startShip", "c1", map[string]any{"world": world, "design": "cabin", "x": 1600, "y": 142, "z": 0, "q": q})
	st.ok("game.placeBlock", "c1", map[string]any{"index": 74})
	dig := func(rock, ore float64) {
		res := st.ok("game.editTerrain", world, map[string]any{"kind": "dig", "x": 0, "y": 200, "z": 0, "radius": 1.5, "took": map[string]any{"dust": 0, "rock": rock, "ore": ore}})
		var pack game.Pack
		st.read(game.PacksProjector, game.PackKey(world, 1), res.Seq, &pack)
	}
	dig(1, 2)
	if r := st.refused("game.craft", "c1", map[string]any{"item": "powerDrill"}); !strings.Contains(r.Common.Message, "manual") {
		t.Fatalf("a powered drill without the manual: %q", r.Common.Message)
	}
	res := st.ok("game.salvage", world, map[string]any{"wreck": "k1", "index": game.ManualBlock, "x": 40, "y": 90, "z": 0})
	var p game.Player
	if !st.read(game.PlayersProjector, game.PackKey(world, 1), res.Seq, &p) || !p.Manuals["powerDrill"] {
		t.Fatalf("player after the manual %+v", p)
	}
	if r := st.refused("game.craft", "c1", map[string]any{"item": "powerDrill"}); !strings.Contains(r.Common.Message, "hand drill") {
		t.Fatalf("a powered drill without a drill: %q", r.Common.Message)
	}
	st.ok("game.craft", "c1", map[string]any{"item": "drill"})
	dig(1, 2.25)
	res = st.ok("game.craft", "c1", map[string]any{"item": "powerDrill"})
	if !st.read(game.PlayersProjector, game.PackKey(world, 1), res.Seq, &p) || !p.Tools["powerDrill"] || !p.Tools["drill"] {
		t.Fatalf("player %+v", p)
	}
}

// V20: the cabin's forge smelts ore into iron over time
func TestForge(t *testing.T) {
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Forge moon"}).Target
	q := []float64{0, 0, 0, 1}
	st.ok("game.startShip", "c1", map[string]any{"world": world, "design": "cabin", "x": 1600, "y": 142, "z": 0, "q": q})
	if r := st.refused("game.smelt", "c1", map[string]any{}); !strings.Contains(r.Common.Message, "forge") {
		t.Fatalf("smelt before the forge: %q", r.Common.Message)
	}
	st.ok("game.placeBlock", "c1", map[string]any{"index": 75})
	if r := st.refused("game.smelt", "c1", map[string]any{}); !strings.Contains(r.Common.Message, "ore") {
		t.Fatalf("smelt with an empty pack: %q", r.Common.Message)
	}
	dug := st.ok("game.editTerrain", world, map[string]any{"kind": "dig", "x": 0, "y": 200, "z": 0, "radius": 1.5, "took": map[string]any{"dust": 0, "rock": 0, "ore": 0.02}})
	var pack game.Pack
	st.read(game.PacksProjector, game.PackKey(world, 1), dug.Seq, &pack)
	res := st.ok("game.smelt", "c1", map[string]any{})
	var s game.Ship
	if !st.read(game.ShipsProjector, game.ShipType+".c1", res.Seq, &s) || s.Forge == nil || s.Forge.Ore != 0.02 {
		t.Fatalf("ship %+v", s)
	}
	if !st.read(game.PacksProjector, game.PackKey(world, 1), res.Seq, &pack) || pack.Ore != 0 {
		t.Fatalf("pack %+v", pack)
	}
	if r := st.refused("game.takeIngots", "c1", map[string]any{}); !strings.Contains(r.Common.Message, "still smelting") {
		t.Fatalf("take while smelting: %q", r.Common.Message)
	}
	if r := st.refused("game.smelt", "c1", map[string]any{}); r.Common.Message == "" {
		t.Fatalf("smelt twice")
	}
	time.Sleep(time.Second)
	res = st.ok("game.takeIngots", "c1", map[string]any{})
	if !st.read(game.PacksProjector, game.PackKey(world, 1), res.Seq, &pack) || pack.Iron != 0.016 {
		t.Fatalf("pack %+v", pack)
	}
	var after game.Ship
	if !st.read(game.ShipsProjector, game.ShipType+".c1", res.Seq, &after) || after.Forge != nil {
		t.Fatalf("ship %+v", after)
	}
	// V21: the ore scanner is made from the forge's iron at the bench
	st.ok("game.placeBlock", "c1", map[string]any{"index": 74})
	if r := st.refused("game.craft", "c1", map[string]any{"item": "scanner"}); !strings.Contains(r.Common.Message, "1 m³ of iron and 0.5 m³ of ore") {
		t.Fatalf("a scanner from too little: %q", r.Common.Message)
	}
	// V22: the suit's iron air tank
	if r := st.refused("game.craft", "c1", map[string]any{"item": "airTank"}); !strings.Contains(r.Common.Message, "1.5 m³ of iron and 0.5 m³ of rock") {
		t.Fatalf("an air tank from too little: %q", r.Common.Message)
	}
}

// V23: the fabricator, powered by the cabin's solar panel, makes circuit boards from iron and ore
func TestFabricator(t *testing.T) {
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Fab moon"}).Target
	q := []float64{0, 0, 0, 1}
	st.ok("game.startShip", "c1", map[string]any{"world": world, "design": "cabin", "x": 1600, "y": 142, "z": 0, "q": q})
	if r := st.refused("game.fabricate", "c1", map[string]any{}); !strings.Contains(r.Common.Message, "not built") {
		t.Fatalf("before the fabricator: %q", r.Common.Message)
	}
	st.ok("game.placeBlock", "c1", map[string]any{"index": 76})
	if r := st.refused("game.fabricate", "c1", map[string]any{}); !strings.Contains(r.Common.Message, "solar panel") {
		t.Fatalf("without power: %q", r.Common.Message)
	}
	st.ok("game.placeBlock", "c1", map[string]any{"index": 77})
	if r := st.refused("game.fabricate", "c1", map[string]any{}); !strings.Contains(r.Common.Message, "0.5 m³ of iron") {
		t.Fatalf("with an empty pack: %q", r.Common.Message)
	}
	// iron by the forge, quickly: a little ore smelts in a moment
	st.ok("game.placeBlock", "c1", map[string]any{"index": 75})
	dug := st.ok("game.editTerrain", world, map[string]any{"kind": "dig", "x": 0, "y": 200, "z": 0, "radius": 1.5, "took": map[string]any{"dust": 0, "rock": 0, "ore": 0.02}})
	var pack game.Pack
	st.read(game.PacksProjector, game.PackKey(world, 1), dug.Seq, &pack)
	st.ok("game.smelt", "c1", map[string]any{})
	if r := st.refused("game.fabricate", "c1", map[string]any{}); !strings.Contains(r.Common.Message, "0.5 m³ of iron and 0.25 m³ of ore") {
		t.Fatalf("with too little iron: %q", r.Common.Message)
	}
	if r := st.refused("game.takeFabricated", "c1", map[string]any{}); !strings.Contains(r.Common.Message, "empty") {
		t.Fatalf("take from an empty fabricator: %q", r.Common.Message)
	}
	// V24: the ion thruster kit takes circuit boards, and fits only a ship
	if r := st.refused("game.fabricate", "c1", map[string]any{"item": "ionKit"}); !strings.Contains(r.Common.Message, "2 circuit boards") {
		t.Fatalf("a kit without boards: %q", r.Common.Message)
	}
	if r := st.refused("game.fabricate", "c1", map[string]any{"item": "rocket"}); !strings.Contains(r.Common.Message, "ionKit") {
		t.Fatalf("an unknown item: %q", r.Common.Message)
	}
	if r := st.refused("game.fitKit", "c1", map[string]any{}); !strings.Contains(r.Common.Message, "cabin") {
		t.Fatalf("a kit on a cabin: %q", r.Common.Message)
	}
	st.ok("game.startShip", "s1", map[string]any{"world": world, "design": "skiff", "x": 0, "y": 110, "z": 0, "q": q})
	if r := st.refused("game.fitKit", "s1", map[string]any{}); !strings.Contains(r.Common.Message, "no ion thruster kit") {
		t.Fatalf("a kit from an empty pack: %q", r.Common.Message)
	}
}

// V25: Trade 4 unlocks the ore barge, whose stock thrusters lift its holds
// only half full until ion thruster kits are fitted; the freighter loads
// every hold
func TestBarge(t *testing.T) {
	d, ok := game.DesignOf("barge")
	if !ok || d.Check() != "" || len(d.Blocks) != 112 || d.Lift != 0.5 {
		t.Fatalf("barge %+v", d)
	}
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Barge moon"}).Target
	q := []float64{0, 0, 0, 1}
	st.ok("game.startShip", "p1", map[string]any{"world": world, "design": "prospector", "x": 0, "y": 104, "z": 5, "q": q})
	for i := 0; i < 32; i++ {
		st.ok("game.placeBlock", "p1", map[string]any{"index": i})
	}
	dig := func() {
		dug := st.ok("game.editTerrain", world, map[string]any{"kind": "dig", "x": 0, "y": 200, "z": 0, "radius": 1.5, "took": map[string]any{"dust": 0, "rock": 3.2, "ore": 0}})
		var pk game.Pack
		st.read(game.PacksProjector, game.PackKey(world, 1), dug.Seq, &pk)
	}
	var p game.Player
	for i := 0; i < 8; i++ {
		if i == 5 {
			if r := st.refused("game.startShip", "b0", map[string]any{"world": world, "design": "barge", "x": 0, "y": 104, "z": -8, "q": q}); !strings.Contains(r.Common.Message, "Trade has reached level 4") {
				t.Fatalf("locked barge: %q", r.Common.Message)
			}
		}
		dig()
		st.ok("game.loadShip", "p1", map[string]any{})
		res := st.ok("game.handIn", "p1", map[string]any{"contract": "pad-rock"})
		p = game.Player{}
		st.read(game.PlayersProjector, game.PackKey(world, 1), res.Seq, &p)
	}
	if p.Levels["trade"] != 4 || !p.Unlocked["barge"] || !p.Unlocked["freighter"] {
		t.Fatalf("after eight jobs %+v", p)
	}
	if r := st.refused("game.handIn", "p1", map[string]any{"contract": "refinery-ore"}); !strings.Contains(r.Common.Message, "needs a barge") {
		t.Fatalf("refinery ore from a prospector: %q", r.Common.Message)
	}
	st.ok("game.startShip", "b1", map[string]any{"world": world, "design": "barge", "x": 0, "y": 104, "z": -8, "q": q})
	refineParts(t, st, world, map[string]int{"plate": 12}) // V32: three holds' plates
	// two holds placed: 12 m³, of which the stock thrusters lift 6
	st.ok("game.placeBlock", "b1", map[string]any{"index": 27})
	st.ok("game.placeBlock", "b1", map[string]any{"index": 97})
	dig()
	st.ok("game.loadShip", "b1", map[string]any{})
	dig()
	res := st.ok("game.loadShip", "b1", map[string]any{})
	var s game.Ship
	if !st.read(game.ShipsProjector, game.ShipType+".b1", res.Seq, &s) || s.Cargo.Rock < 5.99 || s.Cargo.Rock > 6.01 {
		t.Fatalf("barge load %+v", s.Cargo)
	}
	if r := st.refused("game.loadShip", "b1", map[string]any{}); !strings.Contains(r.Common.Message, "lift only 6 of its 12") {
		t.Fatalf("barge lift: %q", r.Common.Message)
	}
	// the freighter's holds load (they were missing before V25)
	st.ok("game.startShip", "f1", map[string]any{"world": world, "design": "freighter", "x": 0, "y": 104, "z": 20, "q": q})
	st.ok("game.placeBlock", "f1", map[string]any{"index": 43})
	res = st.ok("game.loadShip", "f1", map[string]any{})
	s = game.Ship{}
	if !st.read(game.ShipsProjector, game.ShipType+".f1", res.Seq, &s) || s.Cargo.Rock < 0.39 {
		t.Fatalf("freighter load %+v", s.Cargo)
	}
}

// V27: a wreck's drive core gives a jump coil; the fabricator makes a jump
// drive from it, and only a powered ship takes one
func TestJumpDrive(t *testing.T) {
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Jump moon"}).Target
	q := []float64{0, 0, 0, 1}
	st.ok("game.startShip", "c1", map[string]any{"world": world, "design": "cabin", "x": 1600, "y": 142, "z": 0, "q": q})
	st.ok("game.placeBlock", "c1", map[string]any{"index": 76})
	st.ok("game.placeBlock", "c1", map[string]any{"index": 77})
	if r := st.refused("game.fabricate", "c1", map[string]any{"item": "jumpDrive"}); !strings.Contains(r.Common.Message, "jump coil: salvage") {
		t.Fatalf("a drive without a coil: %q", r.Common.Message)
	}
	res := st.ok("game.salvage", world, map[string]any{"wreck": "k0", "index": 26, "x": 40, "y": 90, "z": 0})
	var pack game.Pack
	if !st.read(game.PacksProjector, game.PackKey(world, 1), res.Seq, &pack) || pack.Coils != 1 || pack.Ore != game.SalvageOre {
		t.Fatalf("pack %+v", pack)
	}
	res = st.ok("game.salvage", world, map[string]any{"wreck": "k0", "index": 5, "x": 40, "y": 90, "z": 0})
	if !st.read(game.PacksProjector, game.PackKey(world, 1), res.Seq, &pack) || pack.Coils != 1 {
		t.Fatalf("an ordinary block gave a coil: %+v", pack)
	}
	if r := st.refused("game.fabricate", "c1", map[string]any{"item": "jumpDrive"}); !strings.Contains(r.Common.Message, "1 m³ of ore and 2 circuit boards") {
		t.Fatalf("a drive from too little: %q", r.Common.Message)
	}
	if r := st.refused("game.fitDrive", "c1", map[string]any{}); !strings.Contains(r.Common.Message, "cabin") {
		t.Fatalf("a drive on a cabin: %q", r.Common.Message)
	}
	st.ok("game.startShip", "s1", map[string]any{"world": world, "design": "skiff", "x": 0, "y": 110, "z": 0, "q": q})
	if r := st.refused("game.fitDrive", "s1", map[string]any{}); !strings.Contains(r.Common.Message, "no battery") {
		t.Fatalf("a drive on a skiff: %q", r.Common.Message)
	}
	st.ok("game.startShip", "p1", map[string]any{"world": world, "design": "prospector", "x": 0, "y": 110, "z": 8, "q": q})
	if r := st.refused("game.fitDrive", "p1", map[string]any{}); !strings.Contains(r.Common.Message, "no jump drive") {
		t.Fatalf("a drive from an empty pack: %q", r.Common.Message)
	}
	// V28: the jump trial needs the drive fitted (the prospector stands by the beacon)
	if r := st.refused("game.handIn", "p1", map[string]any{"contract": "jump-trial"}); !strings.Contains(r.Common.Message, "jump drive fitted") {
		t.Fatalf("the jump trial without a drive: %q", r.Common.Message)
	}
}

// V29: minerals dug go into the pack by name, count against its room, and
// unknown names are dropped
func TestMinerals(t *testing.T) {
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Mineral moon"}).Target
	res := st.ok("game.editTerrain", world, map[string]any{"kind": "dig", "x": 0, "y": 100, "z": 0, "radius": 1, "took": map[string]any{"dust": 0.5, "rock": 0.5, "minerals": map[string]any{"ferrite": 0.8, "silica": 0.2, "gold": 1}}})
	var pack game.Pack
	if !st.read(game.PacksProjector, game.PackKey(world, 1), res.Seq, &pack) || pack.Minerals["ferrite"] != 0.8 || pack.Minerals["silica"] != 0.2 || len(pack.Minerals) != 2 || pack.Rock != 0.5 {
		t.Fatalf("pack %+v", pack)
	}
	if r := st.refused("game.editTerrain", world, map[string]any{"kind": "dig", "x": 0, "y": 100, "z": 4, "radius": 1, "took": map[string]any{"dust": 0, "rock": 0, "minerals": map[string]any{"ice": 2.5}}}); !strings.Contains(r.Common.Message, "pack is full") {
		t.Fatalf("minerals past the pack's room: %q", r.Common.Message)
	}
}

// V30: the forge refines minerals into components and rock into cement
func TestRefine(t *testing.T) {
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Refinery moon"}).Target
	st.ok("game.startShip", "c1", map[string]any{"world": world, "design": "cabin", "x": 1600, "y": 142, "z": 0, "q": []float64{0, 0, 0, 1}})
	st.ok("game.placeBlock", "c1", map[string]any{"index": 75})
	st.ok("game.editTerrain", world, map[string]any{"kind": "dig", "x": 0, "y": 100, "z": 0, "radius": 1, "took": map[string]any{"dust": 0, "rock": 0.05, "minerals": map[string]any{"malachite": 0.1, "ferrite": 0.05}}})
	if r := st.refused("game.smelt", "c1", map[string]any{"what": "gold"}); !strings.Contains(r.Common.Message, "cannot refine gold") {
		t.Fatalf("an unknown kind: %q", r.Common.Message)
	}
	if r := st.refused("game.smelt", "c1", map[string]any{"what": "ferrite"}); !strings.Contains(r.Common.Message, "too little ferrite") {
		t.Fatalf("too little to make a plate: %q", r.Common.Message)
	}
	if r := st.refused("game.smelt", "c1", map[string]any{"what": "silica"}); !strings.Contains(r.Common.Message, "no silica") {
		t.Fatalf("a mineral not carried: %q", r.Common.Message)
	}
	res := st.ok("game.smelt", "c1", map[string]any{"what": "malachite"})
	var s game.Ship
	if !st.read(game.ShipsProjector, game.ShipType+".c1", res.Seq, &s) || s.Forge == nil || s.Forge.Kind != "malachite" || s.Forge.Ore != 0.1 {
		t.Fatalf("ship %+v", s)
	}
	var pack game.Pack
	if !st.read(game.PacksProjector, game.PackKey(world, 1), res.Seq, &pack) || pack.Minerals["malachite"] != 0 || pack.Minerals["ferrite"] != 0.05 {
		t.Fatalf("pack %+v", pack)
	}
	if r := st.refused("game.takeIngots", "c1", map[string]any{}); !strings.Contains(r.Common.Message, "still smelting") {
		t.Fatalf("take while refining: %q", r.Common.Message)
	}
	time.Sleep(6100 * time.Millisecond) // 0.1 m³ at 60 s a m³
	res = st.ok("game.takeIngots", "c1", map[string]any{})
	if !st.read(game.PacksProjector, game.PackKey(world, 1), res.Seq, &pack) || pack.Parts["wire"] != 1 || pack.Iron != 0 {
		t.Fatalf("pack %+v", pack)
	}
	if r := st.refused("game.smelt", "c1", map[string]any{"what": "rock"}); !strings.Contains(r.Common.Message, "needs lime") { // V33
		t.Fatalf("rock without lime: %q", r.Common.Message)
	}
	if game.Refines["rock"].Part != "cement" || game.Refines["silica"].Part != "solarCell" {
		t.Fatalf("refines %+v", game.Refines)
	}
}

// V31: the cutter, a chip design: the cutter-chip contract takes components
// from the pack and unlocks it, and each of its blocks costs components
func TestCutter(t *testing.T) {
	d, ok := game.DesignOf("cutter")
	if !ok || !d.Chip || d.Check() != "" {
		t.Fatalf("cutter %+v", d)
	}
	for _, b := range d.Blocks {
		if _, ok := game.BlockCosts[b.Type]; !ok {
			t.Fatalf("no cost for a %s", b.Type)
		}
	}
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Cutter moon"}).Target
	q := []float64{0, 0, 0, 1}
	if r := st.refused("game.startShip", "k1", map[string]any{"world": world, "design": "cutter", "x": 0, "y": 110, "z": 0, "q": q}); !strings.Contains(r.Common.Message, "design chip") {
		t.Fatalf("a cutter without its chip: %q", r.Common.Message)
	}
	// refine one component in each of seven cabins at once: 4 plates, 3 wire
	loads := []string{"ferrite", "ferrite", "ferrite", "ferrite", "malachite", "malachite", "malachite"}
	for i, m := range loads {
		id := fmt.Sprintf("c%d", i)
		st.ok("game.startShip", id, map[string]any{"world": world, "design": "cabin", "x": 1600, "y": 142, "z": float64(i * 6), "q": q})
		st.ok("game.placeBlock", id, map[string]any{"index": 75})
		amount := map[string]float64{"ferrite": 0.125, "malachite": 0.1}[m]
		st.ok("game.editTerrain", world, map[string]any{"kind": "dig", "x": float64(i * 4), "y": 100, "z": 0, "radius": 1, "took": map[string]any{"dust": 0, "rock": 0, "minerals": map[string]any{m: amount}}})
		st.ok("game.smelt", id, map[string]any{"what": m})
	}
	time.Sleep(7600 * time.Millisecond)
	var last bus.Result
	for i := range loads {
		last = st.ok("game.takeIngots", fmt.Sprintf("c%d", i), map[string]any{})
	}
	var pack game.Pack
	if !st.read(game.PacksProjector, game.PackKey(world, 1), last.Seq, &pack) || pack.Parts["plate"] != 4 || pack.Parts["wire"] != 3 {
		t.Fatalf("pack %+v", pack)
	}
	// the contract, with a ship parked at the beacon
	st.ok("game.startShip", "s1", map[string]any{"world": world, "design": "skiff", "x": 0, "y": 110, "z": 0, "q": q})
	res := st.ok("game.handIn", "s1", map[string]any{"contract": "cutter-chip"})
	pack = game.Pack{} // a fresh value: unmarshalling keeps map keys the fold deleted
	if !st.read(game.PacksProjector, game.PackKey(world, 1), res.Seq, &pack) || pack.Parts["plate"] != 0 || pack.Parts["wire"] != 1 {
		t.Fatalf("pack after the contract %+v", pack)
	}
	res = st.ok("game.startShip", "k1", map[string]any{"world": world, "design": "cutter", "x": 0, "y": 110, "z": 8, "q": q})
	var k game.Ship
	if !st.read(game.ShipsProjector, game.ShipType+".k1", res.Seq, &k) || k.Blocks != len(d.Blocks) || len(k.Types) != len(d.Blocks) {
		t.Fatalf("cutter %+v", k)
	}
	for i, b := range d.Blocks {
		if k.Types[i] != b.Type {
			t.Fatalf("block %d is a %s in the blueprint graph and a %s in designs/cutter.json", i, k.Types[i], b.Type)
		}
	}
	index := func(typ string) int {
		for i, b := range d.Blocks {
			if b.Type == typ {
				return i
			}
		}
		return -1
	}
	if r := st.refused("game.placeBlock", "k1", map[string]any{"index": index("armor")}); !strings.Contains(r.Common.Message, "short of plate") {
		t.Fatalf("armour without plates: %q", r.Common.Message)
	}
	if r := st.refused("game.placeBlock", "k1", map[string]any{"index": index("battery")}); !strings.Contains(r.Common.Message, "short of cell") {
		t.Fatalf("a battery without cells: %q", r.Common.Message)
	}
	// the wire left over buys a light
	res = st.ok("game.placeBlock", "k1", map[string]any{"index": index("light")})
	pack = game.Pack{}
	if !st.read(game.PacksProjector, game.PackKey(world, 1), res.Seq, &pack) || pack.Parts["wire"] != 0 {
		t.Fatalf("a light's wire: %+v", pack)
	}
	if r := st.refused("game.placeBlock", "k1", map[string]any{"index": index("light") + 1}); !strings.Contains(r.Common.Message, "short of wire") {
		t.Fatalf("a second light: %q", r.Common.Message)
	}
	// the stock skiff's blocks stay free
	st.ok("game.placeBlock", "s1", map[string]any{"index": 1})
}

// V32: every chip design's block types in cmd-game-startShip match its file,
// and every block type it uses has a cost
func TestChipDesigns(t *testing.T) {
	raw, err := os.ReadFile("graphs/cmd-game-startShip.json")
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Nodes []struct {
			ID     string `json:"id"`
			Config struct {
				Expr string `json:"expr"`
			} `json:"config"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	expr := ""
	for _, n := range g.Nodes {
		if n.ID == "started" {
			expr = n.Config.Expr
		}
	}
	for _, id := range []string{"cutter", "hauler", "freighter", "barge", "pad"} {
		d, ok := game.DesignOf(id)
		if !ok || !d.Chip {
			t.Fatalf("%s is not a chip design", id)
		}
		want := []string{}
		for _, b := range d.Blocks {
			if _, ok := game.BlockCosts[b.Type]; !ok {
				t.Fatalf("%s: no cost for a %s", id, b.Type)
			}
			want = append(want, b.Type)
		}
		list, _ := json.Marshal(want)
		if !strings.Contains(strings.ReplaceAll(expr, " ", ""), id+":"+string(list)) {
			t.Fatalf("%s: the blueprint's types differ from designs/%s.json", id, id)
		}
	}
}

// refineParts refines components into user 1's pack in a world, one in each
// of as many cabins at once (V32: chip designs' blocks cost them)
func refineParts(t *testing.T, st *stack, world string, parts map[string]int) {
	t.Helper()
	ore := map[string]struct {
		mineral string
		amount  float64
	}{"plate": {"ferrite", 0.125}, "wire": {"malachite", 0.1}, "solarCell": {"silica", 0.25}, "nozzle": {"wolframite", 0.334}, "cell": {"spodumene", 0.25}}
	ids := []string{}
	for part, n := range parts {
		for i := 0; i < n; i++ {
			m := ore[part]
			id := fmt.Sprintf("rf%d", st.n)
			st.n++
			st.ok("game.startShip", id, map[string]any{"world": world, "design": "cabin", "x": 1600, "y": 142, "z": float64(len(ids) * 6), "q": []float64{0, 0, 0, 1}})
			st.ok("game.placeBlock", id, map[string]any{"index": 75})
			st.ok("game.editTerrain", world, map[string]any{"kind": "dig", "x": float64(len(ids)%20) * 3, "y": 100, "z": 30, "radius": 1, "took": map[string]any{"dust": 0, "rock": 0, "minerals": map[string]any{m.mineral: m.amount}}})
			st.ok("game.smelt", id, map[string]any{"what": m.mineral})
			ids = append(ids, id)
		}
	}
	deadline := time.Now().Add(30 * time.Second)
	for _, id := range ids {
		for {
			r := st.send(1, "game.takeIngots", id, map[string]any{})
			if r.Common.Code < 300 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("refining in %s: %q", id, r.Common.Message)
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
}

// V33: limestone burns into lime, cement takes lime, coal fuels the forge,
// and the landing pad's concrete costs cement
func TestLimeAndPad(t *testing.T) {
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Kiln planet"}).Target
	q := []float64{0, 0, 0, 1}
	st.ok("game.startShip", "c1", map[string]any{"world": world, "design": "cabin", "x": 1600, "y": 142, "z": 0, "q": q})
	st.ok("game.placeBlock", "c1", map[string]any{"index": 75})
	st.ok("game.editTerrain", world, map[string]any{"kind": "dig", "x": 1600, "y": 135, "z": 0, "radius": 1, "took": map[string]any{"dust": 0, "rock": 0.5, "minerals": map[string]any{"limestone": 0.2, "coal": 0.15}}})
	if r := st.refused("game.smelt", "c1", map[string]any{"what": "rock"}); !strings.Contains(r.Common.Message, "needs lime") {
		t.Fatalf("cement without lime: %q", r.Common.Message)
	}
	if r := st.refused("game.smelt", "c1", map[string]any{"what": "coal"}); !strings.Contains(r.Common.Message, "fuel") {
		t.Fatalf("coal as a load: %q", r.Common.Message)
	}
	res := st.ok("game.smelt", "c1", map[string]any{"what": "limestone"})
	var s game.Ship
	if !st.read(game.ShipsProjector, game.ShipType+".c1", res.Seq, &s) || s.Forge == nil || s.Forge.Kind != "limestone" || s.Forge.Rate != 2 {
		t.Fatalf("a fuelled kiln %+v", s.Forge)
	}
	var pack game.Pack
	if !st.read(game.PacksProjector, game.PackKey(world, 1), res.Seq, &pack) || pack.Minerals["coal"] != 0.05 || pack.Minerals["limestone"] != 0 {
		t.Fatalf("pack %+v", pack)
	}
	time.Sleep(3100 * time.Millisecond) // 0.2 m³ at 30 s a m³, twice as fast
	res = st.ok("game.takeIngots", "c1", map[string]any{})
	pack = game.Pack{}
	if !st.read(game.PacksProjector, game.PackKey(world, 1), res.Seq, &pack) || pack.Parts["lime"] != 1 {
		t.Fatalf("lime %+v", pack)
	}
	// one sack of lime makes cement of half a m³ of rock
	res = st.ok("game.smelt", "c1", map[string]any{"what": "rock"})
	s = game.Ship{}
	pack = game.Pack{}
	if !st.read(game.ShipsProjector, game.ShipType+".c1", res.Seq, &s) || s.Forge.Ore != 0.5 || s.Forge.Rate != 0 || !st.read(game.PacksProjector, game.PackKey(world, 1), res.Seq, &pack) || pack.Parts["lime"] != 0 || pack.Rock != 0 {
		t.Fatalf("cement load %+v, pack %+v", s.Forge, pack)
	}
	// the landing pad: anyone may lay it out; its concrete costs cement
	st.ok("game.startShip", "pad1", map[string]any{"world": world, "design": "pad", "x": 1610, "y": 142, "z": 0, "q": q})
	if r := st.refused("game.placeBlock", "pad1", map[string]any{"index": 0}); !strings.Contains(r.Common.Message, "short of cement") {
		t.Fatalf("concrete without cement: %q", r.Common.Message)
	}
}
