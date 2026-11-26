// The cabin's forge (forge 360 V20; the play design's crafting ladder, its
// second rung: ore becomes iron over time). A forge is a block of the cabin
// (block 75). U puts up to 2 m³ of the pack's ore in; it smelts for SMELT_MS
// a m³, and U again takes IRON_YIELD m³ of iron a m³ of ore into the pack.
// The server decides both (game.smelt, game.takeIngots); this module only
// tells the player where the forge stands.

export const SMELT_MS = 45_000
export const IRON_YIELD = 0.8
export const FORGE_LOAD = 2

export interface ForgeState {
  ore: number
  at: number // Unix ms the ore went in
}

// Milliseconds left until the forge's iron is ready, 0 when it is
export function smeltLeft(f: ForgeState, now: number): number {
  return Math.max(0, f.ore * SMELT_MS - (now - f.at))
}

// How far along the smelt is, 0 to 1
export function smeltFraction(f: ForgeState, now: number): number {
  const total = f.ore * SMELT_MS
  return total <= 0 ? 1 : Math.min(1, (now - f.at) / total)
}

// The iron a forge's ore gives, rounded as the server rounds it
export function ironFrom(ore: number): number {
  return Math.round(ore * IRON_YIELD * 1000) / 1000
}

// What U at the forge does now, given the forge and the pack
export type ForgeAction =
  | { kind: 'smelt'; ore: number }
  | { kind: 'wait'; seconds: number }
  | { kind: 'take'; iron: number }
  | { kind: 'none'; why: string }

export function forgeAction(f: ForgeState | null | undefined, pack: { dust: number; rock: number; ore?: number; iron?: number; capacity: number } | null, now: number): ForgeAction {
  if (f) {
    const left = smeltLeft(f, now)
    if (left > 0) return { kind: 'wait', seconds: Math.ceil(left / 1000) }
    const iron = ironFrom(f.ore)
    const held = pack ? pack.dust + pack.rock + (pack.ore ?? 0) + (pack.iron ?? 0) : 0
    if (pack && held + iron > pack.capacity + 1e-4) return { kind: 'none', why: 'your pack has no room for the iron' }
    return { kind: 'take', iron }
  }
  const ore = Math.min(pack?.ore ?? 0, FORGE_LOAD)
  if (ore <= 0) return { kind: 'none', why: 'the forge needs ore: the asteroids hold it' }
  return { kind: 'smelt', ore }
}
