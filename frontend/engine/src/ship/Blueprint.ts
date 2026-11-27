// Blueprints from design chips (forge 360 V31; ships from blocks, the user,
// 2026-11-27, who chose blueprints first): a design earned as a chip (the
// cutter, from the quartermaster's cutter-chip contract) lays its outline
// out as a ghost, and each block costs components refined at a forge (V30),
// taken from the pack as it is placed. The server decides (game.placeBlock,
// game.BlockCosts); this module says what a block costs and what is short.
// Older designs' blocks stay free until free building replaces them.

import type { Part } from '../craft/Forge.ts'
import { PART_NAMES } from '../craft/Forge.ts'

export type Cost = Partial<Record<Part, number>>

export const BLOCK_COSTS: Record<string, Cost> = {
  armor: { plate: 2 },
  cockpit: { plate: 3, wire: 2 },
  thruster: { nozzle: 1, plate: 1, wire: 1 },
  gyro: { plate: 2, wire: 3 },
  battery: { cell: 2, wire: 1 },
  solar: { solarCell: 2, wire: 1 },
  light: { wire: 1 },
  cargo: { plate: 4 },
  refinery: { plate: 6, wire: 4 }, // V32
  concrete: { cement: 2 }, // V33: the landing pad's
  deck: { plate: 1 }, // V34: the bay floor's
  clamp: { plate: 4, wire: 2 },
  kiln: { plate: 3, cement: 2 }, // V35: the refinery's
  control: { plate: 2, wire: 3 },
}

// What a block of a ship costs: nothing unless the ship keeps chip types
export function blockCost(types: readonly string[] | undefined, index: number): { type: string; cost: Cost } | null {
  const type = types?.[index]
  return type ? { type, cost: BLOCK_COSTS[type] ?? {} } : null
}

// The parts a pack is short of for a cost, by name
export function shortOf(parts: Partial<Record<Part, number>> | undefined, cost: Cost): Part[] {
  return (Object.keys(cost) as Part[]).filter((k) => (parts?.[k] ?? 0) < (cost[k] ?? 0)).sort()
}

// A cost in words: "3 steel plates, 2 copper wire"
export function costLine(cost: Cost): string {
  return (Object.entries(cost) as [Part, number][]).map(([k, n]) => `${n} ${PART_NAMES[k]}`).join(', ')
}

// The parts a pack keeps once a cost is paid
export function pay(parts: Partial<Record<Part, number>> | undefined, cost: Cost): Partial<Record<Part, number>> {
  const out = { ...parts }
  for (const [k, n] of Object.entries(cost) as [Part, number][]) out[k] = Math.max(0, (out[k] ?? 0) - n)
  return out
}

// The whole bill for a chip ship's blocks still to place
export function billFor(types: readonly string[] | undefined, placed: readonly number[]): Cost {
  const bill: Cost = {}
  const done = new Set(placed)
  types?.forEach((t, i) => {
    if (done.has(i)) return
    for (const [k, n] of Object.entries(BLOCK_COSTS[t] ?? {}) as [Part, number][]) bill[k] = (bill[k] ?? 0) + n
  })
  return bill
}
