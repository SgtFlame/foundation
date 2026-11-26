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

## Decisions to review after seeing it

- A terrain edit is a command graph of the existing `command` kind, not a new `game.command` kind; a game kind comes with the Node runner (346), when rules run per tick.
- Edits are kept per 16 m cell and V1 reads them all. Streaming (360d) reads only the cells in view.
- Undo covers world definitions only; digging is play and is not an op.
- S2 walks on a 1 m grid rather than a Recast navigation mesh, and moves the character in the browser. Recast comes with height levels; the server takes movement over in S4.
- The moons are drawn in a corner of the view rather than in the sky, since the camera looks down. Bloom is left out until it is needed.
- V2's pack trusts the browser's reckoning of what a dig takes or a fill needs, bounded by the brush's volume. The server reckons it itself once the generator runs on the server (346).
- Asteroids are generated from the world's seed, not placed one by one; bodies as their own definitions come with 361a.
