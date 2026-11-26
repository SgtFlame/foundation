import { mulberry32 } from '../voxel/Noise.ts'

// Real-time combat decided by the character's skills (forge 375 S4, a thin
// 382 and 379). The player chooses what the character does: a target and a
// stance, Strike, Pivot or Guard. The character's skills decide how well:
// whether a swing lands, and whether it lands where it matters. A Hollowed
// takes real harm only to the neck or the heart; any other hit staggers it
// and it keeps coming (375 design › Fighting the Hollowed). The Hollowed
// drive for the character, show a tell before each claw to a character who
// can read them, and are blocked by Guard.
//
// It is a pure simulation, deterministic from its seed and the orders
// given, with no rendering, so it runs in Node tests and, later, in the
// Node runner on the server (346), which takes combat over from the browser.

export type Stance = 'strike' | 'pivot' | 'guard'

// The character's skills, 0 (untrained) to 5 (a master)
export interface Skills {
  blade: number
  polearm: number
  // reading the opponent: from 1 the Hollowed's tells show before a claw
  reading: number
}

export interface Weapon {
  id: string
  title: string
  kind: 'blade' | 'polearm'
  reach: number // metres from the character's centre to where it strikes
  swing: number // seconds between strikes
  aim: number // added to the chance of a vital hit: polearms reach the heart (375 › pikes beat swords)
}

export const WEAPONS: Record<string, Weapon> = {
  sword: { id: 'sword', title: 'Sword and knife', kind: 'blade', reach: 1.5, swing: 0.9, aim: 0 },
  pitchfork: { id: 'pitchfork', title: 'Pitchfork', kind: 'polearm', reach: 2.3, swing: 1.3, aim: 0.15 },
}

export interface CreatureKind {
  id: string
  title: string
  hp: number // for creatures harmed anywhere; ignored when vitalOnly
  vitalOnly: boolean // only a neck or heart wound ends it
  speed: number
  reach: number
  clawEvery: number // seconds between claws
  claw: number // damage
  tell: number // seconds its tell shows before a claw lands
}

export const CREATURES: Record<string, CreatureKind> = {
  hollowed: { id: 'hollowed', title: 'Hollowed', hp: 1, vitalOnly: true, speed: 3.2, reach: 1.25, clawEvery: 2.4, claw: 8, tell: 0.7 },
}

export function isHostile(kind: string | undefined): boolean {
  return !!kind && kind in CREATURES
}

export interface Foe {
  id: string
  kind: CreatureKind
  x: number
  z: number
  alive: boolean
  hp: number
  cooldown: number // until its next claw may start
  telling: number // > 0 while its tell shows; the claw lands at 0
  stagger: number // seconds it reels after a hit
}

export type CombatEvent =
  | { kind: 'tell'; foe: string }
  | { kind: 'miss'; foe: string }
  | { kind: 'futile'; foe: string } // a hit that does not matter to it
  | { kind: 'hurt'; foe: string; damage: number }
  | { kind: 'kill'; foe: string; vital: 'neck' | 'heart' | null }
  | { kind: 'claw'; foe: string; damage: number; dodged?: boolean; blocked?: boolean; read?: boolean }
  | { kind: 'cadence' } // Strike, Pivot, Guard chained: the next strike is aimed
  | { kind: 'fallen' }

export interface CombatOptions {
  skills: Skills
  weapon: Weapon
  seed?: number
  hp?: number
  // Where a foe may stand (the walk grid), so they go round furniture rather than through it
  walkable?: (x: number, z: number) => boolean
  // S8: whether the cadence may come now (the story spends a Hum on it); left out, always
  cadenceAllowed?: () => boolean
}

export const HERO_HP = 100
const ORDER: Stance[] = ['strike', 'pivot', 'guard']

export class Combat {
  readonly foes = new Map<string, Foe>()
  hp: number
  stance: Stance = 'strike'
  target: string | null = null
  private readonly opts: CombatOptions
  private readonly rand: () => number
  private cooldown = 0
  private stanceTime = 0
  // stances held in turn, for the cadence
  private chain: Stance[] = ['strike']
  private cadence = false
  private flank = false // a pivot set up a flank strike
  private opening = false // a read tell was guarded: an opening
  private time = 0

  constructor(foes: { id: string; kind: string; x: number; z: number }[], opts: CombatOptions) {
    this.opts = opts
    this.rand = mulberry32(opts.seed ?? 1)
    this.hp = opts.hp ?? HERO_HP
    for (const f of foes) {
      const kind = CREATURES[f.kind]
      if (!kind) continue
      this.foes.set(f.id, { id: f.id, kind, x: f.x, z: f.z, alive: true, hp: kind.hp, cooldown: 0.8 + this.rand() * 0.6, telling: 0, stagger: 0 })
    }
  }

  // One more joins the fight (S5: up through the hearthstone)
  addFoe(f: { id: string; kind: string; x: number; z: number }): void {
    const kind = CREATURES[f.kind]
    if (!kind || this.foes.has(f.id)) return
    this.foes.set(f.id, { id: f.id, kind, x: f.x, z: f.z, alive: true, hp: kind.hp, cooldown: 0.8 + this.rand() * 0.6, telling: 0, stagger: 0 })
  }

  get over(): 'won' | 'fallen' | null {
    if (this.hp <= 0) return 'fallen'
    for (const f of this.foes.values()) if (f.alive) return null
    return 'won'
  }

  get weapon(): Weapon {
    return this.opts.weapon
  }

  // The skill the weapon in hand uses
  get skill(): number {
    return this.opts.weapon.kind === 'polearm' ? this.opts.skills.polearm : this.opts.skills.blade
  }

  setStance(s: Stance): void {
    if (s === this.stance) return
    // a stance held a moment counts toward the cadence: Strike, Pivot, Guard, in that order
    const last = this.chain[this.chain.length - 1]
    const next = ORDER[(ORDER.indexOf(last) + 1) % 3]
    if (this.stanceTime >= 0.25 && s === next) this.chain.push(s)
    else this.chain = [s]
    if (s === 'strike' && this.chain.length >= 4 && this.skill >= 2 && (this.opts.cadenceAllowed?.() ?? true)) {
      this.cadence = true
      this.chain = ['strike']
      this.pending.push({ kind: 'cadence' })
    }
    if (s === 'pivot') this.flank = false
    this.stance = s
    this.stanceTime = 0
  }

  attack(id: string | null): void {
    this.target = id && this.foes.get(id)?.alive ? id : null
  }

  // The chance a landed strike is vital, from the skill, the weapon and what set it up
  vitalChance(): number {
    let p = 0.12 + 0.07 * this.skill + this.opts.weapon.aim
    if (this.flank) p += 0.15
    if (this.opening) p += 0.2
    if (this.cadence) p += 0.2
    return Math.min(0.85, p)
  }

  // The chance a strike lands at all
  hitChance(): number {
    return Math.min(0.95, 0.55 + 0.08 * this.skill)
  }

  private pending: CombatEvent[] = []

  // Advances the fight by dt seconds with the character at hero; returns
  // what happened. The host moves the character (toward its target when
  // out of reach: wantsApproach) and the foes move here.
  step(dt: number, hero: { x: number; z: number }): CombatEvent[] {
    const out = this.pending
    this.pending = []
    if (this.over) return out
    this.time += dt
    this.stanceTime += dt
    this.cooldown = Math.max(0, this.cooldown - dt)
    if (this.target && !this.foes.get(this.target)?.alive) this.target = null
    // Pivoting round the target sets up a flank strike
    if (this.stance === 'pivot' && this.stanceTime > 0.4 && this.target) this.flank = true

    // The character strikes
    const t = this.target ? this.foes.get(this.target) : undefined
    if (t && this.stance === 'strike' && this.cooldown <= 0 && dist(t, hero) <= this.opts.weapon.reach + 0.3) {
      this.cooldown = this.opts.weapon.swing
      if (this.rand() >= this.hitChance()) out.push({ kind: 'miss', foe: t.id })
      else {
        const vital = this.rand() < this.vitalChance()
        this.flank = this.opening = this.cadence = false
        if (vital) {
          t.alive = false
          out.push({ kind: 'kill', foe: t.id, vital: this.opts.weapon.kind === 'polearm' ? 'heart' : 'neck' })
        } else if (t.kind.vitalOnly) {
          t.stagger = 0.5
          out.push({ kind: 'futile', foe: t.id })
        } else {
          t.hp -= 1
          if (t.hp <= 0) {
            t.alive = false
            out.push({ kind: 'kill', foe: t.id, vital: null })
          } else out.push({ kind: 'hurt', foe: t.id, damage: 1 })
        }
      }
    }

    // The foes drive for the character and claw
    for (const f of this.foes.values()) {
      if (!f.alive) continue
      f.cooldown = Math.max(0, f.cooldown - dt)
      if (f.stagger > 0) {
        f.stagger -= dt
        continue
      }
      const d = dist(f, hero)
      if (f.telling > 0) {
        f.telling -= dt
        if (f.telling <= 0) this.claw(f, d, out)
        continue
      }
      if (d > f.kind.reach) {
        this.move(f, hero, Math.min(f.kind.speed * dt, d - f.kind.reach * 0.9))
      } else if (f.cooldown <= 0) {
        f.telling = f.kind.tell
        if (this.opts.skills.reading >= 1) out.push({ kind: 'tell', foe: f.id })
      }
    }
    if (this.hp <= 0) out.push({ kind: 'fallen' })
    return out
  }

  // Whether the character should close on its target
  wantsApproach(hero: { x: number; z: number }): { x: number; z: number } | null {
    const t = this.target ? this.foes.get(this.target) : undefined
    if (!t || !t.alive || this.stance === 'guard') return null
    return dist(t, hero) > this.opts.weapon.reach ? { x: t.x, z: t.z } : null
  }

  private claw(f: Foe, d: number, out: CombatEvent[]): void {
    f.cooldown = f.kind.clawEvery
    if (d > f.kind.reach + 0.4) {
      out.push({ kind: 'claw', foe: f.id, damage: 0, dodged: true }) // stepped out of reach
      return
    }
    const read = this.opts.skills.reading >= 1
    if (this.stance === 'guard') {
      // the knife arm catches the reaching hands; read in time, it is an opening
      const damage = read ? 0 : Math.round(f.kind.claw * 0.2)
      if (read) this.opening = true
      this.hp -= damage
      out.push({ kind: 'claw', foe: f.id, damage, blocked: true, read })
      return
    }
    if (this.stance === 'pivot' && this.rand() < 0.5 + 0.05 * this.skill) {
      out.push({ kind: 'claw', foe: f.id, damage: 0, dodged: true })
      return
    }
    this.hp -= f.kind.claw
    out.push({ kind: 'claw', foe: f.id, damage: f.kind.claw })
  }

  // A step toward the character, sliding along a blocked axis
  private move(f: Foe, to: { x: number; z: number }, len: number): void {
    if (len <= 0) return
    const dx = to.x - f.x
    const dz = to.z - f.z
    const d = Math.hypot(dx, dz) || 1
    const ok = this.opts.walkable ?? (() => true)
    const nx = f.x + (dx / d) * len
    const nz = f.z + (dz / d) * len
    if (ok(nx, nz)) {
      f.x = nx
      f.z = nz
    } else if (ok(nx, f.z)) f.x = nx
    else if (ok(f.x, nz)) f.z = nz
  }
}

function dist(a: { x: number; z: number }, b: { x: number; z: number }): number {
  return Math.hypot(a.x - b.x, a.z - b.z)
}
