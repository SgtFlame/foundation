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

## Built in V5 (forge 360)

The voxel game's fifth tracer bullet: crash a ship, lose blocks, repair it.

- **Crashes** (`ship/Ship.ts`, a thin 366b and 366e): a landing harder than 5 m/s along up breaks some of the blocks that struck the ground, the deepest first, and every one of them at 12 m/s or more. `ShipBody` refits at once (mass, thrust, gyroscope, power) and gives the broken indices through `takeBroken()`.
- **`game.damageShip {blocks}`** (`cmd-game-damageShip`) is sent by the pilot's window, like movement. Only the ship's pilot may send it, and indices not placed are ignored. Its `damaged` event takes the blocks out of `placed`, so they show in the outline again. A broken cockpit seats nobody.
- **Repair** is placing them again from the outline with `game.placeBlock`.
- **In the viewport**: pieces of each broken block fly off and fade (`ShipLayer.debris`), and the hint says how many broke. A smashed cockpit throws the pilot clear beside the ship. Stats add `crashes`.

## Built in V6 (forge 360)

The voxel game's sixth tracer bullet: an outpost, a quartermaster, contracts and skills (a thin 372 and the 350 candidates for quests and skills).

- **The outpost** (`outpost/Outpost.ts`): a beacon mast with a blinking lamp, a landing pad and the quartermaster stand on the ground at each moon's north pole. The server reckons the beacon at `(0, radius + amplitude, 0)`; a ship within 40 m of it counts as parked there.
- **Contracts** (`backend/features/game/contracts/contracts.json`, the `game.contracts` query): the quartermaster's name, title and line, and each contract's id, title, description, skill and experience. V6 has two: `first-flight` (bring a ship with every block placed; piloting 100 xp) and `beacon-iron` (hand over 1 m³ of iron from a ship's hold; prospecting 100 xp).
- **`game.handIn {contract}`** (`cmd-game-handIn`) targets the ship being handed in. It is refused when the ship is not parked at the beacon, the contract is unknown or already done by the sender, or its need is not met. Its `handedIn` event is folded twice: `game.ships` takes the iron out of the hold, and `game.players` (keyed `<worldId>.<userId>`) marks the contract done and adds the experience, so one event changes both.
- **Skills** (`game.player {world}`): `{done, xp, levels}`, a level per 100 xp from level 1. Each piloting level past the first makes thrusters answer a tenth harder (`thrustFor`, `ShipBody.thrustScale`).
- **In the viewport**: T beside the quartermaster opens the contracts panel (`data-contracts`), which says what each contract still needs from the ships parked at the beacon; Esc frees the mouse to click *Hand in* (`data-hand-in=<id>`). A level gained shows in the hint. Stats add `outpost` and `levels`.

## Built in V7 (forge 360)

The voxel game's seventh tracer bullet: a second player crews the ship (a thin 373).

- **`game.crewShip {aboard}`** (`cmd-game-crewShip`): boards a finished ship as its crew, or leaves it. Each design has one crew seat. It is refused for an unfinished ship, for its pilot, for someone already aboard, when the seat is taken, and for leaving a ship the sender is not aboard. Its `crewed` event keeps the ship's `crew` (user ids); whoever takes the pilot's seat leaves the crew seat.
- **The crew's job**: `game.refineShip` now also runs while the ship is flown when its sender is the crew, so one player flies and the other refines.
- **In the viewport**: B beside a finished ship boards it. The rider's camera follows the ship as others see it move (`ShipLayer.ride`), R refines and B leaves, standing on the ship's right (`ShipLayer.exitOf`). The pack panel shows the ship's line. Stats add `crew`.

## Built in V8, first part (forge 360)

The start of "the first hour above, end to end": a guide that leads a new player through V1 to V7's play.

- **`objectives/FirstHour.ts`**: `firstHour(input)` works out nine steps (set foot on the moon, dig, lay out a skiff, build it, fly it, report to the outpost, build a prospector, refine iron, bring iron for the beacon) from what the server already keeps (the pack, the world's ships, `game.player`) and what this window saw (walking, flying). A later step done counts the earlier ones, so the list only moves forward. Nothing new is stored on the server.
- **The panel** (`FirstHourPanel`, `data-first-hour`, a row per step with `data-step` and `data-done`) shows while walking, flying or riding, with the current step's how-to; J hides it. Stats add `firstHour`, the ids done.

## Built in V8, second part (forge 360)

The landing and the saved place.

- **`game.standAt {x, y, z}`** (`cmd-game-standAt`, on the world's terrain target): saves where the sender stands. Its `stood` event is folded by `game.players` into `at`, so `game.player` gives `at` once the player has landed and not before. Refused for a missing world or a point far from its bodies.
- **The drop pod** (`landing/DropPod.ts`): the first walk in a world with no `at` brings the player down in a pod that falls 160 m over 9 s onto the ground near the outpost, braking with its retro rocket at the end, watched from beside the landing point; the player steps out of the hatch and the place is saved. Later walks start where they last stood.
- **Saving**: while walking, the place is saved every 15 s when the player has moved 3 m and stands on the ground, and when the tab closes. Stats add `landing`.

## Built in V8, third part (forge 360)

A reason to leave the moon (the user, 2026-11-26: the moon has what a first ship needs, not what bigger ships need).

- **Ore** (`MATERIAL_ORE`, voxel material 2): `MoonParams.ore` (0 to 1) puts veins of ore in a body's rock; the moon has none and the asteroids `0.6`. Surface nets pass `(rocky + ore) / solid` per vertex, so the terrain material blends dust, rock and a blue-grey speckled ore. A dig's `took`, the pack (`game.pack`'s `ore`) and a ship's hold (`cargo.ore`) carry ore; fills never spend it; `game.loadShip` loads ore first.
- **Contracts with needs and unlocks** (`contracts/contracts.json`): each contract may give `needs` (`complete`, `iron`, `ore`), `design` (the ship it must be handed in from) and `unlocks` (a design). `game.handIn` reads them from one table, takes the iron and ore from the hold and records the unlock, which `game.players` folds into `unlocked` on `game.player`. Two new contracts: *Cobalt for the shipyard* (2 ore, Mining, unlocks the hauler) and *Ore haul* (12 ore, from a hauler, Mining).
- **The hauler** (`designs/hauler.json`): 58 blocks with six holds (36 m³) and a refinery; `game.startShip` refuses it until the sender has it unlocked, and the ship graphs know its holds, refinery and crew seat.
- **Names** (placeholders, the user, 2026-11-26): the farmer is Wenlan Harrow and the quartermaster Ines Varga.
- The first-hour guide has twelve steps: mine ore on an asteroid, hand in the cobalt, build a hauler.

## Built in S6 (forge 375)

The story game's sixth tracer bullet: the first hour end to end, with saves (the user, 2026-11-26: as written in the play design's first hour).

- **Saves**: `game.reachStory {step, area, x, z}` (`cmd-game-reachStory`) on the target `story.<areaId>`, where the story is known by its starting area (the farmstead). It is refused for a step not in the first hour (`StorySteps`: arrive, farmer, howl, signature, trail, fight, road, rest), a missing area, or a point outside it; an empty step saves only the place. `game.stories` folds it per player, keyed `<areaId>.<userId>`, and the `game.story {story}` query gives `{done, last, area, at}`. `game.storyLog` (keyed by target) is the command's own state.
- **`story/FirstHourStory.ts`**: the steps with their how-to (the ids are the server's; a test checks them), the raid signature's text, the drag marks (`dragMarks`), a howl made with Web Audio, and a lesser deck of five placeholder cards (`drawCard`, `showCard`).
- **The signpost** (farmland piece `signpost`, a door) stands where the north road leaves the farmstead (`Samples.SIGNPOST`) and leads to **Hawk's Rest** (`Samples.hawksRest`), an empty one-room cottage of the kitchen set.
- **In the viewport**: `playStory(place)` plays the first hour from the area shown, resuming from the saved place: the steps done, and the area and point last stood at (in another area, through `onTravel`). Talking to the farmer brings the first howl; the raid signature (a scorched ring and a dropped lantern by the well) and three furrows to the barn's door appear; standing by the signature reads it, reaching the barn shows the two Hollowed (hidden until then and gone after the fight) and starts the fight; going through the signpost reaches the road, and R by Hawk's Rest's hearth rests and draws the first card. Each step is sent through `onStory`, and the place every 15 s after moving 3 m and on Stop. The steps panel is `FirstHourPanel` below the moons; a caption tells the story at the bottom. Stats add `story`.

## Built in V9 (forge 360)

A thin first step off the moon to a planet with air (the user's progression, 2026-11-26: plant food, fish and hunt, build a cabin).

- **The planet** (`Bodies.PLANET`, `planetOf`): a body of kind `planet` in every world, 1600 m along x from the moon, 140 m in radius with gentle hills and no craters. Its ground is **soil** (`MATERIAL_SOIL`, 3; `MoonParams.soil`), which the mesher passes as a negative attribute, so the terrain material blends a grassy green; dug soil goes into the pack as dust. Its pull is its own (2.27 m/s² at the surface), and the moon's gravity test allows its far, tiny pull.
- **The sky**: from afar the planet is a sphere just under its lowest ground with a thin glow of air; going down into its air (60 m) turns the background blue and hides the stars (`stats().air`).
- **Crops**: `game.plant {x, y, z}` (`cmd-game-plant`) plants barley on the planet's surface, refused off it or within 1.5 m of another crop; `game.harvest {crop}` (`cmd-game-harvest`) takes a ripe one (90 s, `RipeSeconds`) as a ration of food, which `game.packs` folds (`Pack.Food`). `game.crops` keeps each world's crops, keyed `terrain.<worldId>`, and the `game.crops {world}` query lists them. `editTerrain`, `standAt` and `startShip` allow points near the planet.
- **In the viewport**: walking, P plants where the player looks on the planet's soil and H harvests the ripe crop beside them; crops grow from small and green to full and gold. The pack line shows food, and the first-hour guide adds *Grow food on the green planet* (thirteen steps). Stats add `crops`.

## Built in S7 (forge 375)

The story game's seventh tracer bullet: the first day after the first hour (the user, 2026-11-26: "both, a thin slice each").

- **Steps**: `StorySteps` gains `morning`, `forage` and `herbal`; `game.reachStory` takes `item` and `spot` (a herb gathered: `StoryItems` thyme, nettle, yarrow), refused for an unknown item, and `game.story` gives `items` (counts) and `picked` (the spots gathered, each counted once).
- **`story/FirstHourStory.ts`**: `HERBS` (title, colour, the herbal's line), `herbSpots(pieces, cell)` (one herb by each of the area's first three tree or copse pieces, in a fixed order, keyed by the piece's id) and `showHerbal` (the herbal page).
- **In the viewport**: once `rest` is done, play continuing (after any resume) reaches `morning`: day light, the day's card drawn and shown. In the story's starting area the herbs grow by the trees; walking within 2 m of one gathers it (sent through `onStory` with `item` and `spot`); all three reach `forage`, and K opens the herbal, which reaches `herbal`, the story's last step for now. Stats' `story` adds `items`.

## Built in V10 (forge 360)

The voxel game's tenth tracer bullet: the body's needs, a thin 362e (the user, 2026-11-26: full survival).

- **`survival/Needs.ts`**: `Needs {air, fed, health}` (0 to 100), `stepNeeds(needs, {breathable}, dt)` with the rates in `NEEDS` (a suit holds 240 s of air and refills at 25 a second; full to hungry in 30 minutes; no air costs 6 health a second, starving 0.25; health comes back at 0.5 a second while fed above 20), `eat` (a ration gives 35), `warning` and the `NeedsHud` (three bars at the bottom right).
- **In the viewport**: the needs run while the player walks, flies a ship or rides as crew. Air fills in the planet's air, aboard a ship and within 14 m of the outpost's quartermaster; E eats a ration from the pack (`ShipIntent` `eat`). At no health the player blacks out and wakes by the outpost with full air and 60 health. The needs go with each saved place (`stand` carries `needs`) and come back from `game.player`. Stats add `needs`, `breathable` and `blackouts`.
- **Server**: `game.eat {}` (`cmd-game-eat`, target the world) refuses an empty pack and emits `ate {pack: {food: -1}}`, which `game.packs` folds; `game.standAt` takes `needs {air, fed, health}` (each rounded and clamped), which `game.players` keeps as `needs` (a stand without them keeps the last).

## Built in S8 (forge 375)

The story game's eighth tracer bullet: the Hum, Chill and Hunger (the design's rules 2 and 3), using the herbs of S7.

- **`story/Hum.ts`**: `Body {hum, chill, hunger}`; `humPool(night)`, a thimble line's small pool set by the moons (1 in the greater moon's dark, up to 4 when it is full, one more at the lesser's full); `breathe` (stillness, one Hum a breath up to the pool), `spendHum` (the cadence: 40 Chill and 15 Hunger), `stepBody` (hunger grows over about 25 minutes, faster while chilled; the chill fades over 15), `DISHES` and `cook` (thyme in hot milk clears the Chill, nettle soup eases Hunger), the `BodyHud` (Hum pips, Chill and Hunger bars), the `BreathPrompt` (a ring swelling four seconds and falling six) and `showCooking`.
- **Combat**: `CombatOptions.cadenceAllowed` gates the Strike, Pivot, Guard cadence; in the story, once stillness is learned, each cadence needs and spends a Hum.
- **In the viewport**: after the herbal, B by a hearth starts stillness (moving, a fight or leaving the hearth breaks it; the first breath reaches `stillness`), and C opens the hearth's cooking (cooking reaches `cook` and spends the herb). The body is sent with every step and saved place (`onStory` `body`, `use`).
- **Server**: `game.reachStory` takes `use` (a herb spent, refused unless held: the graph reads `game.stories`) and `body {hum, chill, hunger}` (clamped); `StorySteps` gains `stillness` and `cook`; `game.story` gives `body` and counts a spent herb off `items`.

## Built in V11 (forge 360)

The voxel game's eleventh tracer bullet: the job board (the user, 2026-11-26: the job board asks for larger and larger ships, and some skills rise only from jobs).

- **Jobs** are contracts with `job: true` in `contracts/contracts.json`, done again and again: *Rock for the landing pads* (3 m³ of rock), *An ore run* (8 m³ of ore, more than a prospector holds), *Bulk ore for the smelter* (30 m³, Trade 2) and *Ore for the new station* (50 m³, Trade 3, only a freighter holds it). Each pays **Trade**, a skill no other work teaches; `level` is the Trade level a job needs.
- **`game.handIn`** (`cmd-game-handIn`) knows the jobs: it refuses a job above the sender's Trade level and checks rock in the hold; it emits `job: true` and `rock`, and once the Trade experience reaches 200 (level 3) it unlocks the **freighter**. `game.players` counts each job in `jobs` instead of marking it done; the ship's hold gives up the rock.
- **The freighter** (`designs/freighter.json`, 82 blocks, eleven holds): `game.startShip` refuses it until unlocked.
- **Engine**: `handInProblem` never calls a job done and checks its Trade level and rock; the quartermaster's panel shows a *Job board* section with how many times each was done, the skills line Trade, and scrolls when long.

## Built in S9 (forge 375)

The story game's ninth tracer bullet: the moon clock and the day and night loop (the design's rule 1; the play design's 20 to 30 minute days).

- **`story/Clock.ts`**: `Clock {day, hour}` from the night of the first snow (calendar night 11, 21:00); a game hour is a real minute (`SECONDS_PER_HOUR`), dawn at 6 and dusk at 19; `nightOf` (the small hours belong to the night before), `sleep` (wake at 7), `canSleep` (from 18:00), `clockText`, `skyNote` (moondark, the lesser's full and dark, waxing and waning, and the rare Doubling and Double Dark, both moons within half a night), `almanac(from, n)` with each night's Hum pool, and `showAlmanac`.
- **In the viewport**: the story's clock runs while playing; the light follows dawn and dusk, and the calendar night (and so the moons and the Hum pool) follows the clock. M opens the almanac (once cooking is done it reaches `almanac`); R by a hearth from 18:00 sleeps until seven, draws the day's card and, after the almanac, reaches `sleep`. The clock shows above the Hum and goes with every save (`onStory` `clock`).
- **Server**: `game.reachStory` takes `clock {day, hour}` (the hour kept within a day), `StorySteps` gains `almanac` and `sleep`, and `game.story` gives `clock`.

## Built in V12 (forge 360)

The voxel game's twelfth tracer bullet: fishing and hunting on the planet with air (the user's progression: plant food, fish, hunt).

- **The sea**: a sphere of water at `SEA_RADIUS` (the planet's radius less 2.5 m) fills its hollows (`wildlife/Wildlife.ts`, `seaMesh`).
- **Fishing**: N while looking at the water within 14 m casts a float (`Fishing`, a small state machine: a bite comes after 3 to 8 s and lasts 1.6 s); N again strikes. A strike in the bite catches a fish (one ration), too early comes up empty, too late the fish gets away.
- **The herd**: twelve grazers in three groups from the world's seed (`herdOf`, ids `g00` to `g23`), each walking a slow circle round its home (`grazerDir`); they show where the ground is above the sea, and run along their circle when the player comes within 9 m. V within 2.6 m hunts one (three rations).
- **`game.forage`** (`cmd-game-forage`, area `game`, target the world's terrain) takes `{kind: fish|hunt, animal, x, y, z}`: refused away from the planet (120 to 160 m from its centre), for an unknown grazer, or one taken less than 300 s ago; it emits `foraged` with `pack: {food}`, which `game.packs` adds to the sender's pack. The `game.wildlife` projector (keyed `terrain.<world>`, folding **every** terrain event so the command's expected sequence holds) keeps `{taken, fish, hunted}`; the live `game.wildlife {world}` query sends it to every player, so a grazer one person hunts is gone for everyone until it comes back.
- The first hour's *Grow food* step counts only once the hauler is built, so a fish never finishes the guide.

## Built in S10 (forge 375)

The story game's tenth tracer bullet: a three-card reading for Wenlan Harrow (the design's rule 7 and the play design's card readings; the quest is the design's *a farmer's winter wheat is drowning and a card reading shows the blocked ditch*).

- **`story/Reading.ts`**: `WENLAN_READING`, three placeholder cards of the lesser deck (past *The Plough*, present *The Willow*, future *The Ash*), each with a faint and a clear vision; `clarity(night, stillness)` from the lesser moon's light (at least a half once stillness is learned), `vision`, `wenlanAnswer`, `ditchSpot` (the low south-east corner of the field piece furthest east) and `ReadingScene`, the cards laid face down and turned left to right by a click, then the choice of what to tell: *all of it* (the ditch and the warning of the last card) or *only the ditch*.
- **In the viewport**: once the first day is over (`sleep`), clicking Wenlan opens the reading instead of his talk; the choice reaches `reading` with `told`. On the farmstead a willow bough lies across the culvert at the east field's low corner and water stands over the barley; walking up to the bough clears it and reaches `ditch`, and Wenlan pays a silver.
- **Server**: `StorySteps` gains `reading` and `ditch`; `game.reachStory` takes `told` (`all` or `ditch`), kept as `told` in `game.story`, and the first `ditch` adds one `silver` to `items`.

## Built in V13 (forge 360)

The voxel game's thirteenth tracer bullet: a cabin on the planet with air (the user's progression: a cabin), and the engine's first **buildings**.

- **Static designs**: a `ShipDesign` with `static: true` is a building: no cockpit (`ShipDesign.Check`), never flown, and its placed blocks are **solid to the walker** (`buildingDistance` in `ship/Ship.ts`, a box distance per placed block, taken as the lesser with the terrain's in the viewport's walker terrain), so walls stop the player and a roof holds them up; `door` blocks stay open. New block types `log`, `door` (an open frame) and `bed` (a slab half a block high with a pillow).
- **The cabin** (`designs/cabin.json`, 74 blocks): walls three logs high with a doorway at the front, a roof with a lamp, and a bed (block 73). `game.startShip` builds it only on the planet (120 to 160 m from its centre).
- **`game.sleep`** (`cmd-game-sleep`, target the cabin) refuses a ship that is not a cabin or a cabin without its bed; it emits `slept`, which `game.players` folds into `home` and full health. In play, F by the bed sleeps (*F: sleep in the bed*), and a blackout wakes the player by their home's bed instead of at the outpost.
- Fixed on the way: a player who has only stood or slept somewhere has no `levels`, which broke the viewport's progress (it now fills the empty maps).
- Known: a building is placed as a ship is (ahead of the player, its lowest blocks half sunk), so on a slope its downhill side floats a little, and a second building started where one stands goes on its roof.

## Built in S11 (forge 375)

The story game's eleventh tracer bullet: the day's card gives a light buff or a warning (the design's rule 7: "a daily one-card draw gives a tone for the day").

- **`story/DayCard.ts`**: `dayCard(seed, day)` is the card of a story's day (the card the morning and sleeping show, `drawCard(seed + day)`, so nothing new is saved), with its `CardEffect`: *The Hearth* makes hot food do half again as much, *The Lantern* lights what is hidden nearby (a soft column of light over unpicked herbs and the willow bough), *The Hare* walks a quarter faster, *The Frost* is a warning (a spent Hum chills half again as much), *The Wren* adds one Hum to the sky's pool. `spendWith` and `cookWith` apply it.
- **In the viewport**: the walking speed, the cadence's Hum, cooking and the Hum pool follow the day's card; the body HUD shows it beside the clock (*Day 4 · 09:01 · The Lantern: what is hidden nearby glows*), and sleeping names it.
- `drawCard` now mixes its seed fully, so days in a row draw varied cards (before, neighbouring seeds often drew the same one).

## Built in V14 (forge 360)

The voxel game's fourteenth tracer bullet: the crafting ladder's first rung (the play design's *hand → field bench*) and its first moment, "crafting the first drill and tearing through rock that took minutes by hand".

- **Digging on foot depends on tools** (`craft/Crafting.ts`, `digLimits`): bare hands dig a brush of at most 1.25 m, a bite every 0.35 s; with a **hand drill** up to 4 m, a bite every 0.12 s. The hint says *Left: dig by hand* or *Left: drill*. Flying the camera digs as before (up to 6 m).
- **The field bench**: a new block type `bench` (a table on four legs with a vice), block 74 of the cabin (now 75 blocks). In play, U at a placed bench crafts (*U: craft a hand drill (2 m³ ore, 1 m³ rock)*), refused with the reason when the pack lacks the materials.
- **`game.craft {item}`** (`cmd-game-craft`, target the cabin) refuses a ship that is not a cabin, a cabin whose bench is not placed, any item but `drill`, a drill the sender already has, or a pack without 2 m³ of ore and 1 m³ of rock; it emits `crafted {world, item, pack}`, which `game.packs` folds (the materials spent) and `game.players` folds into `tools: {drill: true}`. The recipe is in the engine's `RECIPES` and the graph alike. `TestCraftDrill` covers it.

## Built in S12 (forge 375)

The story game's twelfth tracer bullet: the senses layer (the design's rule 6), smells and sounds shown as soft cues at the screen's edge, pointing toward their source without a minimap marker.

- **`story/Senses.ts`**: a `Source` (`sense` smell or sound, `text`, `from`, `range` in metres); `senseCues(x, z, sources, focus)` gives at most three cues within reach but not at hand (3 m), strongest first, each with its direction; `edgePoint` puts a cue on a box inset from the screen's edges along the direction from the screen's centre to its source's projection, and `spread` moves overlapping cues apart. `SensesLayer` draws them (`data-sense`), faded by strength.
- **Focus**: holding F carries every sense 1.75 times as far, and the clearest cue names where it comes from (*Burnt sugar and old iron, from the barn*). The hint says *hold F to focus your senses* once the howl is heard.
- **The first hour's sources** (`AreaViewport.senseSources()`): after the howl, *old pennies, and scorched snow* from the raid signature by the well; after the signature, *burnt sugar and old iron* from the barn and, at night, *the owls have gone silent*; on the first morning each unpicked herb (*Wild thyme, green on the air*); after the reading, *water, choked and gurgling* from the culvert.
- Engine only: senses are not saved, and the server is unchanged.

## Built in V15 (forge 360)

The voxel game's fifteenth tracer bullet: scavenging (the play design's "wrecks hold salvageable blocks; tearing down a block yields part of its materials, so salvage and mining feed the same chain").

- **Two wrecks** (`wreck/Wreck.ts`): derelict skiffs `k0` and `k1` lie half buried on the moon, 30° to 45° from its top on opposite sides (a walk of about 50 to 80 m from the outpost), placed from the world's seed (`wreckSites`) so every player sees them in the same place; drawn from the skiff's design in rust colours, each block a little askew. They are scenery to the walker (not solid yet).
- **Salvage**: by a wreck, Z tears off the nearest block still there (`nearestSalvage`, within 2.6 m; *Z: salvage scrap from the wreck*), and its scrap goes into the pack as 0.25 m³ of **ore**, so a wreck feeds the refinery and the drill's recipe as an asteroid does.
- **`game.salvage {wreck, index, x, y, z}`** (`cmd-game-salvage`, target the world) refuses a point off the moon, an unknown wreck or block (0 to 26), a block already salvaged or a full pack; it emits `salvaged`, which `game.packs` folds (the ore) and the new `game.wrecks` projection folds per world (`{salvaged: {<wreck>.<index>: true}, count}`; it folds every event on the terrain target, so the command's expected sequence is the target's). The `game.wrecks` query keeps the World tab current: a block salvaged is gone for everyone. `TestSalvage` covers it.

## Built in S13 (forge 375)

The story game's thirteenth tracer bullet: the clue journal (the play design's investigation: "a clue journal collects people, places and evidence; some entries can be linked to draw a conclusion, which opens the next step").

- **`story/Journal.ts`**: `journalEntries(done)` gives the notes the story has reached (Wenlan Harrow, Hawk's Rest, the raid by the well, ember eyes in the barn, the moon almanac, the Ash card); `link(a, b, night)` gives what two notes conclude, or null; `nextNoMoon(night)` is the next night with the greater moon dark and the lesser all but dark. `JournalScene` is the notes' page in the herbal's style (`data-note`, `data-conclusion`).
- **N** opens the notes once Wenlan's ditch is clear (before, *Nothing to write down yet*). Clicking two notes links them: the Ash card with the moon almanac concludes the night the eyeless ones come back (*Day …, the next night with no moon*, both moons new), the story's new last step **`foretold`**; the raid with the ember eyes concludes the Hollowed took the Brannocks (a note only); other pairs *do not fit together*.
- `game.reachStory` takes the step `foretold`; the Area tab's *Play the story again* now follows it.
- Known: only the conclusion that is a step is saved, and its night is worked out again from the story's clock when the notes open.

## Built in S14 (forge 375)

The story game's fourteenth tracer bullet: the night the notes foretold, played as S5's hearth defence at Wenlan's farmhouse, so the reading, the investigation and the defence join into one thread.

- New last steps **`watch`** (the foretold night begun) and **`defended`** (the house held until dawn); `game.reachStory` takes both.
- After `foretold`, R by a hearth waits for the dark night: the clock jumps to 20:00 of the next night with both moons new (`nextNoMoon`, stricter since S14 so the moon HUD agrees), Wenlan bars the byre, and the viewport asks its host to start the night (`AreaViewportOptions.onStoryNight`; the Area tab starts `startHearthNight` at the farmhouse, as its *A night of hearth defence* button does).
- At dawn the house held reaches `defended` and Wenlan's thanks; a house lost says so, and R by a hearth waits for the next dark night to try again.
- The Area tab's *Play the story again* now follows `defended`.

## Built in V16 (forge 360)

The voxel game's sixteenth tracer bullet: the first threat on a schedule (the play design's base defence: "a swarm surge when the Silent Fleet's drones wake on a cycle, shown in the HUD ahead").

- **The schedule** (`surge/Surge.ts`): a surge every 12 minutes of real time (`SURGE_PERIOD`; index = Unix ms / 720 000), lasting 75 s, the same for every player. On the moon a pill at the top of the view counts down (*Next swarm surge in 8:41*, amber in the last minute) and, during one, counts the drones left.
- **Drones**: eight a surge (`dronesOf(index)`), each waking at its own time, appearing 30 m up and 30 m off on its side of the sky and diving at the player at 6 to 9 m/s. One that reaches you takes 8 health (`DRONE_HIT`) and is spent. They are glowing red octahedrons, each player's own (they chase whoever sees them).
- **Q shoots** a bolt along the look (`shotHits`: within 1.1 m of the line, 45 m range): the drone falls and gives 0.1 m³ of ore scrap, so surviving a surge pays in salvage.
- **`game.downDrone {surge, drone}`** (`cmd-game-downDrone`, target the world) refuses a surge that is neither the current nor the last one, a drone outside 0 to 7, one the sender already downed or a full pack; it emits `downed`, which `game.packs` folds and the new `game.surges` projection keeps per world (`{downed: {<surge>.<drone>.<userId>: true}, count}`, a query too). `TestDownDrone` covers it.
- Known: drones are not shared between players and pass through walls; there are no turrets or shelters yet.

## Built in V17 (forge 360)

The voxel game's seventeenth tracer bullet: the base defence's second step, a turret that holds the line while you work.

- **The recipe**: the cabin's field bench now knows two recipes (`craft/Crafting.ts` `RECIPES`, `nextRecipe`): the hand drill, then the **sentry turret** (2 m³ of ore and 1.5 m³ of rock, so it fits one pack). U at the bench makes the next one not made yet. `game.craft {item: "turret"}` decides it (`cmd-game-craft`), and `game.player` then gives `tools.turret`.
- **Setting it down**: on the moon, Y sets your one turret down 3 m ahead of you, or moves it there. `game.placeTurret {x, y, z}` (`cmd-game-placeTurret`, target the world, projection `game.surges`) refuses a player without a turret or a point off the moon (more than 130 m from its centre) and emits `turretPlaced`; `game.surges` keeps the turrets by user id (`turrets: {<userId>: {x, y, z}}`), so every player sees every turret.
- **Firing**: during a surge each turret turns its head to the nearest drone within 28 m (`turretPick`, `TURRET_RANGE`) and shoots every 2.2 s (`TURRET_RELOAD`), a yellow tracer. A drone a turret downs counts as yours (`game.downDrone`), so its scrap goes to your pack.
- `TestTurret` covers crafting, the refusals and moving the turret.
- Known: drones are still each player's own, so a turret fires in each player's view at that player's drones; turrets cannot be damaged and need no power or ammunition.

## Built in S15 (forge 375)

The story game's fifteenth tracer bullet: the village remembers the night you held Wenlan's house.

- New last step **`thanked`**, which `game.reachStory` takes. After `defended`, clicking Wenlan opens a new dialogue, `farmer-thanks` (`dialogues/farmer-thanks.json`, placeholder lines): the night behind the barred door, the Brannocks, and a gift.
- **The knot of the last sheaf**: when that talk ends, Wenlan has given you a knot of straw cut from the last sheaf over his hearth. `game.stories` gives `items.knot` once when `thanked` is first reached (as `ditch` pays the silver). While you carry it, each Hum spent leaves a quarter less Chill (`KNOT_CHILL` 0.75, applied over the day's card in `dayEffect`).
- **Word travels**: arriving at Hawk's Rest by the signpost after `defended`, the village greets you (a loaf and a cup of milk on the step, a nod at the well), once a session.
- The dawn line now sends you to find Wenlan in the kitchen.

## Built in V18 (forge 360)

The voxel game's eighteenth tracer bullet: a manual found while scavenging teaches a recipe outright (the play design's *perks and manuals*), and the mining ladder's next rung, the powered drill.

- **The manual**: block 13 of each moon wreck (`ManualBlock`, the engine's `MANUAL_BLOCK`), a cockpit plate, glows faintly green through its seams. Salvaging it with Z gives its scrap and a fabricator's manual: `cmd-game-salvage` adds `manual: "powerDrill"` to the `salvaged` event, and `game.players` folds it into `manuals` (`game.player` gives `manuals: {powerDrill: true}`). Each wreck holds one, so two players can each find one.
- **The powered drill**: the bench's third recipe (`RECIPES` `powerDrill`, with `manual` and `builtOn: 'drill'`; `nextRecipe(tools, manuals)` offers it only once the manual is found and the hand drill made), 2.5 m³ of ore and 1 m³ of rock. `game.craft {item: "powerDrill"}` refuses a sender without the manual or a hand drill. With it, digging on foot takes up to a 6 m brush every 0.07 s (`digLimits`).
- Before the manual, the bench says *a wreck may hold a manual for more*.
- `TestPowerDrill` covers it.

## Built in S16 (forge 375)

The story game's sixteenth tracer bullet: Remembering (the play design's reference rule 8: "sleeping in blessed places unlocks a skill or a memory scene outright. Dreams replay past-life fights; wounds taken there ache until a warm, dreamless night").

- **A blessed place**: the empty cottage at Hawk's Rest (an interior of another area than the story's, with nobody in it; `AreaViewport.blessed()`). After `thanked`, sleeping by its hearth (R after 19:00) brings **the dream of the ford** (`story/Remember.ts` `FORD_DREAM`, shown a line at a time on a dark panel, `showDream`): an older self holding a frozen ford with a long spear, raked in the side. Placeholder text, invented for the slice.
- **What it gives**: the new last step **`dream`** (`game.reachStory` takes it). The character's polearm skill rises by one for the story's fights (`remembered(skills, done)`, applied when a fight starts).
- **The dream wound**: `game.stories` gives `items.wound` when `dream` is first reached. While it aches the Chill never fades below 30 (`aching`, `WOUND_CHILL`). A night's sleep after eating something hot (cooking at a hearth since waking) is a warm, dreamless night: it heals the wound (`use: "wound"`).
- The Area tab's *Play the story again* now follows the last of `STORY_STEPS`, so a new step never cuts the story short.

## Built in V19 (forge 360)

Working the ground by holding (the user, 2026-11-26: "use a tool to harvest resources over time by holding down the action button", as in Space Engineers and 7 Days to Die). Engine only; the server's commands are unchanged.

- **Mining** (`craft/Mining.ts`): on foot, holding the dig button on a spot works it. Each material takes work per m³ by its hardness (`HARDNESS`: dust 1, rock 3, ore 5), and the tool in hand brings work each second (`toolPower`: bare hands 1, hand drill 6, powered drill 16). When the bite under the brush has had its work (`workFor` over `VoxelWorld.contentsOf`), it comes away as one `game.editTerrain`, and the next bite starts. A bigger brush is a longer hold; turning to another spot starts over (`Hold`, `spotKey`). A ring around the crosshair fills in the colour of what is mostly there (ore blue, rock tan, dust pale), with *drill · rock · 1.4 s* under it, and chips fly from the tool.
- **Hold actions**: holding Z cuts a wreck's plate free (3 s by hand, under a second with the powered drill), and holding H cuts a ripe crop (1.5 s), with the same ring.
- Filling (right button) and digging from the flying camera act at once, as before.

## Built in S17 (forge 375)

The story game's seventeenth tracer bullet: the first companion (the play design's party, one companion at the start by the user's rule; companions are kinds of people the reference describes, never the book's cast).

- **Corran Vey, a veteran of the Drum** (`story/Companion.ts` `VETERAN`, `dialogues/veteran.json`; placeholder name and lines). The morning after the dream, a knock at the cottage door at Hawk's Rest opens his talk; when it ends he joins, the new last step **`companion`** (`game.reachStory` takes it).
- **Following**: out of a fight he keeps a step behind the character (`companionStep`), and comes along through doors.
- **Fighting**: in a fight he goes for the character's target, or the nearest foe, from its flank (beside it, square to the character), and strikes with a pike every 2.6 s with polearm 3. `Combat.allyStrike` decides it as the character's own strikes are decided (hit chance, then a vital chance with the pike's aim and the flank), so he can behead or pierce a Hollowed's heart; his strikes show as *pike* over the foe and a line in the fight panel.
- The fight panel now shows the skills the story has given (`remembered`).

## Built in V20 (forge 360)

The forge: ore becomes iron over time, the second rung of the crafting ladder after the bench.

- The cabin gains a forge (block 75, `forge`; 76 blocks now). A cabin started before keeps working: `game.placeBlock` lets a cabin place up to the design's 76, and the fold raises `blocks`.
- `game.smelt` (`cmd-game-smelt`, target the cabin): puts up to 2 m³ of the pack's ore in the forge (event `smelting {world, ore, at, pack}`), refused while it holds ore. The cabin keeps it as `forge {ore, at}` on `game.ships` and `game.worldShips`.
- `game.takeIngots` (`cmd-game-takeIngots`): once 45 s a m³ have passed (`SmeltMs`), takes 0.8 m³ of iron a m³ of ore (`IronYield`) into the pack (event `ingots {world, iron, pack}`); refused while smelting or without room. The pack gains `iron`, which counts against its 4 m³ in `game.editTerrain`, `game.salvage` and `game.downDrone`.
- Engine: `craft/Forge.ts` (`forgeAction`, `smeltLeft`, `ironFrom`), the forge's mesh (a stone furnace with a glowing mouth and a chimney), `ShipLayer.forgeNear`, and U at the forge in `VoxelViewport` (`forgeAt`, `forgeHint`: "The forge is smelting: n s left"). `TestForge` covers it.

## Built in S18 (forge 375)

The Nine Sisters†, the design's ring of nine lichened standing stones where "the ground remembers", a ritual site the moons wake (the play design: the moons set "when the Nine Sisters wake").

- **The ring** (`story/Stones.ts`): nine stones crowned with pines on the free 3 × 3 cells nearest the signpost (`ringSpot`, `stonePoints`), shown in the story's area once the veteran has joined. When he joins he tells of them: they wake when the little moon is full.
- **Waking**: on a night the lesser moon is more than 90 % lit (`stonesWake`, `nextWaking`), a pale ring glows on the ground, the caption says something hums by the signpost, and the senses layer hears it.
- **Remembering**: at the ring's heart on such a night, the ground gives up a memory (`STONES_MEMORY`, shown like a dream): the queen of the First Flight, silver charms in her braids, naming the winter stars for a child and telling her the Wanderer is a world, not a star. It is the new last step **`stones`** (`game.reachStory` takes it), and `game.story`'s `items.starlore` is 1. On other nights the ring says how many nights until the lesser moon is full.
- **Star lore**: on nights the lesser moon is more than half lit, the Hum pool holds one more (`starlorePool`).

## Built in S19 (forge 375)

The hedge-witch's cache, from the design's Hawk's Rest (a hedge-witch's cottage with a hidden floor cache).

- **Finding it** (`story/Cache.ts`): once the Nine Sisters have remembered (`stones`), the senses layer in the empty cottage at Hawk's Rest hears *a draught, and a board that knocks hollow* by the box bed (`cacheSpot`, `cacheReady`). Walking up to the bed lifts the loose board: the hedge-witch's notes (`CACHE_NOTES`, shown like a dream) and the new last step **`cache`**, with `game.story`'s `items.hedgecraft` 1.
- **The yarrow poultice** (`POULTICE`): with the notes, C at a hearth also offers a poultice from the yarrow gathered on the first morning (until now unused). It takes 40 Chill off and closes the dream's wound at once (`use: "wound"`), without waiting for a warm night. `showCooking` takes the dishes it offers.

A note from the user on S18 (2026-11-27): in the books the Nine Sisters wake when an Elder is near (Kasimir, Anna, the Stranger); the lesser full moon is a second way to wake them, which does not conflict.

## Built in V21 (forge 360)

The ore scanner: the first thing made from the forge's iron.

- `game.craft` knows a fourth recipe, `scanner` (1 m³ of iron and 0.5 m³ of ore from the pack); the refusal names iron when the pack is short ("a scanner takes 1 m³ of iron and 0.5 m³ of ore from your pack"). `game.player` then gives `tools.scanner`.
- Engine only for the pulse: `craft/Scanner.ts` (`scan`, `scanLine`) samples the voxel world on a 2.5 m grid within 36 m, gathers solid ore samples into 8 m pockets and returns the nearest six. In `VoxelViewport`, K with a scanner sends a pulse (6 s to recharge), draws a wireframe octahedron through the rock at each pocket, sized by how rich it looks, for 12 s, and says how many pockets it found and how far the nearest is. `Recipe.needs` and `Carried` take `iron`; `missing` says "an" before a vowel.
- `TestForge` checks the scanner's refusal; `Scanner.test.ts` and `Crafting.test.ts` the rest.

## Built in S20 (forge 375)

The hedge-witch's bundles and her moon calendar, from the design's Hawk's Rest ("overgrown herb garden, moon-calendar stones on the sill") and its herb list ("lavender, mugwort, yarrow, valerian, feverfew, borage, sage: gardens and dried bundles").

- **The bundles** (`story/Bundles.ts`): once her notes are found (`cache`), four bundles hang drying over the cottage's shelves, two to a shelf (`bundleSpots`, spots `bundle-<herb>`): lavender, mugwort, valerian and sage (`BUNDLE_HERBS`). The senses smell each; walking up to one takes it into the herbal (an `item` save, like the first morning's herbs), and all four reach the new last step **`bundles`**. The herbal lists them once the cache is open (`showHerbal`'s `more`). `game.reachStory` accepts the four herbs, and `StoryItems` lists them.
- **Two recipes from her notes** (`HEDGE_DISHES`, offered with hedge-craft): *Valerian and lavender tea* (60 Chill off) and *Sage broth* (45 Hunger and 20 Chill off). Lavender and mugwort are kept for the dream tincture still to come.
- **The moon calendar**: pebbles on the cottage's window sill (`sillSpot`); standing by them says how many nights until the lesser and the greater full moons (`sillLine`, `nextGreaterFull`, the stones' `nextWaking`).
- `TestStory` gathers a bundle, reaches `bundles` and cooks the tea; `Bundles.test.ts` covers the rest.

## Built in V22 (forge 360)

The iron air tank: the suit's first upgrade (the play design: gear answers the environment; a cheap suit forces short trips).

- `game.craft` knows a fifth recipe, `airTank` (1.5 m³ of iron and 0.5 m³ of rock). The refusal now names every material the item needs, iron first, joined with *and* (`join(filter(…))` in `cmd-game-craft`), so the older messages read as before.
- Engine: `survival/Needs.ts` `ratesFor(tools)` gives the suit's rates, with `AIR_TANK_SECONDS` (480, twice the plain suit's 240) when the player has the tank; `VoxelViewport` steps the needs and shows the air warning ("Air low: n s left") with them. The bench's hint once everything is made says so.
- `TestForge` checks the refusal; `Crafting.test.ts` and `Needs.test.ts` the rest.

## Built in S21 (forge 375)

The dream tincture, from the design's recipes ("Dream tincture. Made only at the full of the lesser moon. Brings true dreams. Ingredients not given").

- **Making it** (`story/Tincture.ts`): on a night of the lesser full moon, with her notes (`items.hedgecraft`) and a mugwort and a lavender from her bundles, C at a hearth offers the *Dream tincture* (`TINCTURE`, `tinctureReady`). It spends both herbs (`use: "mugwort"`, then `use: "lavender"`).
- **The true dream**: the next sleep (R), wherever it is slept, plays `TRUE_DREAM` and reaches the new last step **`truedream`**; `game.story` then gives `items.wardlore` 1, the lore of the design's threshold ward (sage along the sill and five stones in their order), for a later step to lay. The dream's scene is a placeholder until the past lives are written, as the user asked (2026-11-27).
- `TestStory` reaches `truedream`; `Tincture.test.ts` checks when it can be made.

## Built in V23 (forge 360)

The fabricator: the third rung of the crafting ladder ("fabricator (powered; electronics, thrusters, advanced blocks)"), thin: power from a solar panel and one product, the circuit board.

- The cabin gains a **fabricator** (block 76, type `fabricator`, across from the forge) and a **solar panel** on the roof (block 77, `solar`); 78 blocks now. `game.placeBlock` lets a cabin place up to 78, so cabins started before can add them.
- `game.fabricate` (`cmd-game-fabricate`, target the cabin): refused until both are placed ("the fabricator has no power: place the solar panel on the roof"), while it is at work, or without 0.5 m³ of iron and 0.25 m³ of ore in the pack; otherwise event `fabricating {world, item: "circuit", at, pack}`, kept as `fab {item, at}` on the ship.
- `game.takeFabricated` (`cmd-game-takeFabricated`): after 30 s (`FabMs`), event `fabricated {world, item, pack: {circuits: 1}}`. The pack gains `circuits`, a count that takes no room (at most 20).
- Engine: `craft/Fabricator.ts` (`fabAction`, `fabLeft`), the fabricator's mesh (a grey cabinet with a green screen and a tool arm), `ShipLayer.fabricatorNear` and `hasPlaced`, and U at the fabricator in `VoxelViewport` (`fabAt`, `fabHint`), the forge taking U when nearer. Circuit boards are for what the fabricator builds next (thrusters, bigger ships).
- `TestFabricator` checks the refusals and `TestCabin` the design; `Fabricator.test.ts` the engine's view.

## Built in S22 (forge 375)

The threshold ward, from the design's hedge-craft ("Wards. Arrangements of herbs and stones on a threshold so nothing unwelcome crosses") and its blessed places ("Sleep there is dreamless and healing").

- **The river stones** (`story/Ward.ts`): after the true dream (`truedream`), five pale stones lie in the cleared culvert by the east field (the ditch's spot), heard as *clean water running over stones*. Walking up to them takes them (`item: "riverstones"`, spot `culvert-stones`; `riverStonesReady`).
- **Laying it**: at Hawk's Rest's door (`thresholdSpot`, the door's cell) with the stones and a sage, the ward is laid: the new last step **`ward`**, whose fold spends a sage and the stones and gives `items.ward` 1. Without them the door says what is missing (`wardMissing`). The ward shows as a line of sage and five stones on the threshold.
- **A warded night**: sleeping at a warded blessed place clears the Chill by morning (`wardedNight`) and closes a dream's wound, warm food or not.
- `TestStory` lays the ward; `Ward.test.ts` covers the rest.

## Built in V24 (forge 360)

Ion thruster kits: the fabricator's second product, the first ship part, a step towards the bigger ships the job board needs.

- `game.fabricate` takes `item`: `circuit` (the default, as in V23) or `ionKit`, 1 m³ of iron and 2 circuit boards, 60 s. `game.takeFabricated` gives a kit into the pack's `kits` (a count).
- `game.fitKit` (`cmd-game-fitKit`, target a ship): fits a kit from the pack, event `kitFitted {world, boost: 0.25, pack: {kits: -1}}`; the ship keeps `boost` (at most 0.5, two kits). Refused for a building, a ship with two, or a pack without a kit.
- Engine: `craft/Fabricator.ts` gains the kit (`KIT_MS`, `KIT_IRON`, `KIT_CIRCUITS`, `KIT_BOOST`, `MAX_BOOST`; `fabAction(…, item)`); at the fabricator U makes a circuit board and I a kit, the hint offering what the pack can make; by a ship, O fits a kit (`canFitKit`, `fitKit`). A ship's thrust is the pilot's skill times 1 + its boost (`thrustOf`).
- `TestFabricator` checks the refusals; `Fabricator.test.ts` the kit's timing and needs.

## Built in V25 (forge 360)

The ore barge: the job board's biggest ship, the first that needs ion thruster kits.

- `designs/barge.json` (112 blocks): the freighter lengthened by five rows to 21 cargo holds (126 m³), with no more thrusters. A design's `lift` (`ShipDesign.Lift`, the barge's 0.5) is the share of its holds its stock thrusters lift; each kit's boost adds to it, so two kits lift a full hold.
- `game.startShip` takes `barge` once the player's Trade reaches 4 (`game.handIn` unlocks it at 300 trade xp, as it unlocks the freighter at 200).
- `game.loadShip` loads the barge up to `holds × 6 × min(1, lift + boost)` and then refuses with *the barge's stock thrusters lift only N of its M m³: fit ion thruster kits*. It also loads the freighter's eleven holds, which were missing from the blueprint since V11 (the *Ore for the new station* job could not be done).
- A new job, *Ore for the refinery* (`refinery-ore`, 100 m³ of ore, Trade 4, 200 xp), needs a barge.
- Engine: `liftOf(design, boost)` in `ship/Ship.ts`; a ship's line shows what its thrusters lift when that is less than its holds.
- `TestBarge` checks the unlock, the lift refusal and the freighter's holds; `Ship.test.ts` the lift and that the empty barge hovers.

## Built in S23 (forge 375)

What the ward keeps out: the night the threshold ward is tried.

- Step `tried` (Go `StorySteps`, engine `STORY_STEPS`, the reachStory blueprint's list); the fold gives `items.hearthlore`.
- `story/Tried.ts`: `triedReady(done)` (the ward laid, the night not yet kept), `TRIED_NIGHT` (one raider, 70 s to dawn) and the night's lines.
- Once the ward is laid, R by Hawk's Rest's hearth waits for the next night with no moon (`wardNight`, 23:00 that night): the ward holds the door (*the scratching stops dead at the ward*), and a night of hearth defence (S5's `HearthNight`) starts at Hawk's Rest itself, with nobody keeping the watch but the character: one Hollowed comes up through the hearthstone. Held until dawn, the step is reached (*A hearthstone is a threshold too*); left alone, the cottage is lost and R waits for the next dark night.
- `story_test` checks `tried` and the hearth lore; `Tried.test.ts` when the night is ready.

## Built in S24 (forge 375)

The hearth ward: the hearth lore (S23) put to use.

- Step `hearthward`; the fold spends a sage and the river stones and gives `items.hearthward`.
- `story/HearthWard.ts`: `hearthStonesReady` (the culvert gives five more stones once `tried` is reached, spot `culvert-stones-2`), `bankSageReady` and `bankSageSpot` (sage seeded on the ditch bank beside the culvert, spot `ditch-sage`), `hearthWardReady`, `hearthWardMissing` and the lines. Both show in the senses layer (*Clean water running over stones*, *Sage, sharp and green*).
- Standing on Hawk's Rest's hearthstone (`hearthOf(area).stone`, S5's) with both lays the ward: a frame of sage round the stone and five stones at its corners and heart.
- `story_test` checks the two pickings and the step; `HearthWard.test.ts` the conditions.

## Built in V26 (forge 360)

Cargo weighs in flight: the barge's lift (V25) as physics, not only a loading rule.

- `ship/Ship.ts`: `CARGO_KG` (300 kg a m³) and `cargoOf(state)`; `ShipBody` takes `load` (m³) and adds it to its mass (`setLoad`), and `canLift(g)` says whether its thrusters, at the pilot's scale (skill times 1 + kits' boost), climb against a pull. `ShipLayer.sit` weighs the hold when a ship is taken.
- At 300 kg a m³ the ore barge climbs off the moon with half its hold on its stock thrusters, and with a full hold only with two kits; the freighter climbs full off the moon, the hauler and prospector full off the planet with air. A full barge cannot climb off the planet (2.27 m/s²) even with kits.
- While piloting, the ship's line says *too heavy to climb here* when the pull is more than its thrusters give.
- `Ship.test.ts` checks the weights and each ship's climb.

## Built in S25 (forge 375)

Wenlan's hearth ward, and what a warded hearth does on a dark night.

- Step `farmward`; the fold spends a sage and the river stones and gives `items.farmward`.
- `story/HearthWard.ts`: `nextHearthWard(done)` (Hawk's Rest's, then Wenlan's) and `wardSpots(ward)` (`culvert-stones-3`, `ditch-sage-2` for the second), so the culvert and the bank give a third handful; `hearthWarded(done, house)` (Hawk's Rest, a house with nobody in it, once `hearthward`; a house with Wenlan or a watcher once `farmward`).
- In Wenlan's kitchen (an interior with the farmer in it), standing on the hearthstone with both lays the ward, drawn as at Hawk's Rest.
- `HearthNight` takes `warded`: the stone stirs once (event `settled`) and nothing comes through, so the house holds until dawn. `startHearthNight` sets it from `hearthWarded`.
- Tests: `story_test` (`farmward`), `HearthWard.test.ts`, `HearthNight.test.ts` (a warded night).

## Built in V27 (forge 360)

The jump drive: the first part made from something the moon's ground does not give.

- **The jump coil**: block 26 of each moon wreck is its drive core (`DRIVE_CORE_BLOCK` in `wreck/Wreck.ts`); salvaging it (`cmd-game-salvage`) also puts a jump coil in the pack (`Pack.Coils`), so a world holds two.
- **Making it**: `game.fabricate {item: "jumpDrive"}` takes a jump coil, 1 m³ of iron, 1 m³ of ore and 2 circuit boards, and 90 s (`DRIVE_MS` in `craft/Fabricator.ts`, which now has `fabTime(item)`); `game.takeFabricated` puts it in the pack as `drives` (`Pack.Drives`); a finished drive or kit is taken whatever boards the pack holds.
- **Fitting it**: `game.fitDrive` (`cmd-game-fitDrive`, event `driveFitted`, `Ship.Jump`) fits one from the pack to a powered ship, refused for a cabin, the skiff (no battery), a ship that has one or a pack without one.
- **Jumping**: `ship/Jump.ts` (`jumpTarget`, `canJump`): from the cockpit, M jumps a ship with a drive and at least half its battery (`JUMP_CHARGE`) to `JUMP_HEIGHT` (60 m) above the other body's top, the planet from the moon and the moon from the planet, spending half its charge; the pilot saves the place with `game.flyShip`, which has never limited distance.
- Keys: M at the fabricator makes a drive, M by a ship fits it, M in the seat jumps. `TestJumpDrive` checks the coil and the refusals; `Jump.test.ts` and the Fabricator test the rest.

## Built in S26 (forge 375)

The well: with both hearths warded, the Hollowed's next way in.

- Step `well`; the fold gives `items.welllore`.
- `story/Well.ts`: `wellSpot(pieces, cell)` (the yard piece's well, where `Models.ts` draws it), `wellReady(done)` (both `hearthward` and `farmward`, not yet `well`), and the lines.
- Once Wenlan's hearth is warded, frost rings the well's stones (drawn in `drawMarks`) and the senses layer smells *frost, at noon* from it; walking to the well reaches the step.
- Tests: `story_test` (`well`), `Well.test.ts`, and the step order in `FirstHourStory.test.ts`.

## Built in V28 (forge 360)

The jump trial: the outpost's first contract that needs a jump drive.

- `contracts.json` gains `jump-trial` (Piloting, 150 xp, once): a ship with a jump drive fitted and 2 m³ of iron in its hold; a contract's `needs.jump` asks for `Ship.jump`.
- `cmd-game-handIn` refuses it with *this job needs a ship with a jump drive fitted*; `handInProblem` in `outpost/Outpost.ts` says the same on the contract board.
- Tests: `TestJumpDrive` (the refusal), `Outpost.test.ts` (the board's reasons), the contract count in `TestOutpost`.

## Built in S27 (forge 375)

The well ward: the threshold nobody warded, warded.

- Step `wellward`; the fold spends a sage and the river stones and gives `items.wellward`.
- `story/HearthWard.ts`: `nextHearthWard` gives `wellward` once `well` is reached (type `Ward`), `wardSpots('wellward')` is `culvert-stones-4` and `ditch-sage-3`, and `hearthWardMissing(items, what)` names the well; so the culvert and the bank give a fourth handful through the same senses and steps as before.
- At the well's rim with both, the ward is laid (`WELL_WARD_LINE` in `story/Well.ts`); the frost ring gives way to sage round the rim and five stones, four on it and one on the windlass beam.
- Tests: `story_test` (`wellward`), `HearthWard.test.ts`, the step order in `FirstHourStory.test.ts`.

## Built in V29 (forge 360)

Minerals in the ground: the first step of the user's *Ships from blocks* (forge `360-VoxelRPGDesign.md`), ships made from mined and refined materials rather than unlocked whole.

- `voxel/Types.ts`: six minerals as materials 4 to 9 (`MINERALS`, `mineralMaterial`, `mineralOf`, `MINERAL_TINT`): ferrite, silica and ice on the moon (`MOON_MINERALS`), malachite, wolframite and spodumene on the asteroids (`ASTEROID_MINERALS`, beside V8's generic ore); `MoonParams.minerals` gives each a share of the rock, veined by its own noise in `MoonGenerator.rockOrOre`.
- The mesher gives each vertex a `mineral` attribute (tint and weight); `terrainMaterial` tints the rock's grain toward it, so veins show on outcrops and in dug walls.
- `VoxelWorld.contentsOf` counts minerals by name; a dig's `took.minerals` goes to `cmd-game-editTerrain`, which keeps only the six names, bounds them by the sphere and the pack's room (minerals count against it), and adds them to the pack (`Pack.Minerals`, m³ by name).
- Mining: `HARDNESS.mineral` (4), and the work ring and label name the main mineral in its colour (`mineralsIn`, `workFor`). The scanner (K) finds minerals as well as ore, in pockets per kind, marked in each mineral's colour.
- Tests: `TestMinerals`, `Bodies.test.ts` (which minerals each body holds), `Mining.test.ts`, `Scanner.test.ts`.

## Built in V30 (forge 360)

Refining: the second step of *Ships from blocks*. The cabin's forge refines V29's minerals into components, the parts ship blocks will cost (V31).

- `game.smelt` takes `what`: `ore` (the default, into iron as in V20), a mineral or `rock`. A load is one kind, up to 2 m³, kept on the forge as `{ore, at, kind}`. `game.takeIngots` gives iron for ore, otherwise `parts` (counts that take no room, kept in `Pack.Parts`).
- Yields (`game.Refines`, the engine's `REFINES` and both blueprints agree): ferrite → 8 steel plates a m³, malachite → 10 copper wire, silica → 4 solar cells, wolframite → 3 tungsten nozzles, spodumene → 4 battery cells, all at 60 s a m³; rock → 6 bags of cement at 30 s a m³. Ice is not refined yet. A load too small to make one component is refused.
- Engine: `craft/Forge.ts` (`nextLoad`: ore first, then the mineral the pack holds most of, then rock of at least `CEMENT_MIN`; `forgeAction` gives `takeParts`; `partsFrom`, `msPer`, `PART_NAMES`). U at the forge loads and takes; the pack line lists the components.
- Tests: `TestRefine`, `Forge.test.ts`.

## Built in V31 (forge 360)

Blueprints from design chips: the third step of *Ships from blocks*, as the user chose ("blueprints first"). A design earned as a chip lays its outline out as a ghost, and each block costs components (V30) taken from the pack.

- `ShipDesign.Chip` marks such a design; the first is the **cutter** (`designs/cutter.json`, 20 blocks: cockpit, two lights, six armour, gyroscope, battery, solar panel, eight thrusters). `game.startShip` refuses it until the sender's `unlocked.cutter`, and its `started` event carries `types`, the block types by index, kept on the ship (`Ship.Types`; `TestCutter` checks the blueprint's list against the design file).
- `game.placeBlock` reads the sender's pack for a ship with types: each block costs `game.BlockCosts` (armour 2 plates; cockpit 3 plates and 2 wire; thruster a nozzle, a plate and a wire; gyroscope 2 plates and 3 wire; battery 2 battery cells and a wire; solar panel 2 solar cells and a wire; light a wire; cargo 4 plates), taken with the `placed` event's `pack`, and is refused naming what the pack is short of. Other designs' blocks stay free until free building replaces them.
- The chip is earned by the outpost's new **cutter-chip** contract: 4 steel plates and 2 copper wire from the sender's pack (`needs.parts`; `game.handIn` reads the pack and its `handedIn` carries the pack's change), paying prospecting and unlocking the cutter.
- The contract formerly called *Cobalt for the shipyard* is now *Asteroid ore for the shipyard* (its id `cobalt-shipyard` is kept): the user asked not to use cobalt.
- Engine: `ship/Blueprint.ts` (`BLOCK_COSTS`, `blockCost`, `shortOf`, `costLine`, `pay`, `billFor`); the viewport's outline hint names a chip block's cost, a click on one the pack cannot pay is refused in place, and `handInProblem` checks a contract's parts in the pack.
- Tests: `TestCutter`, `Blueprint.test.ts`.

## Built in V32 (forge 360)

The hauler, freighter and barge become design chips: their outlines carry `types` (`cmd-game-startShip`'s `chips` table) and each block costs components like the cutter's; the contract and Trade levels that unlocked them now give the chip. A refinery block costs 6 plates and 4 wire. Ships started before V32 keep free outlines. `TestChipDesigns` checks every chip's list against its file; the test helper `refineParts` refines components in parallel cabins. The editor's *Start* tooltip gives a chip ship's whole bill (`billFor`).

## Built in V33 (forge 360)

Lime, coal and the landing pad.

- Two more minerals, under the green planet's soil (`PLANET_MINERALS`): **limestone** (material 10) and **coal** (11), dug into the pack by name (`cmd-game-editTerrain`'s names).
- The forge burns limestone into **lime** (6 sacks a m³, 30 s a m³). **Cement now takes lime**: 2 sacks a m³ of rock (`CEMENT_LIME`, `limeFor`), the load bounded by the lime carried. **Coal is fuel**: with 0.1 m³ in the pack a load burns it and is done twice as fast (`forge.rate` 2, `FUEL_COAL`, `FUEL_RATE`); coal is never a load.
- The **landing pad** (`designs/pad.json`): a static chip design anyone may lay out, 25 `concrete` blocks (2 bags of cement each) and four lamps. A ship resting on a finished pad charges its battery (`ship/Pad.ts`, `padUnder`, `PAD_RATE`).
- Tests: `TestLimeAndPad`, `Forge.test.ts`, `Pad.test.ts`.

## Built in S28 and S29 (forge 375)

The Hollowed's road, and Ashby, the village they emptied (the user's lore, 2026-11-27: the Hollowed are villagers captured in Hollowed raids, not killed, and turned by Kasamir and the Enemy; the player sees only what the raids leave behind).

- Steps `track` ("The road they took") and `ashby` ("What is left of Ashby"); the fold gives `items.ashbylore` at `ashby`.
- `story/Ashby.ts`: `trackReady` (after `wellward`), `trackPrints(well, sign)` (frost prints from the well to the signpost), `REMNANTS` (door, table, shoe, furrows, with their lines), `remnantSpots(width, depth, cell)` placed by the area's size, `remnantAt`, `isAshby(name)` (an area named *Ashby*).
- `AreaViewport`: `drawRoad` lays the prints in the story's home area and the remnants in Ashby; `stepRoad` reaches `track` at the signpost (`TRACK_REACH`) and `ashby` once all four remnants are seen (`REMNANT_REACH`). The track is a smell; the door a sound, the rest smells.
- An area named *Ashby* must exist (made in the Areas editor; the seeder does not make one); travel there from the signpost.
- Tests: `story_test` (`track`, `ashby`, `items.ashbylore`), `Ashby.test.ts`, the step order in `FirstHourStory.test.ts`.

## Built in V34 (forge 360)

The bay floor (the user, 2026-11-27: small ships are hand-built only on a bay floor, whose docking clamp holds them, so ships need no wheels; an early building blueprint).

- `designs/bay.json`: a static chip anyone may lay out: a `clamp` (4 plates, 2 wire) in a deck of 34 `deck` blocks (1 plate each) and four lamps.
- `game.startShip` takes `bay`: the cutter, hauler, freighter and barge are refused unless it names a finished bay floor of the same world within 12 m (`BAY_REACH`). The skiff, prospector and buildings stay free of bays.
- Engine `ship/Bay.ts`: `needsBay`, `bayNear`, `clamped`. The viewport's `startShip` stands a chip ship on the nearest finished bay's deck and names it (or says to lay one out); while flying, a ship resting on a bay with its helm idle is held by the clamp (`stepClamp`). A pad or bay floor is laid on the ground, not 2 m above it.
- Not built: one ship per bay (an empty bay), the outpost's own bay floor for the starting skiff.
- Tests: `TestBayFloor`, `Bay.test.ts`; the hauler, freighter, barge and cutter tests lay a bay first (`layBay`), and `refineParts` refines four components a load.

## Built in V35 (forge 360)

The refinery, the first industry building (360-VoxelRPGDesign.md › *Industry*).

- `designs/refinehouse.json` (*Refinery*): a static chip of 12 concrete, a back wall of 4 armour, two `kiln`s (3 plates, 2 cement), a `control` block (2 plates, 3 wire) and two lamps; laid out once the `refinery-chip` contract (6 plates, 4 wire, 2 cement) unlocks `refinehouse`.
- Finished, its kilns smelt and refine like a cabin's forge (`game.smelt`, `game.takeIngots` on the building), but take loads of up to 8 m³ at rate 2, 4 with coal. Unfinished, it is refused.
- Engine: `ForgeSpec`, `forgeSpec(design)` (`craft/Forge.ts`; `nextLoad` takes the cap), `ShipLayer.kilnNear` (a finished refinery's kiln, found by `forgeNear`).
- Not built: two lines at once, feeding from storage (the pack still bounds a load).
- Tests: `TestRefinery` (`refineCement`), `Forge.test.ts`.

## Built in S30 (forge 375)

Word of Ashby. Wenlan's `farmer-ashby` talk (once `ashby` is reached) ends in step `told`: the Brannocks' boy runs to Hawk's Rest, and old Brannock, taken the night the player came, is understood to have been taken, not killed (`TOLD_LINE`). Ashby's furrows now run out along the north road: the captives are walked away, never down into the ground (the user's lore).

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
- V5 trusts the pilot's window to say what a crash broke, as it trusts where the ship is; the Node runner (346) decides it on the server. A block is whole or broken, with no integrity in between (366a), and a ship never splits in two (366d). Repair costs nothing until crafting (363, 368).
