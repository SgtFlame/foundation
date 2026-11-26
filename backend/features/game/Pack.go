package game

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// The pack (forge 360 V2, a thin 362d and 363d): what a player walking a
// world carries of the ground they dug. Each edit the editTerrain
// blueprint decides for a player carries the pack's change as pack:
// {dust, rock} (m³, negative for what a fill spent); the game.packs
// projector folds them per world and player.

const (
	// PacksProjector keeps each player's pack per world, keyed <worldId>.<userId>.
	PacksProjector = "game.packs"
	// PackCapacity is what a pack holds in m³; the blueprint checks it.
	PackCapacity = 4.0
)

// Pack is what a player carries in one world.
type Pack struct {
	Dust     float64 `json:"dust"`
	Rock     float64 `json:"rock"`
	Ore      float64 `json:"ore"` // V8: from the asteroids
	Food     float64 `json:"food"` // V9: harvested on the planet with air
	Capacity float64 `json:"capacity"`
}

// PackKey is a player's pack's key in game.packs.
func PackKey(world string, user uint64) string { return world + "." + strconv.FormatUint(user, 10) }

func contributePacks(reg *extensions.Registry) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: PacksProjector, Source: source}, Title: "Players' packs",
		Description: "What each player carries in each world, keyed <worldId>.<userId>: {dust, rock, capacity} in m³, folded from the pack changes terrain edits carry and what loading a ship's hold took",
		Area:        Area, Types: []string{TerrainType, ShipType},
		KeyOf: func(e live.Event) string {
			if e.UserID == 0 || !hasPack(e.Data) {
				return ""
			}
			switch {
			case (e.Type == EventEdited || e.Type == EventHarvested) && e.Target.Type == TerrainType:
				return PackKey(e.Target.ID, e.UserID)
			case e.Type == EventShipLoaded && e.Target.Type == ShipType:
				// V4: loading a ship's hold empties the loader's pack
				var d struct {
					World string `json:"world"`
				}
				if json.Unmarshal(e.Data, &d) != nil || !live.ValidToken(d.World) {
					return ""
				}
				return PackKey(d.World, e.UserID)
			}
			return ""
		},
		Fold: func(f live.Fold) (json.RawMessage, error) {
			p := Pack{Capacity: PackCapacity}
			if f.State != nil {
				if err := json.Unmarshal(f.State, &p); err != nil {
					return nil, err
				}
			}
			var d struct {
				Pack *struct {
					Dust float64 `json:"dust"`
					Rock float64 `json:"rock"`
					Ore  float64 `json:"ore"`
					Food float64 `json:"food"`
				} `json:"pack"`
			}
			if err := json.Unmarshal(f.Event.Data, &d); err != nil {
				return nil, err
			}
			if d.Pack == nil {
				return f.State, nil
			}
			round := func(v float64) float64 { return math.Max(0, math.Round(v*1000)/1000) }
			p.Dust = round(p.Dust + d.Pack.Dust)
			p.Rock = round(p.Rock + d.Pack.Rock)
			p.Ore = round(p.Ore + d.Pack.Ore)
			p.Food = round(p.Food + d.Pack.Food)
			p.Capacity = PackCapacity
			return json.Marshal(p)
		},
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "game.pack", Source: source}, Title: "My pack",
		Description: fmt.Sprintf("What the caller carries in a world: {dust, rock, ore, capacity} in m³ and food in rations (an empty pack holds %g m³), then each change as they dig and fill", PackCapacity),
		Params:      json.RawMessage(`{"type":"object","properties":{"world":{"type":"string","description":"The world's id"}},"required":["world"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"world":"w123"}`)},
		Action:      "game.read",
		Resource:    func(p features.Principal, _ json.RawMessage) string { return Resource(p) },
		Run: live.ValueOf(PacksProjector, func(f live.Feed) (string, error) {
			var p struct {
				World string `json:"world"`
			}
			if err := json.Unmarshal(f.Params(), &p); err != nil || !live.ValidToken(p.World) {
				return "", fmt.Errorf("game.pack needs a world's id")
			}
			return PackKey(p.World, f.Args().Principal.UserID), nil
		}, json.RawMessage(fmt.Sprintf(`{"dust":0,"rock":0,"ore":0,"food":0,"capacity":%g}`, PackCapacity))),
	})
}

func hasPack(data json.RawMessage) bool {
	var d struct {
		Pack json.RawMessage `json:"pack"`
	}
	return json.Unmarshal(data, &d) == nil && len(d.Pack) > 0 && string(d.Pack) != "null"
}
