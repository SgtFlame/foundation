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

## Decisions to review after seeing it

- A terrain edit is a command graph of the existing `command` kind, not a new `game.command` kind; a game kind comes with the Node runner (346), when rules run per tick.
- Edits are kept per 16 m cell and V1 reads them all. Streaming (360d) reads only the cells in view.
- Undo covers world definitions only; digging is play and is not an op.
