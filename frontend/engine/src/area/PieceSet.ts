// Piece sets and the fit rules (forge 375 S1), the browser's copy of the
// server's (backend/features/game/PieceSet.go): the editor checks a
// placement before sending it, and the server's check decides. Keep the two
// in step; Fit.test.ts runs the same cases as the Go tests.
//
// Cells are 10 × 10 m. x runs east and z south. Sides are n (z = 0), e
// (x = max), s (z = max) and w (x = 0); n and s segments run west to east, e
// and w north to south. A turn of 90° clockwise seen from above takes n to e.

export type Side = 'n' | 'e' | 's' | 'w'

export interface Edges {
  n: string[]
  e: string[]
  s: string[]
  w: string[]
}

export interface Piece {
  id: string
  title: string
  description: string
  size: [number, number] // [width (x), depth (z)] in cells, unturned
  model: string
  edges: Edges
  boundary?: boolean // stands on the area's edge (an interior's hearth, door, window; S3)
  door?: boolean // a door the player walks through: game.linkDoor sets where it leads (S3)
}

export interface PieceSet {
  id: string
  title: string
  description: string
  cell: number
  interior?: boolean // walls round the area's edge and a floor (the kitchen's rooms, S3)
  edgeTypes: Record<string, string>
  meets: [string, string][]
  pieces: Piece[]
}

export interface Placement {
  id: string
  piece: string
  x: number
  z: number
  rot: number // 0 to 3
  link?: string // the area a door piece leads to (S3)
}

// A person standing in an area (game.placeNpc, S3), in metres
export interface Npc {
  id: string
  name: string
  kind?: string
  x: number
  z: number
  facing?: number
  dialogue?: string
}

// What a person says (the game.dialogues query): nodes of lines, each with answers
export interface Dialogue {
  id: string
  title?: string
  speaker: string
  start: string
  nodes: Record<string, { text: string; choices: { text: string; next?: string }[] }>
}

export interface AreaDef {
  id: string
  name: string
  pieceSet: string
  width: number
  depth: number
  pieces: Record<string, Placement>
  npcs?: Record<string, Npc> // the people standing in it (S3)
  deleted?: boolean
  version: number
}

export function pieceOf(set: PieceSet, id: string): Piece | undefined {
  return set.pieces.find((p) => p.id === id)
}

export function meetOK(set: PieceSet, a: string, b: string): boolean {
  return a === b || set.meets.some(([x, y]) => (x === a && y === b) || (x === b && y === a))
}

// A piece's footprint and edges after rot clockwise turns
export function turned(p: Piece, rot: number): { w: number; d: number; edges: Edges } {
  let w = p.size[0]
  let d = p.size[1]
  let e = p.edges
  for (let i = 0; i < (((rot % 4) + 4) % 4); i++) {
    e = { n: [...e.w].reverse(), e: e.n, s: [...e.e].reverse(), w: e.s }
    ;[w, d] = [d, w]
  }
  return { w, d, edges: e }
}

const key = (x: number, z: number, side: Side) => `${x},${z},${side}`

// A placement's edge segments by the cell and side they lie on
export function boundary(p: Piece, pl: Placement): Map<string, string> {
  const { w, d, edges } = turned(p, pl.rot)
  const out = new Map<string, string>()
  for (let i = 0; i < w; i++) {
    out.set(key(pl.x + i, pl.z, 'n'), edges.n[i])
    out.set(key(pl.x + i, pl.z + d - 1, 's'), edges.s[i])
  }
  for (let j = 0; j < d; j++) {
    out.set(key(pl.x + w - 1, pl.z + j, 'e'), edges.e[j])
    out.set(key(pl.x, pl.z + j, 'w'), edges.w[j])
  }
  return out
}

const opposite: Record<Side, Side> = { n: 's', s: 'n', e: 'w', w: 'e' }
const step: Record<Side, [number, number]> = { n: [0, -1], s: [0, 1], e: [1, 0], w: [-1, 0] }
const sideName: Record<Side, string> = { n: 'north', e: 'east', s: 'south', w: 'west' }
const sideOrder: Record<Side, number> = { e: 0, n: 1, s: 2, w: 3 } // the Go check's byte order

// Checks a placement: inside the area, over no other piece, and every edge
// meeting its neighbour's. Gives the first problem, or '' when it fits. The
// placement's own id is skipped, so a move is checked against the rest.
export function fit(set: PieceSet, width: number, depth: number, others: Record<string, Placement>, pl: Placement): string {
  const p = pieceOf(set, pl.piece)
  if (!p) return `there is no piece "${pl.piece}" in the ${set.title} set`
  const { w, d } = turned(p, pl.rot)
  if (pl.x < 0 || pl.z < 0 || pl.x + w > width || pl.z + d > depth) {
    return `the ${p.title} (${w} × ${d} cells) at cell ${pl.x}, ${pl.z} does not fit inside the ${width} × ${depth} area`
  }
  if (p.boundary && pl.x !== 0 && pl.z !== 0 && pl.x + w !== width && pl.z + d !== depth) {
    return `the ${p.title} at cell ${pl.x}, ${pl.z} must stand on the area's edge`
  }
  const cover = new Map<string, string>()
  const edges = new Map<string, string>()
  for (const id of Object.keys(others).sort()) {
    if (id === pl.id) continue
    const o = others[id]
    const op = pieceOf(set, o.piece)
    if (!op) continue
    const t = turned(op, o.rot)
    for (let i = 0; i < t.w; i++) for (let j = 0; j < t.d; j++) cover.set(`${o.x + i},${o.z + j}`, id)
    for (const [k, v] of boundary(op, o)) edges.set(k, v)
  }
  for (let j = 0; j < d; j++) {
    for (let i = 0; i < w; i++) {
      const id = cover.get(`${pl.x + i},${pl.z + j}`)
      if (id) return `the ${p.title} would cover the ${pieceOf(set, others[id].piece)?.title ?? id} at cell ${pl.x + i}, ${pl.z + j}`
    }
  }
  const mine = [...boundary(p, pl)].map(([k, t]) => {
    const [x, z, s] = k.split(',')
    return { x: Number(x), z: Number(z), s: s as Side, t }
  })
  mine.sort((a, b) => a.z - b.z || a.x - b.x || sideOrder[a.s] - sideOrder[b.s])
  for (const { x, z, s, t } of mine) {
    const [dx, dz] = step[s]
    const ns = opposite[s]
    const nt = edges.get(key(x + dx, z + dz, ns))
    if (nt === undefined) continue
    if (!meetOK(set, t, nt)) {
      const o = others[cover.get(`${x + dx},${z + dz}`) ?? '']
      const title = o ? pieceOf(set, o.piece)?.title : '?'
      return `the ${p.title}'s ${sideName[s]} edge (${t}) at cell ${x}, ${z} does not meet the ${title}'s ${sideName[ns]} edge (${nt})`
    }
  }
  return ''
}
