import { moons } from '../sky/Moons.ts'
import type { Herb } from './FirstHourStory.ts'
import type { Dish } from './Hum.ts'
import { nextWaking } from './Stones.ts'

// The hedge-witch's bundles and the moon-calendar stones (forge 375 S20;
// the design's Hawk's Rest: "overgrown herb garden, moon-calendar stones on
// the sill", and its herbs: "lavender, mugwort, yarrow, valerian, feverfew,
// borage, sage: gardens and dried bundles"). Once her notes are found, the
// bundles drying over the cottage's shelves can be read for what they are:
// four herbs from her garden, each taken by walking up to it. Her notes
// teach two more things to make at the hearth. The pebbles on the window
// sill are her moon calendar: standing by them tells how far off the next
// full moons are, the lesser's being the night the Nine Sisters wake.

export const BUNDLE_REACH = 1.6 // metres
export const SILL_REACH = 1.8

export const BUNDLE_HERBS: readonly Herb[] = [
  { id: 'lavender', title: 'Lavender', note: 'Calming. A sprig under the pillow; the notes say it keeps the dreams kind.', color: 0x8e7cc3 },
  { id: 'mugwort', title: 'Mugwort', note: 'The dreaming herb, silver under the leaf. "Not before the lesser moon is full," in red ink.', color: 0x9fae8c },
  { id: 'valerian', title: 'Valerian', note: 'The root smells of old boots and brings sleep. Steeped with lavender, it settles a shaking body.', color: 0xd8c8a8 },
  { id: 'sage', title: 'Sage', note: 'A ward herb, laid on a threshold. Boiled in broth, it warms and fills.', color: 0x8fa38a },
]

// The hearth's new dishes from her notes (with hedge-craft)
export const HEDGE_DISHES: readonly Dish[] = [
  { id: 'valerianTea', herb: 'valerian', title: 'Valerian and lavender tea', line: 'Steeped hot and drunk slowly: the shaking stops and the cold goes out of the hands.', chill: 60, hunger: 5 },
  { id: 'sageBroth', herb: 'sage', title: 'Sage broth', line: 'Barley, sage and a crust: it warms and fills.', chill: 20, hunger: 45 },
]

type Placed = { id: string; piece: string; x: number; z: number }

// Where the bundles hang: spread along the cottage's shelves, two to a
// shelf, west to east; none without a shelf. Spot ids stay with the herb.
export function bundleSpots(pieces: readonly Placed[], cell: number): { spot: string; herb: Herb; x: number; z: number }[] {
  const shelves = pieces.filter((p) => p.piece === 'shelf').sort((a, b) => a.x - b.x || a.z - b.z)
  if (!shelves.length) return []
  const per = Math.ceil(BUNDLE_HERBS.length / shelves.length)
  return BUNDLE_HERBS.map((herb, i) => {
    const s = shelves[Math.floor(i / per)]
    const k = i % per
    return { spot: `bundle-${herb.id}`, herb, x: (s.x + (k + 0.5) / per) * cell, z: (s.z + 0.5) * cell }
  })
}

// Whether the bundles are hers to take: after the cache, until all four are taken
export function bundlesReady(done: Readonly<Record<string, boolean>>): boolean {
  return !!done.cache && !done.bundles
}

// The sill's pebbles: at the cottage's window, or null without one
export function sillSpot(pieces: readonly Placed[], cell: number): { x: number; z: number } | null {
  const w = pieces.find((p) => p.piece === 'window')
  return w ? { x: (w.x + 0.5) * cell, z: (w.z + 0.5) * cell } : null
}

// The next night (from this one) the greater moon is full
export function nextGreaterFull(night: number): number {
  for (let n = night; n < night + 31; n++) if (moons(n).greater.lit > 0.97) return n
  return night
}

const nights = (n: number) => (n === 0 ? 'tonight' : n === 1 ? 'tomorrow night' : `in ${n} nights`)

// What the moon calendar says on a night
export function sillLine(night: number): string {
  const lesser = nextWaking(night) - night
  const greater = nextGreaterFull(night) - night
  return `Pebbles on the sill, white and grey, moved one a night: her moon calendar. The lesser moon is full ${nights(lesser)}, the greater ${nights(greater)}.`
}
