// Package game holds the game definitions on the server (forge plans 300
// and 360, the voxel RPG's first tracer bullet V1): a world definition,
// edited by game.* ops with inverses, and the terrain edits of a world,
// decided by a server game command blueprint and projected per edit cell.
//
// A world is a definition on the def stream (type gameWorld, projector
// game.worlds): its name and its generator's parameters (a seeded moon in
// V1). Every change is an op of the operation log with its inverse, so it
// is undone by request like any other definition.
//
// Terrain edits are not definitions: a dig or a fill is a player's action
// on the world, on the game area (stream EVT_<tenant>_game, target
// terrain.<worldId>). The command game.editTerrain is a built-in, editable
// command graph (graphs/cmd-game-editTerrain.json) that checks the edit
// against the world and emits it; the game.terrainCells projector keeps
// each world's edits per 16 m edit cell, so a client subscribes to the
// edits it needs and applies them in the server's order (the event's
// stream sequence). Browsers rebuild the terrain from the generator and the
// edits; nothing else is stored.
package game

import (
	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/authz"
	"github.com/SgtFlame/foundation/backend/features/extensions"
	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/logging"
)

var log = logging.In("game")

const source = "game"

const (
	// WorldType is a world's target type on the def stream.
	WorldType = "gameWorld"
	// WorldsProjector folds each world, keyed gameWorld.<id>.
	WorldsProjector = "game.worlds"
	// Area is the game area: terrain edits and, later, other play state.
	Area live.Area = "game"
	// TerrainType is a world's terrain target type on the game area, with
	// the world's id as its id.
	TerrainType = "terrain"
	// TerrainProjector keeps a summary of each world's edits, keyed
	// terrain.<worldId>: the edit command's state.
	TerrainProjector = "game.terrain"
	// CellsProjector keeps each world's edits per edit cell, keyed
	// <worldId>.<cx>_<cy>_<cz>.
	CellsProjector = "game.terrainCells"
	// EditCell is the edge of an edit cell in metres. An edit is kept in
	// the cell holding its centre; a client reading a cell also reads its
	// neighbours, since no edit reaches further than one cell.
	EditCell = 16.0
	// MinEditRadius and MaxEditRadius bound a brush's radius in metres;
	// the command graph checks them.
	MinEditRadius = 0.5
	MaxEditRadius = 6.0
)

// Resource is where game definitions and play are checked: the tenant.
func Resource(p features.Principal) string { return authz.Tenant(p) }

// ContributeAccess declares the game resource type: who may see games,
// change their definitions, and play (edit terrain).
func ContributeAccess(reg *extensions.Registry) {
	authz.ResourceTypes.Register(reg, authz.ResourceType{
		Descriptor: extensions.Descriptor{ID: "game", Source: source, Order: 60}, Title: "Games", Parent: "tenant",
		Actions: []authz.Action{
			{Name: "read", Title: "See games and their worlds", Class: authz.ClassRead},
			{Name: "write", Title: "Make and change game definitions such as worlds", Class: authz.ClassWrite},
			{Name: "play", Title: "Play: dig and fill a world's terrain", Class: authz.ClassWrite},
		},
	})
}

// Deps are what the game feature needs, made after the registry.
type Deps struct {
	Store live.Store
}

// Contribute registers the world definition's projector, ops and queries,
// the terrain projectors and query, the built-in edit graph and the tools.
func Contribute(reg *extensions.Registry, deps func() Deps) {
	contributeWorlds(reg)
	contributeTerrain(reg)
	contributePacks(reg)
	contributeAreas(reg)
	contributeGraphs(reg)
	contributeTools(reg, deps)
	contributeAreaTools(reg, deps)
}
