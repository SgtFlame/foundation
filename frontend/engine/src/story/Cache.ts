import type { DreamScene } from './Remember.ts'
import type { Dish } from './Hum.ts'

// The hedge-witch's cache (forge 375 S19; the design's places: Hawk's Rest,
// "a hedge-witch's one-room cottage between two birches. Hidden floor
// cache, overgrown herb garden, moon-calendar stones on the sill"). After
// the Nine Sisters, the cottage lets its guest find what its last keeper
// hid: a loose board by the box bed, which the senses catch as a draught.
// Under it, her notes on hedge-craft, which teach the yarrow poultice: C at
// a hearth turns a handful of yarrow into a poultice that closes a wound
// (the dream's ache at once) and takes the cold out of the body.

export const CACHE_REACH = 2.2 // metres from the bed's middle

// The cache's place: by the box bed of a cottage, or null without one
export function cacheSpot(pieces: readonly { piece: string; x: number; z: number }[], cell: number): { x: number; z: number } | null {
  const bed = pieces.find((p) => p.piece === 'bed')
  return bed ? { x: (bed.x + 0.5) * cell, z: (bed.z + 0.5) * cell } : null
}

// Whether the cache can be found now: after the stones, not yet opened
export function cacheReady(done: Readonly<Record<string, boolean>>): boolean {
  return !!done.stones && !done.cache
}

export const POULTICE: Dish = {
  id: 'poultice',
  herb: 'yarrow',
  title: 'Yarrow poultice',
  line: 'Bruised yarrow, bound warm against the skin: it closes a wound and draws the cold out.',
  chill: 40,
  hunger: 0,
}

export const CACHE_NOTES: DreamScene = {
  title: 'Under the loose board',
  lines: [
    'The board by the box bed lifts on a finger. Underneath, wrapped in oilcloth, a bundle of pages tied with red thread, and a smell of dried flowers.',
    'A careful hand, a hedge-witch\'s: which moss stops bleeding, which bark brings sleep, where the cowslips come first. In the margins, small moons drawn by the days.',
    '"Yarrow for wounds, bruised and bound warm. Soldier\'s woundwort, my mother called it. It takes the cold out of a cut, even one that is not of this world."',
    'You tie the thread again and keep the pages. The cottage feels, for a moment, less empty.',
  ],
}

export const CACHE_LINE = 'You can make a yarrow poultice now: press C at a hearth with yarrow in your herbal. It closes a wound and takes the cold out.'
