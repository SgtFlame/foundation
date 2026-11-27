import type { Mineral } from '../voxel/Types.ts'
// Working the ground over time (forge 360, the user's direction of
// 2026-11-26: "use a tool to harvest resources over time by holding down
// the action button", as in Space Engineers and 7 Days to Die). On foot,
// holding the dig button on a spot works it: each material takes work by
// its hardness, a tool brings work each second, and when the bite under
// the brush has had its work it comes away (one game.editTerrain). Moving
// the aim to another spot starts over; a bigger brush is a longer hold.
// Other hold actions (tearing a plate off a wreck, cutting a crop) use the
// same Hold with a fixed time.

export interface Contents {
  dust: number
  rock: number
  ore: number
  // V29: minerals, m³ by name
  minerals?: Partial<Record<Mineral, number>>
}

// V29: all the minerals in some contents, and the one there is most of
export function mineralsIn(c: Contents): { total: number; main: Mineral | null } {
  let total = 0
  let main: Mineral | null = null
  for (const [k, v] of Object.entries(c.minerals ?? {}) as [Mineral, number][]) {
    total += v
    if (!main || v > (c.minerals?.[main] ?? 0)) main = k
  }
  return { total, main }
}

// Work per m³ of each material
export const HARDNESS = { dust: 1, rock: 3, ore: 5, mineral: 4 } as const

// Work per second each tool brings: bare hands, then the drills
export function toolPower(tools: Readonly<Record<string, boolean>> | undefined): number {
  return tools?.powerDrill ? 16 : tools?.drill ? 6 : 1
}

// The work a bite takes, and what it is mostly made of (for the HUD)
export function workFor(c: Contents): { work: number; main: 'dust' | 'rock' | 'ore' | Mineral | null } {
  const m = mineralsIn(c)
  const work = c.dust * HARDNESS.dust + c.rock * HARDNESS.rock + c.ore * HARDNESS.ore + m.total * HARDNESS.mineral
  if (m.main && m.total >= c.ore && m.total >= c.rock && m.total >= c.dust) return { work, main: m.main }
  const main = c.dust + c.rock + c.ore <= 0 ? null : c.ore >= c.rock && c.ore >= c.dust ? 'ore' : c.rock >= c.dust ? 'rock' : 'dust'
  return { work, main }
}

export function secondsFor(c: Contents, tools: Readonly<Record<string, boolean>> | undefined): number {
  return workFor(c).work / toolPower(tools)
}

// A hold on one target: progress grows while the same target is held and
// starts over on another; done once when it reaches the work needed
export class Hold {
  key = ''
  progress = 0
  need = 0

  // dt seconds of holding target key, which needs `need` work at `power` a second
  step(dt: number, key: string, need: number, power: number): boolean {
    if (key !== this.key) {
      this.key = key
      this.progress = 0
    }
    this.need = need
    if (need <= 0) return false
    this.progress += power * dt
    if (this.progress < need) return false
    this.progress = 0
    return true
  }

  release(): void {
    this.key = ''
    this.progress = 0
    this.need = 0
  }

  get fraction(): number {
    return this.need > 0 ? Math.min(1, this.progress / this.need) : 0
  }
}

// A grid key for a point, so small wobbles of the aim keep the same target
export function spotKey(p: { x: number; y: number; z: number }, cell: number): string {
  return `${Math.round(p.x / cell)},${Math.round(p.y / cell)},${Math.round(p.z / cell)}`
}
