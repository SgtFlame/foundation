// The cabin's fabricator (forge 360 V23; the play design's crafting
// ladder, its third rung: "fabricator (powered; electronics, thrusters,
// advanced blocks)"). A fabricator is a block of the cabin (block 76),
// powered by the solar panel on its roof (block 77). U starts a circuit
// board from FAB_IRON m³ of iron and FAB_ORE m³ of ore; it takes FAB_MS,
// and U again takes the board into the pack. The server decides both
// (game.fabricate, game.takeFabricated); this module only tells the player
// where the fabricator stands.

export const FAB_MS = 30_000
export const FAB_IRON = 0.5
export const FAB_ORE = 0.25
export const MAX_CIRCUITS = 20
// V24: the ion thruster kit, from iron and circuit boards, fitted to a ship
export const KIT_MS = 60_000
export const KIT_IRON = 1
export const KIT_CIRCUITS = 2
export const KIT_BOOST = 0.25 // added to a ship's thrust, at most MAX_BOOST
export const MAX_BOOST = 0.5

export type FabItem = 'circuit' | 'ionKit'

export interface FabState {
  item: string
  at: number // Unix ms the work began
}

// Milliseconds left until the board is done, 0 when it is
export function fabLeft(f: FabState, now: number): number {
  return Math.max(0, (f.item === 'ionKit' ? KIT_MS : FAB_MS) - (now - f.at))
}

export type FabAction =
  | { kind: 'make' }
  | { kind: 'wait'; seconds: number }
  | { kind: 'take' }
  | { kind: 'none'; why: string }

export function fabAction(f: FabState | null | undefined, pack: { ore?: number; iron?: number; circuits?: number } | null, powered: boolean, now: number, item: FabItem = 'circuit'): FabAction {
  if (f) {
    const left = fabLeft(f, now)
    if (left > 0) return { kind: 'wait', seconds: Math.ceil(left / 1000) }
    if (f.item !== 'ionKit' && (pack?.circuits ?? 0) >= MAX_CIRCUITS) return { kind: 'none', why: `you can carry no more than ${MAX_CIRCUITS} circuit boards` }
    return { kind: 'take' }
  }
  if (!powered) return { kind: 'none', why: 'no power: place the solar panel on the roof' }
  if (item === 'ionKit') {
    if ((pack?.iron ?? 0) < KIT_IRON || (pack?.circuits ?? 0) < KIT_CIRCUITS) return { kind: 'none', why: `an ion thruster kit takes ${KIT_IRON} m³ of iron and ${KIT_CIRCUITS} circuit boards` }
    return { kind: 'make' }
  }
  if ((pack?.iron ?? 0) < FAB_IRON || (pack?.ore ?? 0) < FAB_ORE) return { kind: 'none', why: `a circuit board takes ${FAB_IRON} m³ of iron and ${FAB_ORE} m³ of ore` }
  return { kind: 'make' }
}
