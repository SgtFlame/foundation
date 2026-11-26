package game_test

import (
	"strings"
	"testing"

	"github.com/SgtFlame/foundation/backend/features/game"
)

// V9: crops are planted only in the planet's soil, ripen, and are harvested as food
func TestCrops(t *testing.T) {
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Garden moon"}).Target
	st.refused("game.plant", world, map[string]any{"x": 0, "y": 103, "z": 0}) // the moon's dust
	res := st.ok("game.plant", world, map[string]any{"x": 1600, "y": 141, "z": 0})
	if r := st.refused("game.plant", world, map[string]any{"x": 1600.5, "y": 141, "z": 0.5}); !strings.Contains(r.Common.Message, "1.5 m") {
		t.Fatalf("crowded: %q", r.Common.Message)
	}
	res = st.ok("game.plant", world, map[string]any{"x": 1603, "y": 141, "z": 0})
	var c game.Crops
	if !st.read(game.CropsProjector, game.TerrainType+"."+world, res.Seq, &c) || len(c.Crops) != 2 || c.Next != 2 {
		t.Fatalf("crops %+v", c)
	}
	if _, ok := c.Crops["c1"]; !ok {
		t.Fatalf("ids %+v", c.Crops)
	}
	if r := st.refused("game.harvest", world, map[string]any{"crop": "c0"}); !strings.Contains(r.Common.Message, "not ripe") {
		t.Fatalf("unripe: %q", r.Common.Message)
	}
	st.refused("game.harvest", world, map[string]any{"crop": "c9"})
	// standing and starting a ship by the planet are allowed
	st.ok("game.standAt", world, map[string]any{"x": 1600, "y": 142, "z": 0})
}

// V10: a ration is eaten from the pack, and the body's needs are saved with where the player stands
func TestNeeds(t *testing.T) {
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Hungry moon"}).Target
	if r := st.refused("game.eat", world, map[string]any{}); !strings.Contains(r.Common.Message, "no food") {
		t.Fatalf("empty pack: %q", r.Common.Message)
	}
	res := st.ok("game.standAt", world, map[string]any{"x": 0, "y": 101, "z": 0, "needs": map[string]any{"air": 41.6, "fed": 130, "health": -3}})
	var p game.Player
	if !st.read(game.PlayersProjector, game.PackKey(world, 1), res.Seq, &p) || p.Needs == nil || *p.Needs != (game.Needs{Air: 42, Fed: 100, Health: 0}) {
		t.Fatalf("needs %+v", p.Needs)
	}
	// a stand without needs keeps them
	res = st.ok("game.standAt", world, map[string]any{"x": 0, "y": 102, "z": 0})
	p = game.Player{}
	if !st.read(game.PlayersProjector, game.PackKey(world, 1), res.Seq, &p) || p.Needs == nil || p.Needs.Air != 42 {
		t.Fatalf("kept %+v", p.Needs)
	}
}

// V12: fish from the planet's shore, grazers hunted and gone a while, food in the pack
func TestForage(t *testing.T) {
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Wild moon"}).Target
	st.refused("game.forage", world, map[string]any{"kind": "fish", "x": 0, "y": 101, "z": 0})
	// another event on the terrain first: the wildlife's expected sequence must follow it
	planted := st.ok("game.plant", world, map[string]any{"x": 1600, "y": 141, "z": 0})
	var cr game.Crops
	st.read(game.CropsProjector, game.TerrainType+"."+world, planted.Seq, &cr)
	var w0 game.Wildlife
	st.read(game.WildlifeProjector, game.TerrainType+"."+world, planted.Seq, &w0)
	st.ok("game.forage", world, map[string]any{"kind": "fish", "x": 1600, "y": 139, "z": 0})
	res := st.ok("game.forage", world, map[string]any{"kind": "hunt", "animal": "g12", "x": 1600, "y": 141, "z": 0})
	if r := st.refused("game.forage", world, map[string]any{"kind": "hunt", "animal": "g12", "x": 1600, "y": 141, "z": 0}); !strings.Contains(r.Common.Message, "taken") {
		t.Fatalf("taken twice: %q", r.Common.Message)
	}
	st.refused("game.forage", world, map[string]any{"kind": "hunt", "animal": "g99", "x": 1600, "y": 141, "z": 0})
	var w game.Wildlife
	if !st.read(game.WildlifeProjector, game.TerrainType+"."+world, res.Seq, &w) || w.Fish != 1 || w.Hunted != 1 || w.Taken["g12"] == 0 {
		t.Fatalf("wildlife %+v", w)
	}
	var pk game.Pack
	if !st.read(game.PacksProjector, game.PackKey(world, 1), res.Seq, &pk) || pk.Food != 4 {
		t.Fatalf("pack %+v", pk)
	}
}

// V15: a wreck's block salvaged gives scrap as ore, once, for everyone
func TestSalvage(t *testing.T) {
	st := newStack(t)
	world := st.ok("game.createWorld", "", map[string]any{"name": "Wreck moon"}).Target
	at := map[string]any{"x": 40, "y": 90, "z": 0}
	with := func(m map[string]any) map[string]any {
		for k, v := range at {
			m[k] = v
		}
		return m
	}
	if r := st.refused("game.salvage", world, map[string]any{"wreck": "k0", "index": 3, "x": 1600, "y": 140, "z": 0}); !strings.Contains(r.Common.Message, "moon") {
		t.Fatalf("salvage on the planet: %q", r.Common.Message)
	}
	if r := st.refused("game.salvage", world, with(map[string]any{"wreck": "k9", "index": 3})); !strings.Contains(r.Common.Message, "no wreck") {
		t.Fatalf("unknown wreck: %q", r.Common.Message)
	}
	if r := st.refused("game.salvage", world, with(map[string]any{"wreck": "k0", "index": 27})); !strings.Contains(r.Common.Message, "0 to 26") {
		t.Fatalf("unknown block: %q", r.Common.Message)
	}
	res := st.ok("game.salvage", world, with(map[string]any{"wreck": "k0", "index": 3}))
	var w game.Wrecks
	if !st.read(game.WrecksProjector, game.TerrainType+"."+world, res.Seq, &w) || !w.Salvaged["k0.3"] || w.Count != 1 {
		t.Fatalf("wrecks %+v", w)
	}
	var pack game.Pack
	if !st.read(game.PacksProjector, game.PackKey(world, 1), res.Seq, &pack) || pack.Ore != game.SalvageOre {
		t.Fatalf("pack %+v", pack)
	}
	if r := st.refused("game.salvage", world, with(map[string]any{"wreck": "k0", "index": 3})); !strings.Contains(r.Common.Message, "already") {
		t.Fatalf("salvaged twice: %q", r.Common.Message)
	}
	st.ok("game.salvage", world, with(map[string]any{"wreck": "k1", "index": 3}))
}
