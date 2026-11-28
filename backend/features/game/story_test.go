package game_test

import (
	"strings"
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
	// S14: the foretold night kept, and the house held
	st.ok("game.reachStory", farm, map[string]any{"step": "watch", "area": farm, "x": 3, "z": 3})
	res = st.ok("game.reachStory", farm, map[string]any{"step": "defended", "area": farm, "x": 3, "z": 3})
	p = game.StoryPlace{}
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || !p.Done["watch"] || p.Last != "defended" {
		t.Fatalf("defended %+v", p)
	}
	// S15: Wenlan's thanks give the knot of the last sheaf, once
	st.ok("game.reachStory", farm, map[string]any{"step": "thanked", "area": farm, "x": 3, "z": 3})
	res = st.ok("game.reachStory", farm, map[string]any{"step": "thanked", "area": farm, "x": 3, "z": 3})
	p = game.StoryPlace{}
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "thanked" || p.Items["knot"] != 1 {
		t.Fatalf("thanked %+v", p)
	}
	// S16: the dream leaves a wound, which a warm night spends
	res = st.ok("game.reachStory", farm, map[string]any{"step": "dream", "area": farm, "x": 3, "z": 3})
	p = game.StoryPlace{}
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Items["wound"] != 1 {
		t.Fatalf("dream %+v", p)
	}
	res = st.ok("game.reachStory", farm, map[string]any{"step": "", "use": "wound", "area": farm, "x": 3, "z": 3})
	p = game.StoryPlace{}
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Items["wound"] != 0 || !p.Done["dream"] {
		t.Fatalf("healed %+v", p)
	}
	if r := st.refused("game.reachStory", farm, map[string]any{"step": "", "use": "wound", "area": farm, "x": 3, "z": 3}); !strings.Contains(r.Common.Message, "no wound") {
		t.Fatalf("healed twice: %q", r.Common.Message)
	}
	// S17: the first companion joins
	res = st.ok("game.reachStory", farm, map[string]any{"step": "companion", "area": farm, "x": 3, "z": 3})
	p = game.StoryPlace{}
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "companion" {
		t.Fatalf("companion %+v", p)
	}
	// S18: the Nine Sisters remember, and give star lore
	res = st.ok("game.reachStory", farm, map[string]any{"step": "stones", "area": farm, "x": 12, "z": 4})
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "stones" || p.Items["starlore"] != 1 {
		t.Fatalf("stones %+v", p)
	}
	// S19: the hedge-witch's cache teaches the yarrow poultice
	res = st.ok("game.reachStory", farm, map[string]any{"step": "cache", "area": farm, "x": 9, "z": 3})
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "cache" || p.Items["hedgecraft"] != 1 {
		t.Fatalf("cache %+v", p)
	}
	// S20: her dried bundles are gathered like herbs, then cooked
	st.ok("game.reachStory", farm, map[string]any{"item": "valerian", "spot": "bundle-valerian", "area": farm, "x": 9, "z": 3})
	res = st.ok("game.reachStory", farm, map[string]any{"step": "bundles", "area": farm, "x": 9, "z": 3})
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "bundles" || p.Items["valerian"] != 1 || !p.Picked["bundle-valerian"] {
		t.Fatalf("bundles %+v", p)
	}
	res = st.ok("game.reachStory", farm, map[string]any{"step": "cook", "use": "valerian", "area": farm, "x": 9, "z": 3})
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Items["valerian"] != 0 {
		t.Fatalf("valerian tea %+v", p)
	}
	if r := st.refused("game.reachStory", farm, map[string]any{"item": "hemlock", "area": farm, "x": 9, "z": 3}); !strings.Contains(r.Common.Message, "no herb") {
		t.Fatalf("hemlock: %q", r.Common.Message)
	}
	// S21: the true dream after the dream tincture leaves the ward's lore
	res = st.ok("game.reachStory", farm, map[string]any{"step": "truedream", "area": farm, "x": 9, "z": 3})
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "truedream" || p.Items["wardlore"] != 1 {
		t.Fatalf("true dream %+v", p)
	}
	// S22: the river stones and a sage lay the ward
	st.ok("game.reachStory", farm, map[string]any{"item": "riverstones", "spot": "culvert-stones", "area": farm, "x": 9, "z": 3})
	st.ok("game.reachStory", farm, map[string]any{"item": "sage", "spot": "bundle-sage", "area": farm, "x": 9, "z": 3})
	res = st.ok("game.reachStory", farm, map[string]any{"step": "ward", "area": farm, "x": 9, "z": 3})
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "ward" || p.Items["ward"] != 1 || p.Items["riverstones"] != 0 || p.Items["sage"] != 0 {
		t.Fatalf("ward %+v", p)
	}
	// S23: the night the ward is tried leaves the hearth lore
	res = st.ok("game.reachStory", farm, map[string]any{"step": "tried", "area": farm, "x": 9, "z": 3})
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "tried" || p.Items["hearthlore"] != 1 || p.Items["ward"] != 1 {
		t.Fatalf("tried %+v", p)
	}
	// S24: stones from the culvert again, a sage from the ditch bank, and the hearth ward
	st.ok("game.reachStory", farm, map[string]any{"area": farm, "x": 9, "z": 3, "item": "riverstones", "spot": "culvert-stones-2"})
	st.ok("game.reachStory", farm, map[string]any{"area": farm, "x": 9, "z": 3, "item": "sage", "spot": "ditch-sage"})
	res = st.ok("game.reachStory", farm, map[string]any{"step": "hearthward", "area": farm, "x": 9, "z": 3})
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "hearthward" || p.Items["hearthward"] != 1 || p.Items["riverstones"] != 0 || p.Items["sage"] != 0 {
		t.Fatalf("hearthward %+v", p)
	}
	// S25: a third handful of stones and sage for Wenlan's hearth
	st.ok("game.reachStory", farm, map[string]any{"area": farm, "x": 9, "z": 3, "item": "riverstones", "spot": "culvert-stones-3"})
	st.ok("game.reachStory", farm, map[string]any{"area": farm, "x": 9, "z": 3, "item": "sage", "spot": "ditch-sage-2"})
	res = st.ok("game.reachStory", farm, map[string]any{"step": "farmward", "area": farm, "x": 9, "z": 3})
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "farmward" || p.Items["farmward"] != 1 || p.Items["riverstones"] != 0 || p.Items["sage"] != 0 {
		t.Fatalf("farmward %+v", p)
	}
	// S26: the well, where they come up now
	res = st.ok("game.reachStory", farm, map[string]any{"step": "well", "area": farm, "x": 9, "z": 3})
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "well" || p.Items["welllore"] != 1 {
		t.Fatalf("well %+v", p)
	}
	// S27: a fourth handful, and the well warded
	st.ok("game.reachStory", farm, map[string]any{"area": farm, "x": 9, "z": 3, "item": "riverstones", "spot": "culvert-stones-4"})
	st.ok("game.reachStory", farm, map[string]any{"area": farm, "x": 9, "z": 3, "item": "sage", "spot": "ditch-sage-3"})
	res = st.ok("game.reachStory", farm, map[string]any{"step": "wellward", "area": farm, "x": 9, "z": 3})
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "wellward" || p.Items["wellward"] != 1 || p.Items["riverstones"] != 0 || p.Items["sage"] != 0 {
		t.Fatalf("wellward %+v", p)
	}
	// S28: the frost track to the signpost; S29: Ashby's remnants, in the area the road leads to
	st.ok("game.reachStory", farm, map[string]any{"step": "track", "area": farm, "x": 9, "z": 3})
	res = st.ok("game.reachStory", farm, map[string]any{"step": "ashby", "area": farm, "x": 9, "z": 3})
	p = game.StoryPlace{}
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "ashby" || p.Items["ashbylore"] != 1 || !p.Done["track"] {
		t.Fatalf("ashby %+v", p)
	}
	// S30: word of Ashby, told to Wenlan
	res = st.ok("game.reachStory", farm, map[string]any{"step": "told", "area": farm, "x": 9, "z": 3})
	p = game.StoryPlace{}
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "told" {
		t.Fatalf("told %+v", p)
	}
	// S31: the hearth-watch meets at Hawk's Rest
	res = st.ok("game.reachStory", farm, map[string]any{"step": "council", "area": farm, "x": 9, "z": 3})
	p = game.StoryPlace{}
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "council" {
		t.Fatalf("council %+v", p)
	}
	// S32: the north road past Ashby, to the ford
	res = st.ok("game.reachStory", farm, map[string]any{"step": "northroad", "area": farm, "x": 9, "z": 3})
	p = game.StoryPlace{}
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "northroad" {
		t.Fatalf("northroad %+v", p)
	}
	// S33: the reeve hears there are two villages
	res = st.ok("game.reachStory", farm, map[string]any{"step": "reeve", "area": farm, "x": 9, "z": 3})
	p = game.StoryPlace{}
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "reeve" {
		t.Fatalf("reeve %+v", p)
	}
	// S34: the road to Aldermere
	res = st.ok("game.reachStory", farm, map[string]any{"step": "aldermere", "area": farm, "x": 9, "z": 3})
	p = game.StoryPlace{}
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "aldermere" {
		t.Fatalf("aldermere %+v", p)
	}
	// S35: the letter is read
	res = st.ok("game.reachStory", farm, map[string]any{"step": "steward", "area": farm, "x": 9, "z": 3})
	p = game.StoryPlace{}
	if !st.read(game.StoriesProjector, game.PackKey(farm, 1), res.Seq, &p) || p.Last != "steward" {
		t.Fatalf("steward %+v", p)
	}
}
