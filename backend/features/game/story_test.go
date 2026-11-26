package game_test

import (
	"testing"

	"github.com/SgtFlame/foundation/backend/features/game"
)

// S6: a player's place in the first hour is saved step by step and comes back
func TestStorySaves(t *testing.T) {
	st := newStack(t)
	farm := st.ok("game.createArea", "", map[string]any{"name": "Sheaflands farmstead"}).Target
	rest := st.ok("game.createArea", "", map[string]any{"name": "Hawk's Rest", "pieceSet": "kitchen", "width": 5, "depth": 4}).Target
	st.refused("game.reachStory", "nowhere", map[string]any{"step": "arrive", "area": farm, "x": 5, "z": 5})
	st.refused("game.reachStory", farm, map[string]any{"step": "dance", "area": farm, "x": 5, "z": 5})
	st.refused("game.reachStory", farm, map[string]any{"step": "arrive", "area": "gone", "x": 5, "z": 5})
	st.refused("game.reachStory", farm, map[string]any{"step": "arrive", "area": farm, "x": -4, "z": 5})
	st.ok("game.reachStory", farm, map[string]any{"step": "arrive", "area": farm, "x": 55.5, "z": 45})
	st.ok("game.reachStory", farm, map[string]any{"step": "farmer", "area": farm, "x": 56, "z": 44})
	res := st.ok("game.reachStory", farm, map[string]any{"step": "road", "area": rest, "x": 4.555, "z": 6})
	var p game.StoryPlace
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || !p.Done["farmer"] || p.Last != "road" || p.Area != rest || p.At == nil || p.At[0] != 4.56 {
		t.Fatalf("place %+v", p)
	}
	// a place saved alone keeps the steps
	res = st.ok("game.reachStory", farm, map[string]any{"area": rest, "x": 3, "z": 3})
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "road" || len(p.Done) != 3 || p.At[0] != 3 {
		t.Fatalf("place saved alone %+v", p)
	}
}
