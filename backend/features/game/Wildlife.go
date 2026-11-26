package game

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Wildlife (forge 360 V12): fish caught from the shore of the planet's sea
// and grazers hunted on its land. game.forage (cmd-game-forage) emits
// foraged on the world's terrain target with the food it gives as a pack
// change; game.wildlife keeps, per world, when each grazer was last taken
// (it comes back after RespawnSeconds) and how many fish and grazers were
// taken. The herd itself is the engine's (wildlife/Wildlife.ts herdOf):
// twelve grazers g00 to g23 placed from the world's seed.

const (
	// WildlifeProjector keeps each world's wildlife taken, keyed terrain.<worldId>.
	WildlifeProjector = "game.wildlife"
	// EventForaged is a fish caught or a grazer hunted: {kind, animal, pack: {food}}.
	EventForaged = "foraged"
	// RespawnSeconds is how long a grazer taken stays gone.
	RespawnSeconds = 300
)

// Wildlife is what a world's players took from its planet.
type Wildlife struct {
	Taken  map[string]int64 `json:"taken"` // grazer id → when, Unix ms
	Fish   int              `json:"fish"`
	Hunted int              `json:"hunted"`
}

func foldWildlife(f live.Fold) (json.RawMessage, error) {
	w := Wildlife{}
	if f.State != nil {
		if err := json.Unmarshal(f.State, &w); err != nil {
			return nil, err
		}
	}
	if w.Taken == nil {
		w.Taken = map[string]int64{}
	}
	if f.Event.Type != EventForaged {
		// stored with every event on the target, so a command's expected sequence is the target's
		return json.Marshal(w)
	}
	var d struct {
		Kind   string `json:"kind"`
		Animal string `json:"animal"`
	}
	if err := json.Unmarshal(f.Event.Data, &d); err != nil {
		return nil, err
	}
	switch d.Kind {
	case "fish":
		w.Fish++
	case "hunt":
		w.Hunted++
		w.Taken[d.Animal] = f.Event.Time.UnixMilli()
	}
	return json.Marshal(w)
}

func contributeWildlife(reg *extensions.Registry) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: WildlifeProjector, Source: source}, Title: "Wildlife taken",
		Description: "What each world's players took from the planet with air, keyed terrain.<worldId>: {taken: {<grazer>: when, Unix ms}, fish, hunted}",
		Area:        Area, Types: []string{TerrainType},
		Fold: foldWildlife,
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "game.wildlife", Source: source}, Title: "Wildlife",
		Description: fmt.Sprintf("A world's wildlife taken: {taken: {<grazer>: when, Unix ms}, fish, hunted}, then each change. A grazer taken comes back %d s later; fish and hunt with game.forage.", RespawnSeconds),
		Params:      json.RawMessage(`{"type":"object","properties":{"world":{"type":"string","description":"The world's id"}},"required":["world"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"world":"w123"}`)},
		Action:      "game.read",
		Resource:    func(p features.Principal, _ json.RawMessage) string { return Resource(p) },
		Run: live.ValueOf(WildlifeProjector, func(f live.Feed) (string, error) {
			var p struct {
				World string `json:"world"`
			}
			if err := json.Unmarshal(f.Params(), &p); err != nil || !live.ValidToken(p.World) {
				return "", fmt.Errorf("game.wildlife needs a world's id")
			}
			return TerrainType + "." + p.World, nil
		}, json.RawMessage(`{"taken":{},"fish":0,"hunted":0}`)),
	})
}
