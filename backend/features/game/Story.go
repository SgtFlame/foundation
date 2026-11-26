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
var StorySteps = []string{"arrive", "farmer", "howl", "signature", "trail", "fight", "road", "rest"}

// StoryPlace is a player's place in one story.
type StoryPlace struct {
	Done map[string]bool `json:"done"`           // the steps reached
	Last string          `json:"last,omitempty"` // the last step reached
	Area string          `json:"area,omitempty"` // the area they were last in
	At   *[2]float64     `json:"at,omitempty"`   // where they stood in it, x and z in metres
}

func foldStory(f live.Fold) (json.RawMessage, error) {
	p := StoryPlace{}
	if f.State != nil {
		if err := json.Unmarshal(f.State, &p); err != nil {
			return nil, err
		}
	}
	var d struct {
		Step string  `json:"step"`
		Area string  `json:"area"`
		X    float64 `json:"x"`
		Z    float64 `json:"z"`
	}
	if err := json.Unmarshal(f.Event.Data, &d); err != nil {
		return nil, err
	}
	if p.Done == nil {
		p.Done = map[string]bool{}
	}
	if d.Step != "" {
		p.Done[d.Step] = true
		p.Last = d.Step
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
		Description: "Each player's place in each story, keyed <areaId>.<userId>: {done: {<step>: true}, last, area, at: [x, z]}, folded from the steps they reach and the places they save",
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
		Description: fmt.Sprintf("The caller's place in a story known by its starting area: {done: {<step>: true}, last (the last step reached), area (the area they were last in), at: [x, z] (where they stood in it)}, then each change as they play. The first hour's steps are %v.", StorySteps),
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
