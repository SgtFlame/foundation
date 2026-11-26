package game

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"sort"
	"time"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
)

// Ships (forge 360 V3, a thin 364, 365 and 369): a ship is a grid of 1 m
// blocks laid out from a premade design. A player starts one where they
// stand (game.startShip), its outline showing every block still to place,
// places the blocks one by one (game.placeBlock), sits in its cockpit and
// flies it (game.flyShip, which also carries who is piloting). All three
// are play on the game area, decided by built-in blueprints
// (graphs/cmd-game-*.json); the game.ships projector folds them.

const (
	ShipType       = "gameShip"
	ShipsProjector = "game.ships"
	// WorldShipsProjector keeps the same ships keyed <worldId>.<shipId>, for game.worldShips
	WorldShipsProjector = "game.worldShips"
	EventShipStarted    = "started" // {world, design, x, y, z, q, blocks}
	EventBlockPlaced    = "placed"  // {world, index}
	EventShipFlown      = "flown"   // {world, x, y, z, q, v, pilot, charge?}
	EventShipLoaded     = "loaded"  // {world, cargo: {dust, rock}, pack: {dust, rock}} (V4: the pack's change, negative)
	EventShipRefined    = "refined" // {world, rock, iron, charge} (V4: the changes)
)

// ShipDesign is a premade ship: its blocks in its own grid, x right, y up
// and -z forward, the cockpit's facing.
type ShipDesign struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	BlockSize   float64       `json:"blockSize"`
	Blocks      []DesignBlock `json:"blocks"`
}

// DesignBlock is one block of a design: armor, cockpit, gyro, light or
// thruster (push: the way it pushes the ship: up, down, forward, back,
// left, right).
type DesignBlock struct {
	Type string `json:"type"`
	At   [3]int `json:"at"`
	Push string `json:"push,omitempty"`
}

// Ship is a ship as game.ships keeps it.
type Ship struct {
	ID     string     `json:"id"`
	World  string     `json:"world"`
	Design string     `json:"design"`
	Blocks int        `json:"blocks"`
	Placed []int      `json:"placed"`
	X      float64    `json:"x"`
	Y      float64    `json:"y"`
	Z      float64    `json:"z"`
	Q      [4]float64 `json:"q"` // orientation quaternion x, y, z, w
	V      [3]float64 `json:"v"` // velocity m/s, for others to carry it on between updates
	Pilot  string     `json:"pilot"`
	// V4: what its hold carries (m³) and its battery's charge (0 to 1);
	// a design without power blocks has no charge to keep
	Cargo     Cargo     `json:"cargo"`
	Charge    float64   `json:"charge"`
	Seq       uint64    `json:"seq"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Cargo is what a ship's hold carries, in m³.
type Cargo struct {
	Dust float64 `json:"dust"`
	Rock float64 `json:"rock"`
	Iron float64 `json:"iron"`
}

// CargoPerHold is what one cargo block holds, in m³; the loadShip blueprint checks it.
const CargoPerHold = 6.0

//go:embed designs/*.json
var designFiles embed.FS

var designs = map[string]ShipDesign{}

func init() {
	entries, err := fs.ReadDir(designFiles, "designs")
	if err != nil {
		panic(err)
	}
	for _, e := range entries {
		raw, _ := designFiles.ReadFile("designs/" + e.Name())
		var d ShipDesign
		if err := json.Unmarshal(raw, &d); err != nil {
			panic("game: designs/" + e.Name() + ": " + err.Error())
		}
		if why := d.Check(); why != "" {
			panic("game: designs/" + e.Name() + ": " + why)
		}
		designs[d.ID] = d
	}
}

// DesignOf gives a built-in ship design.
func DesignOf(id string) (ShipDesign, bool) {
	d, ok := designs[id]
	return d, ok
}

// Check gives why a design is not sound, or "": one cockpit, no two blocks
// in one cell, thrusters with a push, known types.
func (d ShipDesign) Check() string {
	seen := map[[3]int]bool{}
	cockpits := 0
	for i, b := range d.Blocks {
		if seen[b.At] {
			return fmt.Sprintf("blocks %d: two blocks at %v", i, b.At)
		}
		seen[b.At] = true
		switch b.Type {
		case "cockpit":
			cockpits++
		case "armor", "gyro", "light", "battery", "cargo", "refinery", "solar":
		case "thruster":
			switch b.Push {
			case "up", "down", "forward", "back", "left", "right":
			default:
				return fmt.Sprintf("blocks %d: a thruster pushes up, down, forward, back, left or right", i)
			}
		default:
			return fmt.Sprintf("blocks %d: unknown block type %q", i, b.Type)
		}
	}
	if cockpits != 1 {
		return "a design has one cockpit"
	}
	return ""
}

func foldShip(f live.Fold) (json.RawMessage, error) {
	var s Ship
	if f.State != nil {
		if err := json.Unmarshal(f.State, &s); err != nil {
			return nil, err
		}
	}
	var d struct {
		World  string     `json:"world"`
		Design string     `json:"design"`
		Blocks int        `json:"blocks"`
		Index  *int       `json:"index"`
		X      float64    `json:"x"`
		Y      float64    `json:"y"`
		Z      float64    `json:"z"`
		Q      [4]float64 `json:"q"`
		V      [3]float64 `json:"v"`
		Pilot  string     `json:"pilot"`
		Charge *float64   `json:"charge"`
		Cargo  Cargo      `json:"cargo"`
		Rock   float64    `json:"rock"`
		Iron   float64    `json:"iron"`
	}
	if err := json.Unmarshal(f.Event.Data, &d); err != nil {
		return nil, err
	}
	switch f.Event.Type {
	case EventShipStarted:
		s = Ship{ID: f.Event.Target.ID, World: d.World, Design: d.Design, Blocks: d.Blocks, Placed: []int{}, X: d.X, Y: d.Y, Z: d.Z, Q: d.Q, Charge: 1}
	case EventBlockPlaced:
		if d.Index != nil {
			i := sort.SearchInts(s.Placed, *d.Index)
			if i == len(s.Placed) || s.Placed[i] != *d.Index {
				s.Placed = append(s.Placed[:i], append([]int{*d.Index}, s.Placed[i:]...)...)
			}
		}
	case EventShipFlown:
		s.X, s.Y, s.Z, s.Q, s.V, s.Pilot = d.X, d.Y, d.Z, d.Q, d.V, d.Pilot
		if d.Charge != nil {
			s.Charge = clamp01(*d.Charge)
		}
	case EventShipLoaded:
		s.Cargo.Dust = round3(s.Cargo.Dust + d.Cargo.Dust)
		s.Cargo.Rock = round3(s.Cargo.Rock + d.Cargo.Rock)
	case EventShipRefined:
		s.Cargo.Rock = round3(s.Cargo.Rock + d.Rock)
		s.Cargo.Iron = round3(s.Cargo.Iron + d.Iron)
		if d.Charge != nil {
			s.Charge = clamp01(s.Charge + *d.Charge)
		}
	default:
		return f.State, nil
	}
	s.Seq, s.UpdatedAt = f.Event.Seq, f.Event.Time
	return json.Marshal(s)
}

const shipFields = "{id, world, design, blocks (how many the design has), placed (the indices of the design's blocks placed), x, y, z (metres from the moon's centre), q (orientation [x, y, z, w]), v (velocity), pilot (the user flying it, or empty), cargo {dust, rock, iron} (m³ in its hold), charge (its battery, 0 to 1), seq, updatedAt}"

func round3(v float64) float64 { return math.Max(0, math.Round(v*1000)/1000) }

func clamp01(v float64) float64 { return math.Min(1, math.Max(0, math.Round(v*10000)/10000)) }

func contributeShips(reg *extensions.Registry) {
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: ShipsProjector, Source: source}, Title: "Ships",
		Description: "Each ship in the worlds, keyed gameShip.<shipId>: " + shipFields,
		Area:        Area, Types: []string{ShipType}, Fold: foldShip,
	})
	live.Projectors.Register(reg, live.Projector{
		Descriptor: extensions.Descriptor{ID: WorldShipsProjector, Source: source}, Title: "Ships by world",
		Description: "The same ships keyed <worldId>.<shipId>, for game.worldShips",
		Area:        Area, Types: []string{ShipType},
		KeyOf: func(e live.Event) string {
			var d struct {
				World string `json:"world"`
			}
			if json.Unmarshal(e.Data, &d) != nil || !live.ValidToken(d.World) {
				return ""
			}
			return d.World + "." + e.Target.ID
		},
		Fold: foldShip,
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "game.worldShips", Source: source}, Title: "A world's ships",
		Description: "The ships in a world, as a map from <worldId>.<shipId> to " + shipFields + ", then each ship's new state as blocks are placed and it flies",
		Params:      json.RawMessage(`{"type":"object","properties":{"world":{"type":"string","description":"The world's id"}},"required":["world"]}`),
		Examples:    []json.RawMessage{json.RawMessage(`{"world":"w123"}`)},
		Action:      "game.read",
		Resource:    func(p features.Principal, _ json.RawMessage) string { return Resource(p) },
		Run: live.ListOf(WorldShipsProjector, func(f live.Feed) (string, error) {
			var p struct {
				World string `json:"world"`
			}
			if err := json.Unmarshal(f.Params(), &p); err != nil || !live.ValidToken(p.World) {
				return "", fmt.Errorf("game.worldShips needs a world's id")
			}
			return p.World + ".*", nil
		}),
	})
	live.Queries.Register(reg, live.Query{
		Descriptor: extensions.Descriptor{ID: "game.shipDesigns", Source: source}, Title: "Ship designs",
		Description: "The premade ship designs by id: {id, title, description, blockSize, blocks: [{type (armor, cockpit, gyro, light, thruster), at: [x, y, z] in the design's grid (x right, y up, -z forward), push (a thruster's push: up, down, forward, back, left, right)}]}. Built in; they do not change while the server runs.",
		Params:      json.RawMessage(`{"type":"object","properties":{}}`),
		Examples:    []json.RawMessage{json.RawMessage(`{}`)},
		Action:      "game.read",
		Resource:    func(p features.Principal, _ json.RawMessage) string { return Resource(p) },
		Run: func(f live.Feed) error {
			f.Snapshot(designs, live.Position{})
			<-f.Context().Done()
			return nil
		},
	})
}
