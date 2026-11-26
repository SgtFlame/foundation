package game

import (
	"encoding/json"
	"fmt"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Crops (forge 360 V9): food planted in the soil of the planet with air, a
// short flight from the moon. game.plant (cmd-game-plant) emits planted on
// the world's terrain target, game.harvest (cmd-game-harvest) harvested
// once the crop is ripe, with a pack change of food that game.packs folds.
// game.crops keeps each world's crops, keyed terrain.<worldId>.

const (
	// CropsProjector keeps each world's crops, keyed terrain.<worldId>.
	CropsProjector = "game.crops"
	// EventPlanted is a crop planted: {id, x, y, z}.
	EventPlanted = "planted"
	// EventHarvested is a ripe crop harvested: {crop, pack: {food}}.
	EventHarvested = "harvested"
	// EventAte is a ration eaten from the pack (V10, game.eat): {pack: {food: -1}}.
	EventAte = "ate"
	// RipeSeconds is how long a crop takes to ripen.
	RipeSeconds = 90
	// PlanetX, PlanetRadius: where the planet with air is in every world
	// (the engine's voxel/Bodies.ts PLANET), for the graphs' checks.
	PlanetX      = 1600.0
	PlanetRadius = 140.0
)

// Crop is one planted crop.
type Crop struct {
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
	Z  float64 `json:"z"`
	At int64   `json:"at"` // when it was planted, Unix milliseconds
	By uint64  `json:"by,string"`
}

// Crops are a world's crops and the next crop's number.
type Crops struct {
	Crops map[string]Crop `json:"crops"`
	Next  int             `json:"next"`
}

func foldCrops(f live.Fold) (json.RawMessage, error) {
	c := Crops{}
	if f.State != nil {
		if err := json.Unmarshal(f.State, &c); err != nil {
			return nil, err
		}
	}
	if c.Crops == nil {
		c.Crops = map[string]Crop{}
	}
	switch f.Event.Type {
	case EventPlanted:
		var d struct {
			ID string  `json:"id"`
			X  float64 `json:"x"`
			Y  float64 `json:"y"`
			Z  float64 `json:"z"`
		}
		if err := json.Unmarshal(f.Event.Data, &d); err != nil {
			return nil, err
		}
		c.Crops[d.ID] = Crop{X: d.X, Y: d.Y, Z: d.Z, At: f.Event.Time.UnixMilli(), By: f.Event.UserID}
		c.Next++
	case EventHarvested:
		var d struct {
			Crop string `json:"crop"`
		}
		if err := json.Unmarshal(f.Event.Data, &d); err != nil {
			return nil, err
		}
		delete(c.Crops, d.Crop)
	}
	return json.Marshal(c)
}

func contributeCrops(reg *extensions.Registry) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: CropsProjector, Source: source}, Title: "Crops",
		Description: "Each world's crops on the planet with air, keyed terrain.<worldId>: {crops: {<id>: {x, y, z, at (planted, Unix ms), by}}, next}",
		Area:        Area, Types: []string{TerrainType},
		Fold: foldCrops,
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "game.crops", Source: source}, Title: "Crops",
		Description: fmt.Sprintf("A world's crops on the planet with air: {crops: {<id>: {x, y, z, at (planted, Unix ms), by}}, next}, then each change. A crop is ripe %d s after it was planted; plant with game.plant and harvest with game.harvest.", RipeSeconds),
		Params:      json.RawMessage(`{"type":"object","properties":{"world":{"type":"string","description":"The world's id"}},"required":["world"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"world":"w123"}`)},
		Action:      "game.read",
		Resource:    func(p features.Principal, _ json.RawMessage) string { return Resource(p) },
		Run: live.ValueOf(CropsProjector, func(f live.Feed) (string, error) {
			var p struct {
				World string `json:"world"`
			}
			if err := json.Unmarshal(f.Params(), &p); err != nil || !live.ValidToken(p.World) {
				return "", fmt.Errorf("game.crops needs a world's id")
			}
			return TerrainType + "." + p.World, nil
		}, json.RawMessage(`{"crops":{},"next":0}`)),
	})
}
