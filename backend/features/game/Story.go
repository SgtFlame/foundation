package game

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// The story game's saves (forge 375 S6): a player's place in a story, the
// first hour from the Sheaflands farmstead to Hawk's Rest. A story is
// known by its starting area. The cmd-game-reachStory blueprint emits
// reached {step, area, x, z} on the story's target (story.<areaId>);
// game.stories folds it per player, keyed <areaId>.<userId>, and the
// game.story query gives it back, so Play the first hour resumes where the
// player stopped. The steps themselves are played in the browser
// (frontend/engine/src/story); the server keeps which were reached.

const (
	// StoryType is a story's target type on the game area, with its starting area's id as its id.
	StoryType = "story"
	// StoryLogProjector keeps a count per story, keyed story.<areaId>: the command's own state.
	StoryLogProjector = "game.storyLog"
	// StoriesProjector keeps each player's place in each story, keyed <areaId>.<userId>.
	StoriesProjector = "game.stories"
	// EventReached is a step of a story reached, or only the player's place: {step, area, x, z}.
	EventReached = "reached"
)

// StorySteps are the first hour's steps in order (375 › the first hour).
// S7 adds the first day: the morning's card, foraging and the herbal.
var StorySteps = []string{"arrive", "farmer", "howl", "signature", "trail", "fight", "road", "rest", "morning", "forage", "herbal", "stillness", "cook", "almanac", "sleep", "reading", "ditch", "foretold", "watch", "defended", "thanked", "dream", "companion", "stones", "cache", "bundles", "truedream", "ward", "tried"}

// S10: the step that pays for Wenlan's reading, and the coin it pays
const (
	StepDitch = "ditch"
	Silver    = "silver"
	// S15: Wenlan's thanks after the foretold night, and the knot of the
	// last sheaf he gives, which wards off the Chill
	StepThanked = "thanked"
	Knot        = "knot"
	// S16: the dream at the ford, and the wound it leaves until a warm
	// night (spent with use: wound)
	StepDream = "dream"
	Wound     = "wound"
	// S18: the Nine Sisters remember at a lesser full moon, and the star
	// lore they give, which makes card readings clearer
	StepStones = "stones"
	Starlore   = "starlore"
	// S19: the hedge-witch's cache at Hawk's Rest, and the hedge-craft
	// notes in it, which teach the yarrow poultice
	StepCache  = "cache"
	Hedgecraft = "hedgecraft"
	// S21: the true dream the dream tincture brings, and the threshold
	// ward's lore it leaves
	StepTrueDream = "truedream"
	Wardlore      = "wardlore"
	// S22: the threshold ward laid at Hawk's Rest: it spends a sage and the
	// river stones, and the ward stands from then on
	StepWard    = "ward"
	RiverStones = "riverstones"
	Ward        = "ward"
	Sage        = "sage"
	// S23: the night the ward is tried: it holds the door, and one comes up
	// through the hearthstone instead; the hearth lore it leaves (a
	// hearthstone is a threshold too)
	StepTried  = "tried"
	Hearthlore = "hearthlore"
)

// StoryItems are what a player may gather in a story (S7: herbs for the herbal;
// S20: the hedge-witch's dried bundles at Hawk's Rest).
var StoryItems = []string{"thyme", "nettle", "yarrow", "lavender", "mugwort", "valerian", "sage", "riverstones"}

// StoryPlace is a player's place in one story.
type StoryPlace struct {
	Done map[string]bool `json:"done"`           // the steps reached
	Last string          `json:"last,omitempty"` // the last step reached
	Area string          `json:"area,omitempty"` // the area they were last in
	At   *[2]float64     `json:"at,omitempty"`   // where they stood in it, x and z in metres
	// S7: what they gathered, by item, and where (so a herb is picked once)
	Items  map[string]int  `json:"items,omitempty"`
	Picked map[string]bool `json:"picked,omitempty"`
	// S8: the character's Hum, Chill and Hunger when last saved
	Body *StoryBody `json:"body,omitempty"`
	// S9: the story's clock when last saved
	Clock *StoryClock `json:"clock,omitempty"`
	// S10: what the player told Wenlan after his reading: all or ditch
	Told string `json:"told,omitempty"`
}

// StoryClock is the story's day (since the night of the first snow) and hour (S9).
type StoryClock struct {
	Day  int     `json:"day"`
	Hour float64 `json:"hour"`
}

// StoryBody is the story character's Hum (the pool the sky sets), the
// Chill spending it leaves, and Hunger (S8, the design's rules 2 and 3).
type StoryBody struct {
	Hum    float64 `json:"hum"`
	Chill  float64 `json:"chill"`
	Hunger float64 `json:"hunger"`
}

func foldStory(f live.Fold) (json.RawMessage, error) {
	p := StoryPlace{}
	if f.State != nil {
		if err := json.Unmarshal(f.State, &p); err != nil {
			return nil, err
		}
	}
	var d struct {
		Step  string      `json:"step"`
		Item  string      `json:"item"`
		Spot  string      `json:"spot"`
		Use   string      `json:"use"`
		Body  *StoryBody  `json:"body"`
		Clock *StoryClock `json:"clock"`
		Told  string      `json:"told"`
		Area  string      `json:"area"`
		X     float64     `json:"x"`
		Z     float64     `json:"z"`
	}
	if err := json.Unmarshal(f.Event.Data, &d); err != nil {
		return nil, err
	}
	if p.Done == nil {
		p.Done = map[string]bool{}
	}
	if d.Step == StepDitch && !p.Done[StepDitch] {
		// S10: Wenlan pays a silver for the reading once the ditch runs
		if p.Items == nil {
			p.Items = map[string]int{}
		}
		p.Items[Silver]++
	}
	if d.Step == StepThanked && !p.Done[StepThanked] {
		if p.Items == nil {
			p.Items = map[string]int{}
		}
		p.Items[Knot] = 1
	}
	if d.Step == StepDream && !p.Done[StepDream] {
		if p.Items == nil {
			p.Items = map[string]int{}
		}
		p.Items[Wound] = 1
	}
	if d.Step == StepStones && !p.Done[StepStones] {
		if p.Items == nil {
			p.Items = map[string]int{}
		}
		p.Items[Starlore] = 1
	}
	if d.Step == StepCache && !p.Done[StepCache] {
		if p.Items == nil {
			p.Items = map[string]int{}
		}
		p.Items[Hedgecraft] = 1
	}
	if d.Step == StepTrueDream && !p.Done[StepTrueDream] {
		if p.Items == nil {
			p.Items = map[string]int{}
		}
		p.Items[Wardlore] = 1
	}
	if d.Step == StepWard && !p.Done[StepWard] {
		if p.Items == nil {
			p.Items = map[string]int{}
		}
		if p.Items[Sage] > 0 {
			p.Items[Sage]--
		}
		p.Items[RiverStones] = 0
		p.Items[Ward] = 1
	}
	if d.Step == StepTried && !p.Done[StepTried] {
		if p.Items == nil {
			p.Items = map[string]int{}
		}
		p.Items[Hearthlore] = 1
	}
	if d.Told != "" {
		p.Told = d.Told
	}
	if d.Step != "" {
		p.Done[d.Step] = true
		p.Last = d.Step
	}
	if d.Item != "" {
		if p.Items == nil {
			p.Items = map[string]int{}
			p.Picked = map[string]bool{}
		}
		if !p.Picked[d.Spot] {
			p.Items[d.Item]++
			p.Picked[d.Spot] = true
		}
	}
	if d.Use != "" && p.Items[d.Use] > 0 {
		p.Items[d.Use]--
	}
	if d.Body != nil {
		p.Body = d.Body
	}
	if d.Clock != nil {
		p.Clock = d.Clock
	}
	p.Area = d.Area
	p.At = &[2]float64{d.X, d.Z}
	return json.Marshal(p)
}

func contributeStories(reg *extensions.Registry) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: StoryLogProjector, Source: source}, Title: "Story saves in brief",
		Description: "How many places were saved in each story, keyed story.<areaId>: {saves}",
		Area:        Area, Types: []string{StoryType},
		Fold: func(f live.Fold) (json.RawMessage, error) {
			var s struct {
				Saves int `json:"saves"`
			}
			if f.State != nil {
				if err := json.Unmarshal(f.State, &s); err != nil {
					return nil, err
				}
			}
			s.Saves++
			return json.Marshal(s)
		},
	})
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: StoriesProjector, Source: source}, Title: "Players' places in stories",
		Description: "Each player's place in each story, keyed <areaId>.<userId>: {done: {<step>: true}, last, area, at: [x, z], items: {<item>: n}, picked: {<spot>: true}, body: {hum, chill, hunger}, clock: {day, hour}, told}, folded from the steps they reach and the places they save",
		Area:        Area, Types: []string{StoryType},
		KeyOf: func(e live.Event) string {
			if e.UserID == 0 || e.Type != EventReached {
				return ""
			}
			return PackKey(e.Target.ID, e.UserID)
		},
		Fold: foldStory,
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "game.story", Source: source}, Title: "My place in a story",
		Description: fmt.Sprintf("The caller's place in a story known by its starting area: {done: {<step>: true}, last (the last step reached), area (the area they were last in), at: [x, z] (where they stood in it), items: {<herb>: n} and picked: {<spot>: true} (what they gathered, S7, less what they cooked), body: {hum, chill, hunger} (S8), clock: {day, hour} (S9), told (S10: what the player told Wenlan after his reading; the ditch pays items.silver; thanked gives items.knot, S15; dream gives items.wound, healed with use: wound, S16; stones gives items.starlore, S18; cache items.hedgecraft, S19; truedream items.wardlore, S21; ward spends a sage and items.riverstones and gives items.ward, S22; tried gives items.hearthlore, S23)}, then each change as they play. The first hour's steps are %v.", StorySteps),
		Params:      json.RawMessage(`{"type":"object","properties":{"story":{"type":"string","description":"The story's starting area's id (the farmstead)"}},"required":["story"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"story":"a123"}`)},
		Action:      "game.read",
		Resource:    func(p features.Principal, _ json.RawMessage) string { return Resource(p) },
		Run: live.ValueOf(StoriesProjector, func(f live.Feed) (string, error) {
			var p struct {
				Story string `json:"story"`
			}
			if err := json.Unmarshal(f.Params(), &p); err != nil || !live.ValidToken(p.Story) {
				return "", fmt.Errorf("game.story needs a story's starting area id")
			}
			return PackKey(p.Story, f.Args().Principal.UserID), nil
		}, json.RawMessage(`{"done":{}}`)),
	})
}
