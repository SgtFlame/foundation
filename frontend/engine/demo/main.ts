// The engine's demo page: one moon, digging and filling with the server's
// rules run locally. `yarn dev` in frontend/engine serves it. Query
// parameters: ?seed=7&webgl=1&radius=100&view=160
import { LocalEditLog } from '../src/voxel/LocalEdits.ts'
import { DEFAULT_MOON, type MoonParams } from '../src/voxel/Types.ts'
import { VoxelViewport } from '../src/voxel/VoxelViewport.ts'

const q = new URLSearchParams(location.search)
const num = (k: string, d: number) => (q.has(k) ? Number(q.get(k)) : d)
const params: MoonParams = {
  ...DEFAULT_MOON,
  seed: num('seed', DEFAULT_MOON.seed),
  radius: num('radius', DEFAULT_MOON.radius),
  voxelSize: num('voxel', DEFAULT_MOON.voxelSize),
}
const log = new LocalEditLog(() => params)
const container = document.getElementById('view') as HTMLElement
const viewport = await VoxelViewport.create(container, {
  params,
  forceWebGL: q.get('webgl') === '1',
  viewDistance: num('view', 160),
  onEditIntent: (intent) => {
    const e = log.commit(intent)
    if (typeof e !== 'string') viewport.addEdits([e], 1)
  },
})
// For browser tests and the console
;(window as unknown as { viewport: VoxelViewport }).viewport = viewport
