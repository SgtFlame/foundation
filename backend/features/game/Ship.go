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
	EventShipStarted    = "started"     // {world, design, x, y, z, q, blocks}
	EventBlockPlaced    = "placed"      // {world, index}
	EventShipFlown      = "flown"       // {world, x, y, z, q, v, pilot, charge?}
	EventShipLoaded     = "loaded"      // {world, cargo: {dust, rock}, pack: {dust, rock}} (V4: the pack's change, negative)
	EventShipRefined    = "refined"     // {world, rock, iron, charge} (V4: the changes)
	EventShipDamaged    = "damaged"     // {world, blocks} (V5: the indices a crash broke)
	EventShipCrewed     = "crewed"      // {world, user, aboard} (V7: who boards or leaves the crew seat)
	EventSlept          = "slept"       // {world} (V13: the sender slept in this cabin's bed: their home)
	EventCrafted        = "crafted"     // {world, item, pack} (V14: the sender crafted a tool at this cabin's bench from their pack)
	EventSmelting       = "smelting"    // {world, ore, at, kind, pack} (V20: ore from the sender's pack put in this cabin's forge at Unix ms at; V30: kind a mineral or rock to refine)
	EventIngots         = "ingots"      // {world, iron, parts, pack} (V20: the forge's iron taken into the sender's pack; V30: or its components; the forge is empty again)
	EventFabricating    = "fabricating" // {world, item, at, pack} (V23: iron and ore from the sender's pack put in this cabin's fabricator at Unix ms at)
	EventFabricated     = "fabricated"  // {world, item, pack} (V23: the fabricator's work taken into the sender's pack)
	EventKitFitted      = "kitFitted"   // {world, boost, pack} (V24: an ion thruster kit from the sender's pack fitted to this ship)
	EventDriveFitted    = "driveFitted" // {world, pack} (V27: a jump drive from the sender's pack fitted to this ship)
	EventAssembled      = "assembled"   // {world, block, count} (V37: the block works assembled finished blocks into a storage building)
	EventStored         = "stored"      // {world, stock, pack?} (V36: what a storage building gained or gave, by name, and the sender's pack's change)
)

// ShipDesign is a premade ship: its blocks in its own grid, x right, y up
// and -z forward, the cockpit's facing.
type ShipDesign struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	BlockSize   float64       `json:"blockSize"`
	Blocks      []DesignBlock `json:"blocks"`
	// V13: a building (a static grid): never flown, no cockpit
	Static bool `json:"static,omitempty"`
	// V25: the share of its holds its stock thrusters lift; ion thruster
	// kits (Ship.Boost) add to it, up to a full hold (the ore barge's 0.5)
	Lift float64 `json:"lift,omitempty"`
	// V31: laid out from an earned design chip (ships from blocks, the
	// user, 2026-11-27): each block costs the components BlockCosts names,
	// taken from the placer's pack
	Chip bool `json:"chip,omitempty"`
}

// BlockCosts is what each block of a chip design costs in components
// (V31; the engine's ship/Blueprint.ts and cmd-game-placeBlock agree).
var BlockCosts = map[string]map[string]float64{
	"armor":     {"plate": 2},
	"cockpit":   {"plate": 3, "wire": 2},
	"thruster":  {"nozzle": 1, "plate": 1, "wire": 1},
	"gyro":      {"plate": 2, "wire": 3},
	"battery":   {"cell": 2, "wire": 1},
	"solar":     {"solarCell": 2, "wire": 1},
	"light":     {"wire": 1},
	"cargo":     {"plate": 4},
	"refinery":  {"plate": 6, "wire": 4},   // V32
	"concrete":  {"cement": 2},             // V33
	"deck":      {"plate": 1},              // V34: the bay floor's
	"clamp":     {"plate": 4, "wire": 2},   // V34
	"kiln":      {"plate": 3, "cement": 2}, // V35: the refinery's
	"control":   {"plate": 2, "wire": 3},
	"rack":      {"plate": 3},                         // V36: storage's
	"assembler": {"plate": 4, "wire": 3, "nozzle": 1}, // V37: the block works'
	"press":     {"plate": 6, "cement": 2},
	"arm":       {"plate": 2, "wire": 2, "cell": 1},
}

// DesignBlock is one block of a design: armor, cockpit, gyro, light,
// battery, cargo, refinery, solar, a building's log, door (an opening) or
// bed (V13), bench (V14), or thruster (push: the way it pushes the ship: up, down, forward, back,
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
	// V7: the users riding as its crew
	Crew []string `json:"crew"`
	// V4: what its hold carries (m³) and its battery's charge (0 to 1);
	// a design without power blocks has no charge to keep
	Cargo  Cargo   `json:"cargo"`
	Charge float64 `json:"charge"`
	// V20: a cabin's forge: the ore smelting in it and since when (Unix ms)
	Forge *Forge `json:"forge,omitempty"`
	// V23: a cabin's fabricator: what it is making and since when (Unix ms)
	Fab *Fab `json:"fab,omitempty"`
	// V24: what ion thruster kits add to its thrust (0.25 each, at most 0.5)
	Boost float64 `json:"boost,omitempty"`
	// V27: a jump drive is fitted: it jumps between the moon and the planet
	Jump bool `json:"jump,omitempty"`
	// V31: a chip design's block types by index, so placing one knows its cost
	Types []string `json:"types,omitempty"`
	// V36: what a storage building holds, by name: components (a count),
	// minerals, ore, rock and iron (m³)
	Stock     map[string]float64 `json:"stock,omitempty"`
	Seq       uint64             `json:"seq"`
	UpdatedAt time.Time          `json:"updatedAt"`
}

// Fab is the work in a cabin's fabricator (V23): one circuit board,
// FabMs long, powered by the cabin's solar panel.
type Fab struct {
	Item string `json:"item"`
	At   int64  `json:"at"`
}

// FabMs is how long the fabricator takes for a circuit board (V23).
const FabMs = 30_000

// Forge is the ore smelting in a cabin's forge (V20): SmeltMs a m³,
// IronYield of iron for each m³ of ore.
type Forge struct {
	Ore float64 `json:"ore"`
	At  int64   `json:"at"`
	// V30: what the load is: empty for ore, else a mineral (ferrite,
	// malachite, silica, wolframite, spodumene) or rock, refined into
	// components (Refines)
	Kind string `json:"kind,omitempty"`
	// V33: 2 when 0.1 m³ of coal from the pack fuels it: done twice as fast
	Rate float64 `json:"rate,omitempty"`
}

// Refine is what a forge makes of a m³ of a kind other than ore (V30).
type Refine struct {
	Part string  // the component: plate, wire, solarCell, nozzle, cell, cement
	Per  float64 // how many a m³ gives
	Ms   int64   // how long a m³ takes
}

// Refines is what the forge refines each kind into (V30; the engine's
// craft/Forge.ts REFINES and the smelt and takeIngots blueprints agree).
var Refines = map[string]Refine{
	"ferrite":    {"plate", 8, 60_000},
	"malachite":  {"wire", 10, 60_000},
	"silica":     {"solarCell", 4, 60_000},
	"wolframite": {"nozzle", 3, 60_000},
	"spodumene":  {"cell", 4, 60_000},
	"rock":       {"cement", 6, 30_000},
	"limestone":  {"lime", 6, 30_000}, // V33
}

const (
	// SmeltMs is how long the forge takes for each m³ of ore.
	SmeltMs = 45_000
	// IronYield is the iron a m³ of ore gives.
	IronYield = 0.8
)

// Cargo is what a ship's hold carries, in m³.
type Cargo struct {
	Dust float64 `json:"dust"`
	Rock float64 `json:"rock"`
	Iron float64 `json:"iron"`
	Ore  float64 `json:"ore"` // V8: from the asteroids
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
		case "armor", "gyro", "light", "battery", "cargo", "refinery", "solar", "log", "door", "bed", "bench", "forge", "fabricator", "concrete", "deck", "clamp", "kiln", "control", "rack", "assembler", "press", "arm":
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
	if d.Static {
		if cockpits != 0 {
			return "a building has no cockpit"
		}
		return ""
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
		World  string             `json:"world"`
		Design string             `json:"design"`
		Blocks int                `json:"blocks"`
		Index  *int               `json:"index"`
		X      float64            `json:"x"`
		Y      float64            `json:"y"`
		Z      float64            `json:"z"`
		Q      [4]float64         `json:"q"`
		V      [3]float64         `json:"v"`
		Pilot  string             `json:"pilot"`
		Charge *float64           `json:"charge"`
		Cargo  Cargo              `json:"cargo"`
		Rock   float64            `json:"rock"`
		Iron   float64            `json:"iron"`
		Ore    float64            `json:"ore"`
		User   string             `json:"user"`
		Aboard bool               `json:"aboard"`
		At     int64              `json:"at"`
		Item   string             `json:"item"`
		Kind   string             `json:"kind"`
		Rate   float64            `json:"rate"`
		Types  []string           `json:"types"`
		Boost  float64            `json:"boost"`
		Stock  map[string]float64 `json:"stock"`
	}
	// a damaged event's blocks are indices, read on their own below
	if err := json.Unmarshal(f.Event.Data, &d); err != nil && f.Event.Type != EventShipDamaged {
		return nil, err
	}
	switch f.Event.Type {
	case EventShipStarted:
		s = Ship{ID: f.Event.Target.ID, World: d.World, Design: d.Design, Blocks: d.Blocks, Placed: []int{}, Crew: []string{}, X: d.X, Y: d.Y, Z: d.Z, Q: d.Q, Charge: 1, Types: d.Types}
	case EventBlockPlaced:
		if d.Index != nil {
			i := sort.SearchInts(s.Placed, *d.Index)
			if i == len(s.Placed) || s.Placed[i] != *d.Index {
				s.Placed = append(s.Placed[:i], append([]int{*d.Index}, s.Placed[i:]...)...)
			}
			if *d.Index >= s.Blocks { // V20: a cabin started before its design grew a forge
				s.Blocks = *d.Index + 1
			}
		}
	case EventShipFlown:
		s.X, s.Y, s.Z, s.Q, s.V, s.Pilot = d.X, d.Y, d.Z, d.Q, d.V, d.Pilot
		// whoever takes the pilot's seat leaves the crew seat
		s.Crew = without(s.Crew, d.Pilot)
		if d.Charge != nil {
			s.Charge = clamp01(*d.Charge)
		}
	case EventShipLoaded:
		s.Cargo.Dust = round3(s.Cargo.Dust + d.Cargo.Dust)
		s.Cargo.Rock = round3(s.Cargo.Rock + d.Cargo.Rock)
		s.Cargo.Ore = round3(s.Cargo.Ore + d.Cargo.Ore)
	case EventShipDamaged:
		// Broken blocks go back to the outline; a broken cockpit seats nobody
		var b struct {
			Blocks []int `json:"blocks"`
		}
		if err := json.Unmarshal(f.Event.Data, &b); err != nil {
			return nil, err
		}
		gone := map[int]bool{}
		for _, i := range b.Blocks {
			gone[i] = true
		}
		kept := []int{}
		for _, i := range s.Placed {
			if !gone[i] {
				kept = append(kept, i)
			}
		}
		s.Placed = kept
		if gone[0] {
			s.Pilot = ""
		}
	case EventSmelting:
		s.Forge = &Forge{Ore: d.Ore, At: d.At, Kind: d.Kind, Rate: d.Rate}
	case EventIngots:
		s.Forge = nil
	case EventStored:
		for k, v := range d.Stock {
			if s.Stock == nil {
				s.Stock = map[string]float64{}
			}
			if s.Stock[k] = math.Max(0, math.Round((s.Stock[k]+v)*1000)/1000); s.Stock[k] == 0 {
				delete(s.Stock, k)
			}
		}
	case EventFabricating:
		s.Fab = &Fab{Item: d.Item, At: d.At}
	case EventFabricated:
		s.Fab = nil
	case EventDriveFitted:
		s.Jump = true
	case EventKitFitted:
		s.Boost = math.Min(0.5, math.Round((s.Boost+d.Boost)*1000)/1000)
	case EventShipCrewed:
		s.Crew = without(s.Crew, d.User)
		if d.Aboard && d.User != "" {
			s.Crew = append(s.Crew, d.User)
		}
	case EventHandedIn:
		s.Cargo.Rock = round3(s.Cargo.Rock + d.Rock)
		s.Cargo.Iron = round3(s.Cargo.Iron + d.Iron)
		s.Cargo.Ore = round3(s.Cargo.Ore + d.Ore)
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

const shipFields = "{id, world, design, blocks (how many the design has), placed (the indices of the design's blocks placed), x, y, z (metres from the moon's centre), q (orientation [x, y, z, w]), v (velocity), pilot (the user flying it, or empty), crew (the users riding as its crew), cargo {dust, rock, iron, ore} (m³ in its hold), charge (its battery, 0 to 1), forge {ore, at, kind} (V20: a cabin's forge smelting ore since Unix ms at; V30: kind, when set, the mineral or rock it refines into components), fab {item, at} (V23: a cabin's fabricator making a circuit board, or V24 an ion thruster kit, since Unix ms at), boost (V24: what ion thruster kits add to its thrust), jump (V27: a jump drive is fitted), types (V31: a chip design's block types by index), seq, updatedAt}"

func without(list []string, v string) []string {
	out := []string{}
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

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
