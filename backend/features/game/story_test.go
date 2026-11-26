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
	// S7: a herb gathered counts once per spot; an unknown herb is refused
	st.refused("game.reachStory", farm, map[string]any{"item": "mandrake", "spot": "s1", "area": farm, "x": 3, "z": 3})
	st.ok("game.reachStory", farm, map[string]any{"item": "thyme", "spot": "s1", "area": farm, "x": 3, "z": 3})
	st.ok("game.reachStory", farm, map[string]any{"item": "thyme", "spot": "s1", "area": farm, "x": 3, "z": 3})
	res = st.ok("game.reachStory", farm, map[string]any{"step": "forage", "item": "nettle", "spot": "s2", "area": farm, "x": 4, "z": 3})
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Items["thyme"] != 1 || p.Items["nettle"] != 1 || !p.Done["forage"] {
		t.Fatalf("items %+v", p)
	}
	// S8: cooking spends a herb held, never one not held; the body is saved, clamped
	st.refused("game.reachStory", farm, map[string]any{"use": "yarrow", "area": farm, "x": 3, "z": 3})
	res = st.ok("game.reachStory", farm, map[string]any{"step": "cook", "use": "thyme", "area": farm, "x": 3, "z": 3, "body": map[string]any{"hum": 2.4, "chill": 140, "hunger": 12}})
	p = game.StoryPlace{}
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Items["thyme"] != 0 || p.Body == nil || *p.Body != (game.StoryBody{Hum: 2, Chill: 100, Hunger: 12}) || !p.Done["cook"] {
		t.Fatalf("cooked %+v %+v", p, p.Body)
	}
	st.refused("game.reachStory", farm, map[string]any{"use": "thyme", "area": farm, "x": 3, "z": 3})
	// S9: the clock is saved, its hour kept within a day
	res = st.ok("game.reachStory", farm, map[string]any{"step": "almanac", "area": farm, "x": 3, "z": 3, "clock": map[string]any{"day": 2, "hour": 26.37}})
	p = game.StoryPlace{}
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Clock == nil || *p.Clock != (game.StoryClock{Day: 2, Hour: 23.9}) || !p.Done["almanac"] {
		t.Fatalf("clock %+v", p.Clock)
	}
	// S10: the reading's answer is kept; the ditch pays one silver, once
	st.refused("game.reachStory", farm, map[string]any{"step": "reading", "told": "lies", "area": farm, "x": 3, "z": 3})
	st.ok("game.reachStory", farm, map[string]any{"step": "reading", "told": "ditch", "area": farm, "x": 3, "z": 3})
	st.ok("game.reachStory", farm, map[string]any{"step": "ditch", "area": farm, "x": 3, "z": 3})
	res = st.ok("game.reachStory", farm, map[string]any{"step": "ditch", "area": farm, "x": 3, "z": 3})
	p = game.StoryPlace{}
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Told != "ditch" || p.Items["silver"] != 1 || !p.Done["ditch"] {
		t.Fatalf("reading %+v", p)
	}
	// S13: the conclusion drawn in the notes is a step
	res = st.ok("game.reachStory", farm, map[string]any{"step": "foretold", "area": farm, "x": 3, "z": 3})
	p = game.StoryPlace{}
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || !p.Done["foretold"] || p.Last != "foretold" {
		t.Fatalf("foretold %+v", p)
	}
}
