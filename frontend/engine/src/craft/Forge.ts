// The cabin's forge (forge 360 V20; the play design's crafting ladder, its
// second rung: ore becomes iron over time). A forge is a block of the cabin
// (block 75). U puts up to 2 m³ of the pack's ore in; it smelts for SMELT_MS
// a m³, and U again takes IRON_YIELD m³ of iron a m³ of ore into the pack.
// V30 (ships from blocks, the user, 2026-11-27): the forge also refines the
// minerals V29 put in the ground into components, the parts ship blocks will
// cost: ferrite into plates, malachite into wire, silica into solar cells,
// wolframite into nozzles, spodumene into battery cells, and rock into
// cement. A load is one kind; components are counts and take no room.
// The server decides both (game.smelt, game.takeIngots); this module only
// tells the player where the forge stands.

export const SMELT_MS = 45_000
export const IRON_YIELD = 0.8
export const FORGE_LOAD = 2

// V30: what a forge load can be: ore (into iron), a mineral or rock
export type ForgeKind = 'ore' | 'rock' | 'ferrite' | 'malachite' | 'silica' | 'wolframite' | 'spodumene' | 'limestone'

// V30: the component each kind refines into, how many a m³ gives and how
// long a m³ takes
export const REFINES: Record<Exclude<ForgeKind, 'ore'>, { part: Part; per: number; ms: number }> = {
  ferrite: { part: 'plate', per: 8, ms: 60_000 },
  malachite: { part: 'wire', per: 10, ms: 60_000 },
  silica: { part: 'solarCell', per: 4, ms: 60_000 },
  wolframite: { part: 'nozzle', per: 3, ms: 60_000 },
  spodumene: { part: 'cell', per: 4, ms: 60_000 },
  rock: { part: 'cement', per: 6, ms: 30_000 },
  limestone: { part: 'lime', per: 6, ms: 30_000 }, // V33: burnt into lime
}

export type Part = 'plate' | 'wire' | 'solarCell' | 'nozzle' | 'cell' | 'cement' | 'lime'

export const PART_NAMES: Record<Part, string> = {
  plate: 'steel plates',
  wire: 'copper wire',
  solarCell: 'solar cells',
  nozzle: 'tungsten nozzles',
  cell: 'battery cells',
  cement: 'bags of cement',
  lime: 'sacks of lime',
}

// Rock smaller than this is not worth a load of cement
export const CEMENT_MIN = 0.25
// V33: cement is rock and lime: sacks of lime a m³ of rock takes
export const CEMENT_LIME = 2
// V33: coal in the pack fuels the forge: a load burns FUEL_COAL m³ of it
// and is done FUEL_RATE times as fast
export const FUEL_COAL = 0.1
export const FUEL_RATE = 2

// The lime a rock load takes, as the server counts it
export function limeFor(load: number): number {
  return Math.ceil(load * CEMENT_LIME - 1e-6)
}

export interface ForgeState {
  ore: number // m³ in the forge, of its kind
  at: number // Unix ms the load went in
  kind?: ForgeKind // V30: what it is, ore when left out
  rate?: number // V33: FUEL_RATE when coal fuels it
}

// Milliseconds a m³ of a kind takes
export function msPer(kind: ForgeKind | undefined): number {
  return !kind || kind === 'ore' ? SMELT_MS : REFINES[kind].ms
}

// Milliseconds left until the forge's load is ready, 0 when it is
export function smeltLeft(f: ForgeState, now: number): number {
  return Math.max(0, (f.ore * msPer(f.kind)) / (f.rate ?? 1) - (now - f.at))
}

// How far along the smelt is, 0 to 1
export function smeltFraction(f: ForgeState, now: number): number {
  const total = (f.ore * msPer(f.kind)) / (f.rate ?? 1)
  return total <= 0 ? 1 : Math.min(1, (now - f.at) / total)
}

// The iron a forge's ore gives, rounded as the server rounds it
export function ironFrom(ore: number): number {
  return Math.round(ore * IRON_YIELD * 1000) / 1000
}

// V30: the components a load of a kind gives, as the server counts them
export function partsFrom(kind: Exclude<ForgeKind, 'ore'>, load: number): number {
  return Math.floor(load * REFINES[kind].per + 1e-6)
}

interface PackLike {
  dust: number
  rock: number
  ore?: number
  iron?: number
  minerals?: Partial<Record<string, number>>
  parts?: Partial<Record<string, number>>
  blocks?: Partial<Record<string, number>> // V37: finished blocks
  capacity: number
}

// V30: what the forge takes next from a pack: ore first, then the
// refinable mineral the pack holds most of, then rock for cement
export function nextLoad(pack: PackLike | null, cap = FORGE_LOAD): { kind: ForgeKind; load: number } | null {
  if (!pack) return null
  if ((pack.ore ?? 0) > 0.001) return { kind: 'ore', load: Math.min(pack.ore ?? 0, cap) }
  const mins = Object.entries(pack.minerals ?? {})
    .filter(([k, v]) => k in REFINES && (v ?? 0) > 0.001)
    .sort((a, b) => (b[1] ?? 0) - (a[1] ?? 0))
  if (mins.length) return { kind: mins[0][0] as ForgeKind, load: Math.min(mins[0][1] ?? 0, cap) }
  // V33: cement takes lime too
  const rock = Math.min(pack.rock, cap, (pack.parts?.lime ?? 0) / CEMENT_LIME)
  if (rock >= CEMENT_MIN) return { kind: 'rock', load: Math.floor(rock * 1000) / 1000 }
  return null
}

// What U at the forge does now, given the forge and the pack
export type ForgeAction =
  | { kind: 'smelt'; ore: number; what: ForgeKind; fuel: boolean; fromStock?: boolean } // V36: fed from storage
  | { kind: 'wait'; seconds: number }
  | { kind: 'take'; iron: number }
  | { kind: 'takeParts'; part: Part; count: number }
  | { kind: 'none'; why: string }

// V35: what a building's forge takes: a cabin's 2 m³ at a time, a
// refinery's kilns 8 m³ twice as fast (the smelt blueprint agrees)
export interface ForgeSpec {
  cap: number
  rate: number
}
export const CABIN_FORGE: ForgeSpec = { cap: FORGE_LOAD, rate: 1 }
export const REFINERY_FORGE: ForgeSpec = { cap: 8, rate: 2 }
export function forgeSpec(design: string | undefined): ForgeSpec {
  return design === 'refinehouse' ? REFINERY_FORGE : CABIN_FORGE
}

export function forgeAction(f: ForgeState | null | undefined, pack: PackLike | null, now: number, spec: ForgeSpec = CABIN_FORGE, stock?: Readonly<Record<string, number>>): ForgeAction {
  if (f) {
    const left = smeltLeft(f, now)
    if (left > 0) return { kind: 'wait', seconds: Math.ceil(left / 1000) }
    if (f.kind && f.kind !== 'ore') return { kind: 'takeParts', part: REFINES[f.kind].part, count: partsFrom(f.kind, f.ore) }
    const iron = ironFrom(f.ore)
    const mins = Object.values(pack?.minerals ?? {}).reduce<number>((a, v) => a + (v ?? 0), 0)
    const held = pack ? pack.dust + pack.rock + (pack.ore ?? 0) + (pack.iron ?? 0) + mins : 0
    if (pack && held + iron > pack.capacity + 1e-4) return { kind: 'none', why: 'your pack has no room for the iron' }
    return { kind: 'take', iron }
  }
  const next = nextLoad(pack, spec.cap)
  // V36: with nothing in the pack, a refinery's storage feeds its minerals
  const fed = !next && stock ? stockLoad(stock, spec.cap) : null
  if (fed) return { kind: 'smelt', ore: fed.load, what: fed.kind, fuel: (pack?.minerals?.coal ?? 0) >= FUEL_COAL, fromStock: true }
  if (!next) return { kind: 'none', why: 'the forge needs ore (the asteroids hold it), a mineral to refine, or rock and lime for cement' }
  return { kind: 'smelt', ore: next.load, what: next.kind, fuel: (pack?.minerals?.coal ?? 0) >= FUEL_COAL }
}

// V36: the mineral a storage building feeds a refinery's kilns: the one it
// holds most of (never ore or rock, which come by hand)
export function stockLoad(stock: Readonly<Record<string, number>>, cap: number): { kind: ForgeKind; load: number } | null {
  const best = Object.entries(stock)
    .filter(([k, v]) => k in REFINES && k !== 'rock' && v > 0.001)
    .sort((a, b) => b[1] - a[1])[0]
  return best ? { kind: best[0] as ForgeKind, load: Math.floor(Math.min(best[1], cap) * 1000) / 1000 } : null
}

// V36: what U at storage puts in: every component and material in the
// pack but the coal, which stays as fuel
export function storable(pack: PackLike | null): Record<string, number> {
  if (!pack) return {}
  const out: Record<string, number> = {}
  for (const k of ['ore', 'rock', 'iron'] as const) if ((pack[k] ?? 0) > 0.0005) out[k] = Math.round((pack[k] ?? 0) * 1000) / 1000
  for (const [k, v] of Object.entries(pack.minerals ?? {})) if (k !== 'coal' && (v ?? 0) > 0.0005) out[k] = Math.round((v ?? 0) * 1000) / 1000
  for (const [k, v] of Object.entries(pack.parts ?? {})) if ((v ?? 0) > 0) out[k] = v ?? 0
  for (const [k, v] of Object.entries(pack.blocks ?? {})) if ((v ?? 0) > 0) out[`block:${k}`] = v ?? 0 // V37
  return out
}

// V36: what H at storage takes: the components it holds (they take no room;
// V37: nor do finished blocks, block:<type>)
export function components(stock: Readonly<Record<string, number>>): Record<string, number> {
  return Object.fromEntries(Object.entries(stock).filter(([k, v]) => (k in PART_NAMES || k.startsWith('block:')) && v > 0))
}
