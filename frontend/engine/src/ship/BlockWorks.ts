// The block works (forge 360 V37; the industry plan's third building, the
// user, 2026-11-27): its assembler makes finished blocks from the components
// in a storage building within 30 m, and a blueprint's block takes a
// finished block from the pack in place of its components. The server
// decides (game.assemble); this module picks what U at the assembler makes.

import { BLOCK_COSTS, type Cost } from './Blueprint.ts'

// The blocks the assembler knows: a ship's and a building's shell
export const ASSEMBLES = ['armor', 'cockpit', 'thruster', 'gyro', 'battery', 'solar', 'light', 'cargo', 'concrete', 'deck'] as const
export const ASSEMBLE_MAX = 10 // at most this many a press of U
export const DEPOT_REACH = 30

// How many of a block a storage's stock pays for
export function affordable(stock: Readonly<Record<string, number>>, cost: Cost): number {
  const ks = Object.entries(cost) as [string, number][]
  if (!ks.length) return 0
  return Math.floor(Math.min(...ks.map(([k, n]) => (stock[k] ?? 0) / n)) + 1e-9)
}

// What U at the assembler makes: the block the blueprints laid out nearby
// still want most that storage pays for, else the first it pays for
export function nextAssembly(stock: Readonly<Record<string, number>>, wanted: Readonly<Record<string, number>> = {}): { block: string; count: number } | null {
  const known = ASSEMBLES as readonly string[]
  const want = Object.entries(wanted)
    .filter(([t, n]) => known.includes(t) && n > 0 && affordable(stock, BLOCK_COSTS[t]) >= 1)
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))[0]
  if (want) return { block: want[0], count: Math.min(want[1], affordable(stock, BLOCK_COSTS[want[0]]), ASSEMBLE_MAX) }
  const any = known.find((t) => affordable(stock, BLOCK_COSTS[t]) >= 1)
  return any ? { block: any, count: Math.min(affordable(stock, BLOCK_COSTS[any]), ASSEMBLE_MAX) } : null
}

// The blocks still to place in chip blueprints, by type
export function wantedBlocks(ships: readonly { types?: string[]; placed: number[] }[]): Record<string, number> {
  const out: Record<string, number> = {}
  for (const s of ships) {
    const done = new Set(s.placed)
    s.types?.forEach((t, i) => {
      if (!done.has(i)) out[t] = (out[t] ?? 0) + 1
    })
  }
  return out
}

// V37: finished blocks in storage are named block:<type>
export function finishedBlocks(stock: Readonly<Record<string, number>>): Record<string, number> {
  return Object.fromEntries(Object.entries(stock).filter(([k, v]) => k.startsWith('block:') && v > 0))
}
