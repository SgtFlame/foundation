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
