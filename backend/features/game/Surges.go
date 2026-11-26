package game

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Swarm surges (forge 360 V16; the play design's base defence): every
// SurgePeriodMs of real time drones come down out of the moon's sky for a
// while (the engine's surge/Surge.ts keeps the schedule and the drones,
// eight a surge, from the surge's index). game.downDrone (cmd-game-downDrone)
// pays a little scrap for a drone shot down, once per player, drone and
// surge, while the surge is the current or the last one; game.surges keeps
// which were downed by whom.

const (
	// SurgesProjector keeps each world's downed drones, keyed terrain.<worldId>.
	SurgesProjector = "game.surges"
	// EventDowned is a surge's drone shot down: {surge, drone, pack: {ore}}.
	EventDowned = "downed"
	// EventTurretPlaced is the sender's sentry turret set down (V17): {x, y, z}.
	EventTurretPlaced = "turretPlaced"
	// SurgePeriodMs is the time between surges (the engine's SURGE_PERIOD).
	SurgePeriodMs = 12 * 60_000
	// DroneScrap is the ore a downed drone gives, in m³.
	DroneScrap = 0.1
)

// Surges is what a world's players shot down.
type Surges struct {
	Downed  map[string]bool   `json:"downed"` // <surge>.<drone>.<userId>
	Count   int               `json:"count"`
	Turrets map[string]Turret `json:"turrets,omitempty"` // by user id (V17)
}

// Turret is a player's sentry turret, set down on the moon (V17).
type Turret struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

func foldSurges(f live.Fold) (json.RawMessage, error) {
	s := Surges{}
	if f.State != nil {
		if err := json.Unmarshal(f.State, &s); err != nil {
			return nil, err
		}
	}
	if s.Downed == nil {
		s.Downed = map[string]bool{}
	}
	if f.Event.Type == EventTurretPlaced {
		var t Turret
		if err := json.Unmarshal(f.Event.Data, &t); err != nil {
			return nil, err
		}
		if s.Turrets == nil {
			s.Turrets = map[string]Turret{}
		}
		s.Turrets[fmt.Sprint(f.Event.UserID)] = t
		return json.Marshal(s)
	}
	if f.Event.Type != EventDowned {
		// stored with every event on the target, so a command's expected sequence is the target's
		return json.Marshal(s)
	}
	var d struct {
		Surge int64 `json:"surge"`
		Drone int   `json:"drone"`
	}
	if err := json.Unmarshal(f.Event.Data, &d); err != nil {
		return nil, err
	}
	k := fmt.Sprintf("%d.%d.%d", d.Surge, d.Drone, f.Event.UserID)
	if !s.Downed[k] {
		s.Downed[k] = true
		s.Count++
	}
	return json.Marshal(s)
}

func contributeSurges(reg *extensions.Registry) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: SurgesProjector, Source: source}, Title: "Swarm surges",
		Description: "The drones each world's players shot down in its swarm surges, keyed terrain.<worldId>: {downed: {<surge>.<drone>.<userId>: true}, count, turrets: {<userId>: {x, y, z}}}",
		Area:        Area, Types: []string{TerrainType},
		Fold: foldSurges,
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "game.surges", Source: source}, Title: "Swarm surges",
		Description: fmt.Sprintf("A world's swarm surges: {downed: {<surge>.<drone>.<userId>: true}, count, turrets: {<userId>: {x, y, z}}}, then each change. A surge comes every %d minutes (surge index = Unix ms / %d); each drone shot down with game.downDrone gave its shooter %g m³ of ore; turrets are the players' sentry turrets set down with game.placeTurret.", SurgePeriodMs/60_000, SurgePeriodMs, DroneScrap),
		Params:      json.RawMessage(`{"type":"object","properties":{"world":{"type":"string","description":"The world's id"}},"required":["world"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"world":"w123"}`)},
		Action:      "game.read",
		Resource:    func(p features.Principal, _ json.RawMessage) string { return Resource(p) },
		Run: live.ValueOf(SurgesProjector, func(f live.Feed) (string, error) {
			var p struct {
				World string `json:"world"`
			}
			if err := json.Unmarshal(f.Params(), &p); err != nil || !live.ValidToken(p.World) {
				return "", fmt.Errorf("game.surges needs a world's id")
			}
			return TerrainType + "." + p.World, nil
		}, json.RawMessage(`{"downed":{},"count":0}`)),
	})
}
