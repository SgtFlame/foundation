// The engine's demo page: one moon, digging and filling with the server's
// rules run locally. `yarn dev` in frontend/engine serves it. Query
// parameters: ?seed=7&webgl=1&radius=100&view=160&walk=1&asteroids=2
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
  asteroids: num('asteroids', DEFAULT_MOON.asteroids ?? 0),
}
const log = new LocalEditLog(() => params)
const container = document.getElementById('view') as HTMLElement
const viewport = await VoxelViewport.create(container, {
  params,
  forceWebGL: q.get('webgl') === '1',
  viewDistance: num('view', 160),
  mode: q.get('walk') === '1' ? 'walk' : 'fly',
  onEditIntent: (intent) => {
    const e = log.commit(intent)
    if (typeof e === 'string') console.warn(e)
    else viewport.addEdits([e], 1)
    viewport.setCarried({ ...log.pack })
  },
})
// For browser tests and the console
viewport.setCarried({ ...log.pack })
;(window as unknown as { viewport: VoxelViewport }).viewport = viewport
