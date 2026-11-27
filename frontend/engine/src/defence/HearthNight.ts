import { mulberry32 } from '../voxel/Noise.ts'
import type { AreaDef, Npc } from '../area/PieceSet.ts'

// A night of hearth defence at one house (forge 375 S5, a thin 382 and
// 383): the Hollowed come up through the hearthstone, and the hearth-watch
// inside whistles for help, as the book's watchers do: one blast, the
// stone moved; two, something came through; three, about to die. The
// watcher's pike keeps what came through at bay for a while, but only the
// character can end them; while the character is in the house the watcher
// stands back with the family. A barrel stood on the hearthstone by day
// holds the stone down longer. Pure and seeded, so it runs in Node tests
// and later on the server (346).

export type Whistle = 1 | 2 | 3

export type NightEvent =
  | { kind: 'whistle'; blasts: Whistle } // 1: the stone moved, 2: something came through, 3: about to die
  | { kind: 'through'; foe: { id: string; kind: string; x: number; z: number } }
  | { kind: 'clawed'; damage: number } // the watcher hurt
  | { kind: 'dawn' }
  | { kind: 'taken' } // the watcher fell and the family was dragged under
  | { kind: 'settled' } // S25: a warded hearthstone stirred and settled: nothing comes through

export interface NightOptions {
  seed?: number
  length?: number // seconds to dawn
  raiders?: number // how many come through
  cell?: number // the house's cell size in metres
  warded?: boolean // S25: a hearth ward on the stone: it stirs once and nothing comes through
}

export const WATCHER_HP = 45
const BARREL_DELAY = 25 // seconds a barrel on the hearthstone holds it down
const CLAW_EVERY = 2.4
const CLAW = 6
const DYING = 15

// The hearth and its hearthstone in a house: the stone is the cell in front
// of the hearth; a barrel there weighs it down
export function hearthOf(house: AreaDef, cell: number): { stone: { x: number; z: number }; barrel: boolean } | null {
  const hearth = Object.values(house.pieces).find((p) => p.piece === 'hearth')
  if (!hearth) return null
  // the hearth stands on the wall; the stone is the cell toward the room
  const turned = hearth.rot % 2 === 1
  const w = turned ? 1 : 2
  const d = turned ? 2 : 1
  const front: [number, number][] = []
  if (hearth.rot === 0) for (let i = 0; i < w; i++) front.push([hearth.x + i, hearth.z + d])
  if (hearth.rot === 2) for (let i = 0; i < w; i++) front.push([hearth.x + i, hearth.z - 1])
  if (hearth.rot === 1) for (let j = 0; j < d; j++) front.push([hearth.x - 1, hearth.z + j])
  if (hearth.rot === 3) for (let j = 0; j < d; j++) front.push([hearth.x + w, hearth.z + j])
  const barrel = Object.values(house.pieces).some((p) => p.piece === 'barrel' && front.some(([x, z]) => p.x === x && p.z === z))
  const [fx, fz] = front[0]
  const [lx, lz] = front[front.length - 1]
  return { stone: { x: ((fx + lx) / 2 + 0.5) * cell, z: ((fz + lz) / 2 + 0.5) * cell }, barrel }
}

export class HearthNight {
  readonly house: string
  readonly length: number
  readonly watcher: Npc | null
  readonly barrel: boolean
  readonly warded: boolean
  readonly stone: { x: number; z: number }
  watcherHp: number
  time = 0
  outcome: 'held' | 'taken' | null = null
  // what came through and still stands, by id
  readonly inside = new Map<string, { id: string; kind: string; x: number; z: number }>()
  private readonly schedule: { at: number; kind: 'stone' | 'through' }[] = []
  private readonly raiders: number
  private came = 0
  private claw = CLAW_EVERY
  private warned = false
  private readonly rand: () => number

  constructor(house: AreaDef, opts: NightOptions = {}) {
    this.house = house.id
    this.length = opts.length ?? 180
    this.warded = !!opts.warded
    this.raiders = this.warded ? 0 : (opts.raiders ?? 2)
    this.rand = mulberry32(opts.seed ?? 1)
    this.watcher = Object.values(house.npcs ?? {}).find((n) => n.kind === 'watcher') ?? null
    this.watcherHp = this.watcher ? WATCHER_HP : 0
    const h = hearthOf(house, opts.cell ?? 2)
    this.stone = h?.stone ?? { x: house.width, z: house.depth }
    this.barrel = h?.barrel ?? false
    // The stone moves a little into the night; the first comes through
    // after it, later with a barrel on it, the rest one by one
    const moved = 20 + this.rand() * 20
    this.schedule.push({ at: moved, kind: 'stone' })
    let at = moved + 15 + (this.barrel ? BARREL_DELAY : 0)
    for (let i = 0; i < this.raiders; i++) {
      this.schedule.push({ at, kind: 'through' })
      at += 20 + this.rand() * 10
    }
  }

  // Seconds to dawn
  get left(): number {
    return Math.max(0, this.length - this.time)
  }

  // One step; present is whether the character is in the house
  step(dt: number, present: boolean): NightEvent[] {
    const out: NightEvent[] = []
    if (this.outcome) return out
    this.time += dt
    while (this.schedule.length && this.schedule[0].at <= this.time) {
      const s = this.schedule.shift()!
      if (s.kind === 'stone') {
        if (this.warded) out.push({ kind: 'settled' })
        else if (this.watcherHp > 0) out.push({ kind: 'whistle', blasts: 1 })
      } else {
        this.came++
        const foe = { id: `raider-${this.came}`, kind: 'hollowed', x: this.stone.x + (this.rand() - 0.5) * 0.8, z: this.stone.z + (this.rand() - 0.5) * 0.8 }
        this.inside.set(foe.id, foe)
        out.push({ kind: 'through', foe })
        if (this.watcherHp > 0 && this.came === 1) out.push({ kind: 'whistle', blasts: 2 })
      }
    }
    // Alone with what came through, the watcher holds it off with the pike and is clawed
    if (this.inside.size && !present) {
      this.claw -= dt
      if (this.claw <= 0) {
        this.claw = CLAW_EVERY / Math.min(2, this.inside.size)
        // with no watcher, nobody holds them and the family is taken at once
        const damage = this.watcherHp > 0 ? CLAW : 0
        this.watcherHp = Math.max(0, this.watcherHp - damage)
        if (damage) out.push({ kind: 'clawed', damage })
        if (!this.warned && this.watcherHp > 0 && this.watcherHp <= DYING) {
          this.warned = true
          out.push({ kind: 'whistle', blasts: 3 })
        }
        if (this.watcherHp <= 0) {
          this.outcome = 'taken'
          out.push({ kind: 'taken' })
          return out
        }
      }
    } else {
      this.claw = CLAW_EVERY
    }
    if (this.time >= this.length) {
      // Dawn sends back whatever still stands: the house held
      this.outcome = 'held'
      out.push({ kind: 'dawn' })
    }
    return out
  }

  // The character ended one
  slay(id: string): void {
    this.inside.delete(id)
  }

  // Whether any more are still to come through
  get quietForGood(): boolean {
    return !this.schedule.some((s) => s.kind === 'through') && this.inside.size === 0
  }
}
