import { moons } from '../sky/Moons.ts'
import type { DreamScene } from './Remember.ts'

// The Nine Sisters† (forge 375 S18; the design's places: "ring of nine
// lichened standing stones crowned with pines. The ground remembers.
// Moon-activated ritual site", and the play design: the moons set "when
// the Nine Sisters wake"). The ring stands on open ground of the story's
// area, the free 3 × 3 cells nearest the signpost up the road to Hawk's
// Rest. On a night of the lesser full moon the stones wake: stand at the
// ring's heart and the ground gives up a memory, the queen of the First
// Flight naming the winter stars. The character keeps the star lore: on
// nights the lesser moon is more than half lit, the Hum holds one more.

export const RING_CELLS = 3
export const RING_RADIUS = 4.2 // metres from the heart to the stones
export const HEART = 1.8 // how near the heart the memory comes
export const WAKE_LIT = 0.9 // the lesser moon's light the stones wake at

interface Placed {
  piece: string
  x: number
  z: number
  rot: number
}

// The ring's heart in metres: the centre of the free 3 × 3 cells nearest
// the given cell (the signpost's), or null when the area has none
export function ringSpot(pieces: readonly Placed[], sizeOf: (p: Placed) => { w: number; d: number }, width: number, depth: number, cell: number, near: { x: number; z: number }): { x: number; z: number } | null {
  const used = new Set<string>()
  for (const p of pieces) {
    const { w, d } = sizeOf(p)
    for (let i = 0; i < w; i++) for (let j = 0; j < d; j++) used.add(`${p.x + i},${p.z + j}`)
  }
  let best: { x: number; z: number } | null = null
  let bestD = Infinity
  for (let x = 0; x + RING_CELLS <= width; x++)
    for (let z = 0; z + RING_CELLS <= depth; z++) {
      let free = true
      for (let i = 0; i < RING_CELLS && free; i++) for (let j = 0; j < RING_CELLS && free; j++) free = !used.has(`${x + i},${z + j}`)
      if (!free) continue
      const d = Math.hypot(x + 1 - near.x, z + 1 - near.z)
      if (d < bestD) {
        bestD = d
        best = { x: (x + RING_CELLS / 2) * cell, z: (z + RING_CELLS / 2) * cell }
      }
    }
  return best
}

// Where each of the nine stones stands around the heart
export function stonePoints(heart: { x: number; z: number }): { x: number; z: number; tilt: number }[] {
  return Array.from({ length: 9 }, (_, i) => {
    const a = (i / 9) * Math.PI * 2
    return { x: heart.x + Math.cos(a) * RING_RADIUS, z: heart.z + Math.sin(a) * RING_RADIUS, tilt: ((i * 37) % 7) / 40 - 0.08 }
  })
}

// Whether the stones are awake: a night of the lesser full moon
export function stonesWake(night: number, day: boolean): boolean {
  return !day && moons(night).lesser.lit > WAKE_LIT
}

// The next night (from this one) the stones wake
export function nextWaking(night: number): number {
  for (let n = night; n < night + 14; n++) if (moons(n).lesser.lit > WAKE_LIT) return n
  return night
}

// The Hum the star lore adds: one on nights the lesser moon is more than half lit
export function starlorePool(night: number, starlore: boolean): number {
  return starlore && moons(night).lesser.lit > 0.5 ? 1 : 0
}

export const STONES_MEMORY: DreamScene = {
  title: 'The Nine Sisters remember',
  lines: [
    'The stones hum under the lesser moon, and the ground under your feet goes soft as memory. It is another winter, long ago, and the pines on the stones are saplings.',
    'A woman sits on the tallest stone with silver charms in her braids. A child at her knee asks for the stars, and she names them, every one in the winter sky, slow, as if she were counting her own.',
    '"And that one?" The child points at the bright one that does not twinkle. "The Wanderer," she says. "Not a star at all. A world, like ours. Remember that, when they tell you otherwise."',
    'The hum fades. You are standing in the ring in the snow, and the stars overhead have names now.',
  ],
}

export const STARLORE_LINE = 'You remember the names of the winter stars. On nights the lesser moon is more than half lit, the Hum holds one more.'
