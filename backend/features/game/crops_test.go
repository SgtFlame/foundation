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
