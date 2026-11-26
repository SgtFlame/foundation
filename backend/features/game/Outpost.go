package game

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// The outpost (forge 360 V6, a thin 372 and the 350 candidates): a beacon
// at each moon's north pole with a quartermaster offering contracts. A
// contract is handed in with a ship parked at the beacon (the
// cmd-game-handIn blueprint), which emits handedIn on the ship: its fold
// takes what was handed over from the hold, and game.players folds the
// same event into the sender's progress, keyed <worldId>.<userId>, so one
// event changes both.

//go:embed contracts/contracts.json
var contractsFile []byte

const (
	// PlayersProjector keeps each player's progress per world, keyed <worldId>.<userId>.
	PlayersProjector = "game.players"
	// EventHandedIn is a contract handed in with a ship: {world, contract, iron, skill, xp}.
	EventHandedIn = "handedIn"
	// XPPerLevel is the experience each skill level takes.
	XPPerLevel = 100
	// EventStood is where a player stands in a world, saved now and then and
	// when they stop (V8): {x, y, z}, on the world's terrain target
	EventStood = "stood"
)

// Player is a player's progress in one world.
type Player struct {
	Done   map[string]bool    `json:"done"`   // contracts handed in
	XP     map[string]float64 `json:"xp"`     // experience per skill
	Levels map[string]int     `json:"levels"` // 1 + xp / XPPerLevel
	// V8: the designs the contracts they did unlocked (the hauler)
	Unlocked map[string]bool `json:"unlocked,omitempty"`
	// V8: where they last stood, and so whether they have landed yet
	At *[3]float64 `json:"at,omitempty"`
}

// Contracts are the outpost's quartermaster and contracts.
type Contracts struct {
	Giver struct {
		Name  string `json:"name"`
		Title string `json:"title"`
		Line  string `json:"line"`
	} `json:"giver"`
	Contracts []struct {
		ID          string  `json:"id"`
		Title       string  `json:"title"`
		Description string  `json:"description"`
		Skill       string  `json:"skill"`
		XP          float64 `json:"xp"`
		// V8: what the ship must bring ({complete}, {iron}, {ore} in m³), the
		// design the job needs, and the design it unlocks
		Needs   map[string]any `json:"needs,omitempty"`
		Design  string         `json:"design,omitempty"`
		Unlocks string         `json:"unlocks,omitempty"`
	} `json:"contracts"`
}

var contracts = func() Contracts {
	var c Contracts
	if err := json.Unmarshal(contractsFile, &c); err != nil {
		panic("game: contracts.json: " + err.Error())
	}
	return c
}()

// ContractsOf gives the outpost's contracts.
func ContractsOf() Contracts { return contracts }

func foldPlayer(f live.Fold) (json.RawMessage, error) {
	p := Player{}
	if f.State != nil {
		if err := json.Unmarshal(f.State, &p); err != nil {
			return nil, err
		}
	}
	var d struct {
		Contract string  `json:"contract"`
		Skill    string  `json:"skill"`
		XP       float64 `json:"xp"`
		Unlock   string  `json:"unlock"`
		X        float64 `json:"x"`
		Y        float64 `json:"y"`
		Z        float64 `json:"z"`
	}
	if err := json.Unmarshal(f.Event.Data, &d); err != nil {
		return nil, err
	}
	if f.Event.Type == EventStood {
		p.At = &[3]float64{d.X, d.Y, d.Z}
		return json.Marshal(p)
	}
	if p.Done == nil {
		p.Done = map[string]bool{}
	}
	if p.XP == nil {
		p.XP = map[string]float64{}
	}
	p.Done[d.Contract] = true
	if d.Unlock != "" {
		if p.Unlocked == nil {
			p.Unlocked = map[string]bool{}
		}
		p.Unlocked[d.Unlock] = true
	}
	p.XP[d.Skill] += d.XP
	p.Levels = map[string]int{}
	for k, v := range p.XP {
		p.Levels[k] = 1 + int(math.Floor(v/XPPerLevel))
	}
	return json.Marshal(p)
}

func contributeOutpost(reg *extensions.Registry) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: PlayersProjector, Source: source}, Title: "Players' progress",
		Description: "Each player's progress in each world, keyed <worldId>.<userId>: {done: {<contract>: true}, xp: {<skill>: n}, levels: {<skill>: n}, at: [x, y, z]}, folded from the contracts they hand in and where they last stood",
		Area:        Area, Types: []string{ShipType, TerrainType},
		KeyOf: func(e live.Event) string {
			if e.UserID == 0 {
				return ""
			}
			if e.Type == EventStood && e.Target.Type == TerrainType {
				return PackKey(e.Target.ID, e.UserID)
			}
			if e.Type != EventHandedIn {
				return ""
			}
			var d struct {
				World string `json:"world"`
			}
			if json.Unmarshal(e.Data, &d) != nil || !live.ValidToken(d.World) {
				return ""
			}
			return PackKey(d.World, e.UserID)
		},
		Fold: foldPlayer,
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "game.player", Source: source}, Title: "My progress",
		Description: fmt.Sprintf("The caller's progress in a world: {done: {<contract>: true}, xp: {<skill>: n}, levels: {<skill>: n} (a level each %d xp, from 1), unlocked: {<design>: true} (designs contracts opened), at: [x, y, z] (where they last stood; missing until they have landed)}, then each change as they hand in contracts and move", XPPerLevel),
		Params:      json.RawMessage(`{"type":"object","properties":{"world":{"type":"string","description":"The world's id"}},"required":["world"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"world":"w123"}`)},
		Action:      "game.read",
		Resource:    func(p features.Principal, _ json.RawMessage) string { return Resource(p) },
		Run: live.ValueOf(PlayersProjector, func(f live.Feed) (string, error) {
			var p struct {
				World string `json:"world"`
			}
			if err := json.Unmarshal(f.Params(), &p); err != nil || !live.ValidToken(p.World) {
				return "", fmt.Errorf("game.player needs a world's id")
			}
			return PackKey(p.World, f.Args().Principal.UserID), nil
		}, json.RawMessage(`{"done":{},"xp":{},"levels":{}}`)),
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "game.contracts", Source: source}, Title: "Outpost contracts",
		Description: "The outpost's quartermaster and contracts: {giver: {name, title, line}, contracts: [{id, title, description, skill, xp}]}. Hand one in with game.handIn. Built in; they do not change while the server runs.",
		Params:      json.RawMessage(`{"type":"object","properties":{}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`)},
		Action:      "game.read",
		Resource:    func(p features.Principal, _ json.RawMessage) string { return Resource(p) },
		Run: func(f live.Feed) error {
			f.Snapshot(contracts, live.Position{})
			<-f.Context().Done()
			return nil
		},
	})
}
