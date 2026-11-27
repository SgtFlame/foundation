import { moons } from '../sky/Moons.ts'
import type { Dish } from './Hum.ts'
import type { DreamScene } from './Remember.ts'
import { WAKE_LIT } from './Stones.ts'

// The dream tincture (forge 375 S21; the design's recipes: "Dream tincture.
// Made only at the full of the lesser moon. Brings true dreams.
// Ingredients not given"). The hedge-witch's bundles give the herbs her
// notes name, mugwort (the dreaming herb) and lavender (to keep the dreams
// kind). On a night of the lesser full moon, C at a hearth offers the
// tincture; the next sleep, anywhere, brings a true dream. Its scene is a
// placeholder until the story's past lives are written (the user's note of
// 2026-11-27); what it gives is lore of the threshold ward, the design's
// "herbs and stones laid in a set pattern so nothing unwelcome crosses",
// for a later step to lay.

export const TINCTURE: Dish = {
  id: 'tincture',
  herb: 'mugwort',
  title: 'Dream tincture',
  line: 'Mugwort and lavender steeped under the full lesser moon, bitter and silver. Drink it, then sleep.',
  chill: 0,
  hunger: 0,
}

// The other herb it takes
export const TINCTURE_ALSO = 'lavender'

// Whether the tincture can be made tonight: the lesser moon full, both
// herbs in the herbal, her notes read, and no true dream had yet
export function tinctureReady(night: number, items: Readonly<Record<string, number>>, done: Readonly<Record<string, boolean>>): boolean {
  return moons(night).lesser.lit > WAKE_LIT && !!items.hedgecraft && (items.mugwort ?? 0) >= 1 && (items[TINCTURE_ALSO] ?? 0) >= 1 && !done.truedream
}

export const TRUE_DREAM: DreamScene = {
  title: 'A true dream',
  lines: [
    'Moonlight through a cottage window you know, though the birches outside are saplings. A woman with ink on her fingers kneels at the threshold.',
    'She lays sage in a line along the sill of the door, then five river stones, each in its place: one at each post, three between. She says a word over each, and the word is a name.',
    'She looks up, straight at you, as if you were standing in the doorway. "Remember the order," she says. "It is the only part that matters."',
    'You wake knowing the pattern as surely as your own hand.',
  ],
}

export const TRUE_DREAM_LINE = 'You remember the threshold ward: sage along the sill, and five stones in their order. Nothing unwelcome crosses a ward laid true.'
