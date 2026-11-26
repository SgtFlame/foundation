// The engine's area demo (forge 375 S1): the Sheaflands farmstead laid out
// from the farmland set, edited with the server's fit rules run locally.
// `yarn dev` in frontend/engine serves it at /area.html. ?webgl=1, ?night=1
import farmland from '../../../backend/features/game/piecesets/farmland.json'
import { AreaViewport } from '../src/area/AreaViewport.ts'
import type { AreaDef, PieceSet, Placement } from '../src/area/PieceSet.ts'
import { farmstead } from '../src/area/Samples.ts'

const set = farmland as unknown as PieceSet

const q = new URLSearchParams(location.search)
const pieces: Record<string, Placement> = {}
farmstead.pieces.forEach((p, i) => (pieces[`p${i}`] = { ...p, id: `p${i}` }))
let area: AreaDef = { id: 'demo', name: farmstead.name, pieceSet: 'farmland', width: farmstead.width, depth: farmstead.depth, pieces, version: 1 }
let n = 1000
const viewport = await AreaViewport.create(document.getElementById('view') as HTMLElement, {
  set,
  area,
  forceWebGL: q.get('webgl') === '1',
  lighting: q.get('night') === '1' ? 'night' : 'day',
  onRefused: (m) => console.warn(m),
  onIntent: (i) => {
    const next = { ...area.pieces }
    if (i.kind === 'place') next[`p${n++}`] = { id: `p${n - 1}`, piece: i.piece, x: i.x, z: i.z, rot: i.rot }
    if (i.kind === 'move') next[i.id] = { ...next[i.id], ...Object.fromEntries(Object.entries(i).filter(([k, v]) => k !== 'kind' && v !== undefined)) }
    if (i.kind === 'remove') delete next[i.id]
    area = { ...area, pieces: next, version: area.version + 1 }
    viewport.setArea(area)
  },
})
const palette = document.getElementById('palette') as HTMLElement
for (const p of [{ id: '', title: 'Select' }, ...set.pieces]) {
  const b = document.createElement('button')
  b.textContent = p.title
  b.onclick = () => viewport.setTool(p.id || null)
  palette.appendChild(b)
}
;(window as unknown as { viewport: AreaViewport }).viewport = viewport
