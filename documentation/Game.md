# Game engine and game worlds

The web game engine (Forge plans 300 to 347) and the games built on it (350 to 390). This document covers what is built: **V1**, the voxel game's first tracer bullet ([forge 360-VoxelRPG.md › V1](../../forge/documentation/plans/360-VoxelRPG.md#the-first-tracer-bullet-v1-in-detail)), in which two people fly over a generated moon and dig and fill it together, with the server deciding every edit.

## Built in V1 (forge 360)

### The engine package, `@foundation/engine`

`frontend/engine` is a third Yarn workspace beside `common` and `example`, consumed as TypeScript source like `common` (`@foundation/engine/<path>` maps to `src/<path>`). It depends on `three` 0.187 only. `yarn test` runs its unit tests with `node --test`; `yarn dev` serves `demo/`, a stand-alone moon with local edits (`?seed=`, `?radius=`, `?voxel=`, `?view=`, `?webgl=1`), with no server.

| Module | What it is |
|---|---|
| `core/Loop.ts` | The fixed-step loop: simulation at 60 Hz, rendering every frame with the interpolation `alpha`, frame and work times |
| `render/Renderer.ts` | `Renderer.create(canvas, {forceWebGL})`: three.js `WebGPURenderer`, which falls back to WebGL 2 by itself; `backend` says which (`webgpu` or `webgl2`), `screenshot()` and `info()` (draw calls, triangles) |
| `input/Input.ts` | Keyboard and mouse, pointer lock on click, presses per step, the wheel. Keys count only while the pointer is locked or the canvas has focus, so a panel beside the view can be typed in |
| `camera/FlyCamera.ts` | WASD, Space up, C down, Shift faster, mouse look |
| `debug/StatsOverlay.ts` | The frame-time overlay |
| `voxel/` | The voxel world below |
| `assets/NoiseTexture.ts` | Placeholder textures made in code (models-as-code, plan 300 › 11) |

### Smooth voxels

- **Density and material.** Terrain is a signed distance field, negative inside, sampled at `voxelSize` (0.5 m, the user's decision) in chunks of 32³ cells (16 m). A chunk samples from index -1 to 32, so its mesh reaches its neighbour's and seams match.
- **Surface nets** (`SurfaceNets.ts`) put one vertex per cell that the surface crosses, with normals from the density's gradient, and make a quad per crossing edge the chunk owns, wound from solid to empty.
- **The moon generator** (`MoonGenerator.ts`): a sphere of `radius` with fractal simplex hills (`amplitude`, `frequency`, `octaves`) and `craters` of up to `craterMaxRadius`, all from `seed`; dust above `dustDepth`, rock below it and on rough ground. Hills are sampled on a lattice twice the voxel size and interpolated, and craters are culled per chunk. A chunk samples in about 7.6 ms and meshes in about 2 ms in Node.
- **Edits** are smooth spheres: a dig is a smooth max with the brush, a fill a smooth min, in the server's order (`seq`). `ChunkSampler.ts` applies the edits that touch a chunk. The tests check that chunk samples equal the point function, that neighbouring chunks share seam vertices, that faces wind with the gradient, and that digging removes and filling adds ground.
- **Workers** (`MeshWorker.ts`, `WorkerPool.ts`) sample and mesh chunks off the main thread: from 2 to 8 workers, 2 jobs each, buffers transferred back.
- **`VoxelWorld.ts`** loads the chunks within `viewDistance` (160 m) that the moon's shell can reach, meshes the nearest first (chunks already shown keep their mesh until the new one arrives), and re-meshes only the chunks an edit touches. `TerrainMaterial.ts` is a triplanar node material that mixes dust and rock by a per-vertex attribute.
- **`VoxelViewport.ts`** is what an editor embeds: `VoxelViewport.create(container, {params, onEditIntent, onCameraMove, forceWebGL, stats})`. A left click digs, a right click fills, held to repeat, and the wheel sizes the brush from 0.75 to 6 m. Its methods are `setParams`, `setEdits`, `addEdits(edits, mine)`, `setRemoteCameras` (cones labelled with names), `cameraState`/`setCamera`, `screenshot()` and `stats()` (frame times, chunks, triangles, mesh memory, heap, first load, edits, and the edit round trip from a click until the edit comes back from the server). It sends nothing itself: the host sends each `EditIntent` as a command.

### Game worlds on the server, `features/game`

- **Access**: resource type `game` with `read`, `write` (definitions) and `play` (digging and filling), on the tenant.
- **Worlds** are definitions on the `def` area, target type `gameWorld`, folded by `game.worlds` (keyed `gameWorld.<id>`). Each command is an op with its inverse:
  - `game.createWorld {name, generator?}` (id `w<TSID>`; undone by delete);
  - `game.setWorld {name?, generator?}` (the generator merged per field; the inverse carries only the changed fields);
  - `game.deleteWorld` and `game.restoreWorld`.
  The generator is `game.Moon`, with the same fields as the engine's `MoonParams` and checked against `moonSchema`. Queries are `game.worlds {deleted?}` and `game.world {id}`. Undo scopes are `gameWorld:<id>` (`ops.ParseScope`). `ops.Items` names a world's fields (`field name`, `field generator.<key>`).
- **Terrain edits** are play, not definitions: the `game` area, target type `terrain`, target the world's id, event `edited`. They are decided by **`game.editTerrain`, a built-in editable command graph** (`graphs/cmd-game-editTerrain.json`). It reads the world, refuses a missing world, a kind other than dig or fill, a radius outside 0.5 to 6 m and a brush far from the moon, then emits the edit.
  - `game.terrain` keeps a summary per world (`terrain.<id>`: count, last seq).
  - `game.terrainCells` keeps the edits **per 16 m edit cell** (`<world>.<cx>_<cy>_<cz>`, by the edit's centre; a reader of a cell also reads its neighbours, since a brush is at most 6 m). Each edit holds `seq`, `by` and `cmd`.
  - The live `game.terrainEdits {world}` gives a world's cells and then each changed cell. V1 subscribes to all of a world's cells; per-chunk subscriptions come with streaming (360d).
- **Tools**: `game.describeWorld {id}` gives the definition and a summary of its edits (`game.read`). The view itself is shown by Forge's chat client tool `game.screenshot`, and as a picture by the forge CLI's `game_screenshot`.
- `serve.go` calls `game.ContributeAccess` and `game.Contribute(registry, func() game.Deps)` with the projections. `apps/ops`' `TestEveryOpUndoes` and `apps/tools`' `TestMetadata` include the feature.

### Deep links

`/workbench?open=<workbench link>` opens any subject that a `workbench.subjectOpener` handles (`openDeepLink` in `ui/editorKinds.ts`), for example `?open=workbench:gameWorld/w123`.

## Built in S1 and S2 (forge 375)

The story game's first two tracer bullets: areas laid out from pieces, then walked at night.

### Piece sets and areas, `features/game`

- **Piece sets** are JSON files embedded from `features/game/piecesets/` (`farmland.json` first). A set has a cell size (10 m), edge types (`grass`, `road`) and pieces. Each piece has a footprint in whole cells, a model name and the edge type of each cell along each side (`n`, `e`, `s`, `w`). `PieceSet.go` turns a piece clockwise (`Turned`) and checks a placement (`Fit`): inside the area, not over another piece, and every edge meeting a neighbour's edge of a type it may meet. A misfit names the cell, for example "the Farmhouse would cover the Road end at cell 7, 6". The browser's `area/PieceSet.ts` is a port with the same messages, tested against the same JSON.
- **Areas** are definitions on the `def` area, target type `gameArea`, projection `game.areas`. The ops are `game.createArea {name, pieceSet, width, depth}`, `game.setArea` (refused when shrinking past a piece), `game.placePiece {id?, piece, x, z, rot}`, `game.movePiece`, `game.removePiece`, `game.deleteArea` and `game.restoreArea`, each with its inverse. A misfit is refused with `piece_misfit`. An area has at most 64 cells a side.
- **Queries**: `game.areas {deleted?}`, `game.area {id}` and `game.pieceSets`.
- **Tool**: `game.describeArea {area}` returns the area, an ASCII map with one character per cell, its legend and a digest of each piece. A model reads the layout from it without a picture.
- `apps/ops` knows the `gameArea` scope and its items (a piece by id, the area's fields), and `TestEveryOpUndoes` covers each area op.

### The area viewport, `frontend/engine/src/area`

- `Models.ts` builds each piece's placeholder model from code (grass, roads, fences, trees, a copse, fields, the farmhouse, the barn, the yard). A model names its night lights (`userData.lights`) and what a walker cannot cross (`userData.blockers`, rectangles in the model's own metres).
- `AreaViewport` shows an area with an orbit camera (`camera/OrbitCamera.ts`: right-drag turns, middle-drag or W A S D pans, Q and E turn, the wheel zooms). With a piece in hand it shows a ghost model, a green or red footprint and the edge marks. Clicking places a piece, or selects one; R turns it, Alt and the arrows move it, Delete removes it. It sends each change to its host as an intent and draws what the server sends back. Others' cursors and selections show in their colours.
- **Night** lights the area with two moons as directional lights with shadows, the models' point lights, and fog. The moons' brightness follows their phases from the calendar night (`sky/Moons.ts`: the greater moon over 30 nights, the lesser over 13), and a corner of the view draws both discs with their phase names, since the top-down camera never sees the sky.
- **Play** (`play(true)`) builds a walk grid from the placed pieces' blockers, turned and moved with each piece, and stands a placeholder character (`CharacterModel`) on the yard. A click on the ground walks it there along an A* path, straightened by line of sight, at 4 m/s with a walk animation; the camera follows. The cursor shows whether the ground under it can be reached. A piece laid while playing rebuilds the grid. `walkTo(x, z)` and `player()` serve tests and tools.
- `nav/WalkGrid.ts` is the walk grid: 1 m cells, blocked rectangles grown by the walker's radius, 8-way A* without cutting corners. It stands in for the Recast navigation mesh until areas have height levels, and it is deterministic, so the server can run the same grid when it takes movement over (S4).

## Built in V2 (forge 360)

The voxel game's second tracer bullet: walk on the moon, fly to an asteroid with a thrust pack, carry what you dug.

- **Bodies** (`voxel/Bodies.ts`, a thin 361a): `WorldGenerator` joins the moon and the generator's `asteroids` (0 to 8, default 2 for new worlds) into one terrain, the smallest signed distance of each body's own `MoonGenerator`. A seed places each asteroid, 8 to 16 m in radius, 50 to 90 m above the moon; the first floats over the north pole, in sight of a new player. The chunk sampler, the mesh workers and collisions all read the joined terrain. `VoxelWorld.densityAt` and `sampleAt` give the terrain at a point with the edits that reach it.
- **Gravity** (361b, thin): each body pulls toward its centre with its surface pull (the moon 1.62 m/s² at 100 m, scaling with radius; an asteroid at least 0.35 m/s²) up to its highest ground, falling off as the 7th power of distance beyond it, so an asteroid's own pull wins a short way up.
- **The walker** (`character/Walker.ts`, a thin 362a and 362b): a character of three spheres that walks on whichever body pulls it, its up turning toward away-from-the-pull, with ground snapping so it does not launch off crests in the low pull. Space jumps; X turns the thrust pack on, which flies in the view's direction (Space up, C down) with dampeners that stop it when let go. It reads only a terrain (density and gravity), so its tests run in Node.
- **`VoxelViewport`** has two modes: **fly** (the editor's camera, as in V1) and **walk** (G switches; `setMode`, `onModeChange`). Walking, the brush reaches 8 m from the eyes, and each dig or fill sends what it takes (`took: {dust, rock}`) or needs (`needs`), reckoned from the terrain with `VoxelWorld.contentsOf`. A panel shows the pack, the pull, the speed and the thrust pack.
- **The pack** (a thin 362d and 363d): the `cmd-game-editTerrain` blueprint now reads the sender's pack (`game.packs`, keyed `<worldId>.<userId>`). A dig adds what it took, bounded by its sphere's volume, and is refused when the pack (4 m³) would overflow; a fill spends dust first, then rock, is made of whichever it spent more of, and is refused when the pack holds too little. Edits without `took` or `needs`, the editor's, leave the pack alone. Each edit carries the pack's change as `pack: {dust, rock}`. `game.pack {world}` gives the caller's own pack. The engine's `LocalEditLog` (the demo) runs the same rules.
- The edit's distance limit now reaches the asteroids: the moon's radius plus its hills plus 150 m.

## Built in S3 (forge 375)

The story game's third tracer bullet: go into the farmhouse and talk to the farmer.

- **Interiors** are separate areas (375 open question 7). A piece set may be `interior`: walls stand round the area's edge and every cell is floor. `kitchen.json` is the first, with cells of 2 m: hearth, table, bench, box bed, shelves, barrels, a door and a window. A piece marked `boundary` (hearth, shelves, door, window) must stand on the area's edge; `Fit` and `fit` refuse it elsewhere with "the Hearth at cell 2, 2 must stand on the area's edge". Such pieces draw their wall side at the model's north, so a piece on the south wall is turned twice.
- **Doors**: a piece marked `door` (the farmhouse, the barn, the kitchen's door) may lead to another area. `game.linkDoor {id, area}` sets the placement's `link`, or clears it with `area: ""`. It is refused for a piece with no door (`not_a_door`), for the area itself and for an area that does not exist. Its inverse restores the old link, and `removePiece`'s inverse keeps the link.
- **People** stand in an area in metres: `game.placeNpc {id?, name, kind, x, z, facing, dialogue}`, `game.moveNpc` (fields left out keep their value) and `game.removeNpc`, each with its inverse. They are kept as the area's `npcs`. `apps/ops` knows their items (a person by id, a door's link), and `TestEveryOpUndoes` covers all four ops.
- **Dialogues** are JSON files embedded from `features/game/dialogues/` (`farmer.json` first), read with the `game.dialogues` query: a speaker, a start node, and nodes of a line with answers that lead to another node or end the talk. `Dialogue.Check` refuses an answer that leads nowhere. The farmer's lines are placeholders drawn from the first hour's beats.
- `game.describeArea` lists the people and where each door leads.
- **In the viewport**: `interiorShell` draws an interior's plank floor and walls. People are `CharacterModel`s with their names above them. With a person in hand (`setNpcTool(kind)`) a click stands one there; a click on a person selects them, Alt and the arrows move them half a metre, Delete removes them. In Play, clicking a person walks up to them and opens the talk panel (`area/DialoguePanel.ts`: their line and numbered answers, keys 1 to 9 and Escape). Clicking a linked door walks to the point its model names (`userData.door`) and calls `onTravel(area)`. The host reads that area and calls `travel(area, set)`, and the character comes in by the door that leads back. Stop returns to the area being edited. People block the walk grid. Stats add `areaShown`, `npcs` and `talking`.

## Built in S4 (forge 375)

The story game's fourth tracer bullet: the first fight, two Hollowed at the barn, decided by the character's skills.

- **`combat/Combat.ts`** is a pure simulation, deterministic from its seed and the orders given, so it runs in Node tests and later in the Node runner (346). The player chooses a target and a stance; the character's skills (`blade`, `polearm`, `reading`, 0 to 5) decide whether a strike lands (`hitChance`) and whether it is vital (`vitalChance`).
  - **Stances**: *Strike* attacks, *Pivot* circles the target, dodging half the claws and setting up a flank strike, and *Guard* turns claws on the knife arm (a fifth of the damage; none if the tell was read, which leaves an opening). Strike, Pivot, Guard held in turn and back to Strike is the old **cadence**, from blade skill 2: the next strike is aimed.
  - **The Hollowed** (`CREATURES.hollowed`) take harm only to the neck or the heart. Any other hit staggers them, and they keep coming. They drive for the character, slide round blocked cells, and show a tell (`!`) before each claw to a character with reading 1 or more.
  - **Weapons**: *Sword and knife* (blade, reach 1.5 m) and *Pitchfork* (polearm, reach 2.3 m, slower, aiming for the heart).
  - A test checks that the thimble's starting skills (`STARTING_SKILLS`: blade 2, polearm 1, reading 1) win most fights against two Hollowed by striking alone, but not all of them.
- **In the viewport**: a person whose kind is a creature (`isHostile`) is a foe. They are drawn ash-grey and hunched with ember eyes and a red label, and they do not block the walk grid. Play starts a fight when the area holds foes:
  - Clicking a foe makes it the target. The character closes on it and strikes, and in Pivot circles it.
  - Keys 1, 2 and 3 set the stance, or use the fight panel (`combat/CombatHud.ts`: health, stances, the weapon and skill, and the last three things that happened).
  - A slain foe leaves a heap of ash and rags (`ashPile`).
  - Stop ends the fight and stands the foes back where they were placed.
  - `setWeapon`, `setSkills`, `setStance`, `fight(id)` and `doorPoints()` serve hosts and tests, and stats add `combat`.

## Built in V3 (forge 360)

The voxel game's third tracer bullet: lay out a small ship from a premade design, place its blocks one by one, sit in its cockpit and fly it.

- **Designs** are JSON files embedded from `features/game/designs/` (`skiff.json` first), read with the `game.shipDesigns` query: a grid of 1 m blocks (`armor`, `cockpit`, `gyro`, `light`, `thruster` with the way it pushes), x right, y up and -z forward. The skiff has 27 blocks: a cockpit at the nose, a gyroscope, two lights, three lift thrusters below and one thruster each other way. `ShipDesign.Check` refuses a design without exactly one cockpit or with two blocks in one cell.
- **Ships** are play on the `game` area, not ops (target type `gameShip`), decided by three blueprints:
  - `game.startShip {world, design, x, y, z, q}` (`cmd-game-startShip`) starts a ship with no block placed; the sender makes its id. It is refused when the ship exists, the world does not, the design is unknown or the point is far from the world's bodies.
  - `game.placeBlock {index}` (`cmd-game-placeBlock`) places one block of the design, in any order; it is refused for an index outside the design or a block already placed.
  - `game.flyShip {x, y, z, q, v?, pilot}` (`cmd-game-flyShip`) sets where it is, how it moves and whether the sender sits in it. It needs the cockpit placed and is refused while someone else flies it.
  - `game.ships` folds them by ship, and `game.worldShips {world}` lists a world's ships from `game.worldShips` (keyed `<worldId>.<shipId>`).
- **The flight model** (`ship/Ship.ts`, `ShipBody`, a thin 364 and 365) runs in the pilot's browser and in Node tests. Only placed blocks count: their mass, the thrust each way of the placed thrusters (6 kN each), and turning only with the gyroscope. Inertial dampeners hold the ship still against the pull of the nearest body when the helm is let go, within what its thrusters can give, so a skiff without lift thrusters falls. It rests on the ground, lifted along up from its lowest block faces. `lookQuat`, `rotate` and `blockWorld` are its quaternion helpers.
- **In the viewport** (`ship/ShipLayer.ts`): placed blocks are boxes, thrusters with a nozzle; blocks still to place are a translucent outline. Walking, an outline block within 8 m and nearer than the ground lights up, and a left click places it (`onShipIntent({kind: 'place'})`). F within 3 m of a placed cockpit sits in it. Flying, W and S push forward and back, A and D sideways, Space up, C down, the mouse turns and Q and E roll. A chase camera follows it, and its place goes to the server four times a second. F leaves the seat, standing beside the cockpit. Other people's ships are eased toward the server's place, carried on by their velocity. `startShip(design)` lays a ship out a few metres ahead on the ground, facing the player's way; `sit(id)`, `leaveSeat()`, `setShips`, `setShipDesigns` serve hosts and tests, and stats add `ships` and `piloting`.

## Built in S5 (forge 375)

The story game's fifth tracer bullet: a night of hearth defence at one house, with one hearth-watch and whistle signals.

- **`defence/HearthNight.ts`** is a pure, seeded night at one house (an interior area with a hearth). The stone moves 20 to 40 s into the night; the first Hollowed comes up through the hearthstone 15 s later, and another every 20 to 30 s (2 by default). A **barrel** stood on the hearthstone (the cell in front of the hearth, `hearthOf`) holds it down 25 s longer.
- The **hearth-watch** is a person of kind `watcher` standing in the house. They whistle as the book's watchers do: one blast when the stone moves, two when something comes through, three when about to die. Alone with what came through they hold it off with the pike and are clawed (45 health, 6 a claw). When they fall the family is taken and the night is lost. While the character is in the house, the watch stands back and takes no harm. Dawn (3 minutes by default) ends the night: the house held. With no watch in the house, nobody whistles and the family is taken as soon as something comes through.
- **In the viewport**: `startHearthNight(house)` starts a night while playing. The night runs whatever area is shown. Whistles play as a tin whistle through Web Audio (`defence/Whistle.ts`, no sound file), and each signal's meaning shows in the night panel (`defence/NightHud.ts`: time to dawn, the watch's health, the hearthstone, how many came through). Travelling into the house shows what came through standing by the hearth, and the fight of S4 begins (`Combat.addFoe` adds each newcomer). A slain one is gone from the night too. Stop ends the night. Stats add `hearth`.

## Built in V4 (forge 360)

The voxel game's fourth tracer bullet: power and a hold on the ship, and refining ore aboard.

- **The prospector** (`designs/prospector.json`, 32 blocks) is the skiff with a battery, a cargo hold, a refinery and two solar panels. `ShipDesign.Check` knows the new block types.
- **Power** (`ship/Ship.ts`, a thin 367): a design with a battery or panels counts power; the skiff does not. Thrusters draw by how hard they push (10 kW each at full push), the gyroscope 2 kW while turning, and each panel gives 2 kW. A battery stores 3 MJ. When it runs flat, or with no battery placed, the ship gets only what the panels give, so a prospector sinks to the ground. The pilot's window sends the charge with `game.flyShip` (`charge`). Sitting down adds what the panels gave while it stood, from `updatedAt` (`ShipBody.recharge`).
- **The hold**: `game.loadShip {}` (`cmd-game-loadShip`) empties the sender's pack into the ship's hold, dust first, up to 6 m³ per cargo block placed. Its `loaded` event carries the hold's gain and the pack's loss. `game.packs` also folds ship `loaded` events, keyed by the loader, so one event changes both and no second target is needed.
- **Refining**: `game.refineShip {}` (`cmd-game-refineShip`) runs the refinery once. Up to 1 m³ of rock becomes half as much iron, for a tenth of the battery. It is refused while someone flies the ship, so the pilot's charge never overwrites it.
- `Ship` adds `cargo {dust, rock, iron}` and `charge` (a new ship starts charged).
- **In the viewport**: within 4 m of a ship, L loads the pack into its hold and R refines (`onShipIntent({kind: 'load' | 'refine'})`). The panel in the corner shows the ship's charge, power and hold while near it or flying it.

## Decisions to review after seeing it

- A terrain edit is a command graph of the existing `command` kind, not a new `game.command` kind; a game kind comes with the Node runner (346), when rules run per tick.
- Edits are kept per 16 m cell and V1 reads them all. Streaming (360d) reads only the cells in view.
- Undo covers world definitions only; digging is play and is not an op.
- S2 walks on a 1 m grid rather than a Recast navigation mesh, and moves the character in the browser. Recast comes with height levels; the server takes movement over in S4.
- The moons are drawn in a corner of the view rather than in the sky, since the camera looks down. Bloom is left out until it is needed.
- V2's pack trusts the browser's reckoning of what a dig takes or a fill needs, bounded by the brush's volume. The server reckons it itself once the generator runs on the server (346).
- Asteroids are generated from the world's seed, not placed one by one; bodies as their own definitions come with 361a.
- S3's people stand still and have one dialogue each; schedules and routines come later (375 open question on NPC routines). Dialogues are built-in files; editing them in Forge comes with the dialogue editor.
- Travel through a door reads the other area in the browser and shows it as it was read; the server holds the player's area when play moves to the server (S4).
- S4's fight runs in the browser and nothing of it is stored. The Node runner (346) takes it to the server, with the same `Combat` rules. Skills are fixed at the thimble's start until the character sheet (379) arrives.
- V3's pilot's browser decides where the ship is and the server keeps what it sends (one pilot at a time); the Node runner (346) takes the flight model to the server. Blocks are placed from the design for free; costing them from the pack comes with crafting (363).
- S5's night runs in the browser on a fixed timeline, nothing of it stored. The Hollowed need only come up and fight; the raid signature, drag marks and the watch's own choices come later (382, 383). One house only; a row of houses and watches placed by day come with 383.
- V4's power is counted in the pilot's browser and stored as one charge; a power network per grid (367a) replaces it. Refining is one run per command rather than a queue that works over time (368c). The block indices of a design's hold and refinery are written in the two blueprints, as the block counts are in `cmd-game-startShip`.
