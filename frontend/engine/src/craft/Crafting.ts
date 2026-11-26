// Crafting at a field bench (forge 360 V14; the play design's crafting
// ladder, its first rung: hand → field bench). The bench is a block of the
// cabin; its one recipe so far is the hand drill, the design's "first
// powered drill tearing through rock that took minutes by hand". The
// server's game.craft decides the same recipe (cmd-game-craft).

export interface Recipe {
  id: string
  title: string
  needs: { ore?: number; rock?: number; dust?: number } // m³ from the pack
  line: string // what it does, for the HUD
  manual?: string // V18: the manual that teaches it, salvaged from a wreck
  builtOn?: string // V18: a tool it needs first
}

export const RECIPES: readonly Recipe[] = [
  { id: 'drill', title: 'hand drill', needs: { ore: 2, rock: 1 }, line: 'digs wider and faster than bare hands' },
  // V17: the base defence's turret, set down on the moon with Y
  { id: 'turret', title: 'sentry turret', needs: { ore: 2, rock: 1.5 }, line: 'shoots down the surge\'s drones near where you set it; Y on the moon sets it down' },
  // V18: taught by the manual in a wreck's cockpit block
  { id: 'powerDrill', title: 'powered drill', needs: { ore: 2.5, rock: 1 }, line: 'tears through rock in a 6 m bite, faster than any hand', manual: 'powerDrill', builtOn: 'drill' },
]

// The recipe the bench offers next: the first not made yet, or null
// (one whose manual is not found yet is not offered)
export function nextRecipe(tools: Readonly<Record<string, boolean>> | undefined, manuals?: Readonly<Record<string, boolean>>): Recipe | null {
  return RECIPES.find((r) => !tools?.[r.id] && (!r.manual || manuals?.[r.manual]) && (!r.builtOn || tools?.[r.builtOn])) ?? null
}

export interface Carried {
  dust?: number
  rock?: number
  ore?: number
}

// Why a recipe cannot be made from a pack yet, or '' when it can
export function missing(r: Recipe, pack: Carried | null, tools: Readonly<Record<string, boolean>> | undefined): string {
  if (tools?.[r.id]) return `you already have a ${r.title}`
  const short = (Object.entries(r.needs) as [keyof Carried, number][]).filter(([k, n]) => (pack?.[k] ?? 0) < n)
  if (!short.length) return ''
  return `a ${r.title} takes ${Object.entries(r.needs).map(([k, n]) => `${n} m³ of ${k}`).join(' and ')} from your pack`
}

// The pack after making a recipe
export function spend(r: Recipe, pack: Carried): Carried {
  const out = { ...pack }
  for (const [k, n] of Object.entries(r.needs) as [keyof Carried, number][]) out[k] = Math.round(((out[k] ?? 0) - n) * 1000) / 1000
  return out
}

// How digging on foot goes with and without a drill: the largest brush
// (m) and the seconds between bites while the button is held
export interface DigLimits {
  maxBrush: number
  repeat: number
  name: string
}

export function digLimits(tools: Readonly<Record<string, boolean>> | undefined): DigLimits {
  if (tools?.powerDrill) return { maxBrush: 6, repeat: 0.07, name: 'powered drill' }
  return tools?.drill ? { maxBrush: 4, repeat: 0.12, name: 'drill' } : { maxBrush: 1.25, repeat: 0.35, name: 'dig by hand' }
}
