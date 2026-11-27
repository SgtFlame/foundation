// Ships (forge 360 V3, a thin 364, 365 and 369): a grid of 1 m blocks laid
// out from a premade design, placed block by block, then flown by a pilot
// in its cockpit. This module holds the design and state types, the
// little quaternion maths they need, and ShipBody, the flight model: only
// placed thrusters push and only a placed gyroscope turns it, inertial
// dampeners hold it still against the pull of whatever body is near, and
// it rests on the ground rather than sinking into it. No rendering, so it
// runs in Node tests.

export type Vec3 = { x: number; y: number; z: number }
export type Quat = [number, number, number, number] // x, y, z, w

export interface DesignBlock {
  type: 'armor' | 'cockpit' | 'gyro' | 'light' | 'thruster' | 'battery' | 'cargo' | 'refinery' | 'solar' | 'log' | 'door' | 'bed' | 'bench' | 'forge' | 'fabricator' // V13: a building's (V14: the bench, V20: the forge, V23: the fabricator)
  at: [number, number, number]
  push?: 'up' | 'down' | 'forward' | 'back' | 'left' | 'right'
}

export interface ShipDesign {
  id: string
  title: string
  description: string
  blockSize: number
  blocks: DesignBlock[]
  static?: boolean // V13: a building, never flown; the walker cannot pass its blocks but for doors
  lift?: number // V25: the share of its holds its stock thrusters lift (kits add their boost), up to full
}

// A ship as the server keeps it (game.worldShips)
export interface ShipState {
  id: string
  world: string
  design: string
  blocks: number
  placed: number[]
  x: number
  y: number
  z: number
  q: Quat
  v: [number, number, number]
  pilot: string
  crew?: string[] // V7: the users riding as its crew
  cargo?: { dust: number; rock: number; iron: number; ore?: number } // V4: its hold, m³ (V8: ore)
  charge?: number // V4: its battery, 0 to 1
  forge?: { ore: number; at: number } // V20: the ore smelting in a cabin's forge
  fab?: { item: string; at: number } // V23: the circuit board a cabin's fabricator is making
  boost?: number // V24: what ion thruster kits add to its thrust
  jump?: boolean // V27: a jump drive is fitted
  seq: number
  updatedAt?: string
}

export const BLOCK_MASS: Record<DesignBlock['type'], number> = { armor: 120, cockpit: 200, gyro: 150, light: 20, thruster: 80, battery: 150, cargo: 100, refinery: 300, solar: 30, log: 200, door: 60, bed: 80, bench: 90, forge: 400, fabricator: 250 }

// Power (V4, a thin 367): a design with a battery or solar panels runs on
// power. Thrusters draw by how hard they push, the gyroscope while turning;
// panels give a steady trickle. A battery stores it; without one only what
// the panels give right now drives the ship. A design with no power blocks
// (the skiff) flies without counting it.
export const THRUSTER_KW = 10 // at full push
export const GYRO_KW = 2
export const SOLAR_KW = 2
export const BATTERY_KJ = 3000
export const CARGO_PER_HOLD = 6 // m³; the loadShip blueprint holds the same

// V25: the share of a design's holds its thrusters lift with the ion kits'
// boost fitted (the ore barge: half, a quarter more for each kit), as the
// loadShip blueprint has it
// V26: what a m³ in the hold weighs (kg): a full hold is heavy, so a
// loaded ship climbs slower, and the ore barge's thrusters cannot lift more
// than half its hold without ion kits, as the loadShip rule says
export const CARGO_KG = 300

// V26: all a ship's hold carries, m³
export function cargoOf(s: Pick<ShipState, 'cargo'>): number {
  const c = s.cargo
  return c ? c.dust + c.rock + c.iron + (c.ore ?? 0) : 0
}

export function liftOf(design: Pick<ShipDesign, 'lift'>, boost = 0): number {
  return design.lift ? Math.min(1, design.lift + boost) : 1
}

// Crashes (V5, a thin 366): striking the ground faster than SAFE_IMPACT
// (m/s, along up) breaks some of the blocks that struck, all of them at
// WRECK_IMPACT; broken blocks are gone until placed again from the outline
export const SAFE_IMPACT = 5
export const WRECK_IMPACT = 12
export const THRUST = 6000 // newtons per thruster
export const TURN_RATE = 1.4 // radians a second with a gyroscope

export const PUSH: Record<NonNullable<DesignBlock['push']>, Vec3> = {
  up: { x: 0, y: 1, z: 0 },
  down: { x: 0, y: -1, z: 0 },
  forward: { x: 0, y: 0, z: -1 },
  back: { x: 0, y: 0, z: 1 },
  left: { x: -1, y: 0, z: 0 },
  right: { x: 1, y: 0, z: 0 },
}

// --- quaternion maths ---

export function rotate(q: Quat, v: Vec3): Vec3 {
  const [x, y, z, w] = q
  // t = 2 * cross(q.xyz, v)
  const tx = 2 * (y * v.z - z * v.y)
  const ty = 2 * (z * v.x - x * v.z)
  const tz = 2 * (x * v.y - y * v.x)
  return { x: v.x + w * tx + (y * tz - z * ty), y: v.y + w * ty + (z * tx - x * tz), z: v.z + w * tz + (x * ty - y * tx) }
}

export function conj(q: Quat): Quat {
  return [-q[0], -q[1], -q[2], q[3]]
}

export function mul(a: Quat, b: Quat): Quat {
  const [ax, ay, az, aw] = a
  const [bx, by, bz, bw] = b
  return [aw * bx + ax * bw + ay * bz - az * by, aw * by - ax * bz + ay * bw + az * bx, aw * bz + ax * by - ay * bx + az * bw, aw * bw - ax * bx - ay * by - az * bz]
}

export function axisAngle(axis: Vec3, angle: number): Quat {
  const s = Math.sin(angle / 2)
  return [axis.x * s, axis.y * s, axis.z * s, Math.cos(angle / 2)]
}

export function normalize(q: Quat): Quat {
  const l = Math.hypot(...q) || 1
  return [q[0] / l, q[1] / l, q[2] / l, q[3] / l]
}

// The orientation whose local y is up and local -z faces forward (forward
// need not be square to up)
export function lookQuat(forward: Vec3, up: Vec3): Quat {
  const u = unit(up)
  let f = sub(forward, scale(u, dot(forward, u)))
  if (dot(f, f) < 1e-9) f = Math.abs(u.y) < 0.9 ? cross(u, { x: 0, y: 1, z: 0 }) : cross(u, { x: 1, y: 0, z: 0 })
  f = unit(f)
  const r = cross(f, u) // local +x
  const b = scale(f, -1) // local +z
  // rotation matrix columns r, u, b
  const m00 = r.x, m01 = u.x, m02 = b.x
  const m10 = r.y, m11 = u.y, m12 = b.y
  const m20 = r.z, m21 = u.z, m22 = b.z
  const tr = m00 + m11 + m22
  let q: Quat
  if (tr > 0) {
    const s = 0.5 / Math.sqrt(tr + 1)
    q = [(m21 - m12) * s, (m02 - m20) * s, (m10 - m01) * s, 0.25 / s]
  } else if (m00 > m11 && m00 > m22) {
    const s = 2 * Math.sqrt(1 + m00 - m11 - m22)
    q = [0.25 * s, (m01 + m10) / s, (m02 + m20) / s, (m21 - m12) / s]
  } else if (m11 > m22) {
    const s = 2 * Math.sqrt(1 + m11 - m00 - m22)
    q = [(m01 + m10) / s, 0.25 * s, (m12 + m21) / s, (m02 - m20) / s]
  } else {
    const s = 2 * Math.sqrt(1 + m22 - m00 - m11)
    q = [(m02 + m20) / s, (m12 + m21) / s, 0.25 * s, (m10 - m01) / s]
  }
  return normalize(q)
}

const dot = (a: Vec3, b: Vec3) => a.x * b.x + a.y * b.y + a.z * b.z
const cross = (a: Vec3, b: Vec3): Vec3 => ({ x: a.y * b.z - a.z * b.y, y: a.z * b.x - a.x * b.z, z: a.x * b.y - a.y * b.x })
const sub = (a: Vec3, b: Vec3): Vec3 => ({ x: a.x - b.x, y: a.y - b.y, z: a.z - b.z })
const scale = (a: Vec3, k: number): Vec3 => ({ x: a.x * k, y: a.y * k, z: a.z * k })
const unit = (a: Vec3): Vec3 => scale(a, 1 / (Math.hypot(a.x, a.y, a.z) || 1))

// Where a block's centre is in the world
export function blockWorld(state: { x: number; y: number; z: number; q: Quat }, b: DesignBlock, size = 1): Vec3 {
  const o = rotate(state.q, { x: b.at[0] * size, y: b.at[1] * size, z: b.at[2] * size })
  return { x: state.x + o.x, y: state.y + o.y, z: state.z + o.z }
}

// V13: the signed distance from a point to a building's nearest placed
// block (doors are open; a bed is a slab half a block high), Infinity with none
export function buildingDistance(design: ShipDesign, placed: Iterable<number>, pose: { x: number; y: number; z: number; q: Quat }, p: Vec3): number {
  const size = design.blockSize
  const local = rotate(conj(pose.q), { x: p.x - pose.x, y: p.y - pose.y, z: p.z - pose.z })
  let best = Infinity
  for (const i of placed) {
    const b = design.blocks[i]
    if (!b || b.type === 'door') continue
    const h = b.type === 'bed' ? 0.25 : b.type === 'bench' ? 0.4 : 0.5
    const cy = b.at[1] * size
    const dx = Math.abs(local.x - b.at[0] * size) - 0.5 * size
    const dy = Math.abs(local.y - cy) - h * size
    const dz = Math.abs(local.z - b.at[2] * size) - 0.5 * size
    const d = Math.hypot(Math.max(dx, 0), Math.max(dy, 0), Math.max(dz, 0)) + Math.min(Math.max(dx, dy, dz), 0)
    if (d < best) best = d
  }
  return best
}

// What the terrain answers (the walker's terrain): density is the signed
// distance to the surface, negative inside the ground
export interface ShipTerrain {
  density(x: number, y: number, z: number): number
  gravity(x: number, y: number, z: number, out: Vec3): Vec3
}

export interface Helm {
  forward: number // -1 to 1
  right: number
  up: number
  pitch: number // turn rates -1 to 1
  yaw: number
  roll: number
}

export const IDLE_HELM: Helm = { forward: 0, right: 0, up: 0, pitch: 0, yaw: 0, roll: 0 }

// The flight model of one ship: its placed blocks decide its mass, its
// thrust each way and whether it can turn
export class ShipBody {
  x: number
  y: number
  z: number
  q: Quat
  v: Vec3
  grounded = false
  // What its placed blocks give; refit when blocks break (V5)
  mass = 1
  thrust: Record<NonNullable<DesignBlock['push']>, number> = { up: 0, down: 0, forward: 0, back: 0, left: 0, right: 0 }
  canTurn = false
  // Power (V4): whether it counts power, its battery store (kJ, 0 without
  // one), what its panels give (kW), and its charge, 0 to 1
  readonly powered: boolean
  store = 0
  solar = 0
  charge: number
  // what the last step drew (kW) and the share of what it asked for that it got
  draw = 0
  power = 1
  private readonly design: ShipDesign
  private placed: DesignBlock[] = []
  private placedIdx: number[] = []
  // blocks broken in crashes since the host last took them (V5)
  private broken: number[] = []
  private readonly terrain: ShipTerrain

  // V26: what its hold carries (m³), part of its mass
  private load = 0
  private blockMass = 1

  constructor(design: ShipDesign, placed: readonly number[], state: { x: number; y: number; z: number; q: Quat; v?: [number, number, number]; charge?: number; load?: number }, terrain: ShipTerrain) {
    this.design = design
    this.terrain = terrain
    this.load = Math.max(0, state.load ?? 0)
    this.powered = design.blocks.some((b) => b.type === 'battery' || b.type === 'solar')
    this.refit(placed)
    this.charge = Math.min(1, Math.max(0, state.charge ?? 1))
    this.x = state.x
    this.y = state.y
    this.z = state.z
    this.q = normalize(state.q)
    this.v = { x: state.v?.[0] ?? 0, y: state.v?.[1] ?? 0, z: state.v?.[2] ?? 0 }
  }

  // Its mass, thrust, turning and power from the blocks placed
  private refit(placed: readonly number[]): void {
    this.placedIdx = placed.filter((i) => this.design.blocks[i])
    this.placed = this.placedIdx.map((i) => this.design.blocks[i])
    this.blockMass = this.placed.reduce((m, b) => m + BLOCK_MASS[b.type], 0) || 1
    this.mass = this.blockMass + this.load * CARGO_KG
    this.thrust = { up: 0, down: 0, forward: 0, back: 0, left: 0, right: 0 }
    for (const b of this.placed) if (b.type === 'thruster' && b.push) this.thrust[b.push] += THRUST
    this.canTurn = this.placed.some((b) => b.type === 'gyro')
    this.store = this.placed.filter((b) => b.type === 'battery').length * BATTERY_KJ
    this.solar = this.placed.filter((b) => b.type === 'solar').length * SOLAR_KW
  }

  // V26: what its hold now carries (m³), which it weighs
  setLoad(m3: number): void {
    this.load = Math.max(0, m3)
    this.mass = this.blockMass + this.load * CARGO_KG
  }

  // Whether its thrusters (by the pilot's scale) can lift it against a pull (m/s²)
  canLift(g: number): boolean {
    return this.reach().y[1] > g
  }

  // The design's indices of the blocks still placed
  blocks(): number[] {
    return [...this.placedIdx]
  }

  // The blocks broken since this was last asked, by design index
  takeBroken(): number[] {
    const out = this.broken
    this.broken = []
    return out
  }

  // The most acceleration (m/s²) its thrusters give along a local axis, each way
  reach(): { x: [number, number]; y: [number, number]; z: [number, number] } {
    const m = this.mass / this.thrustScale
    const t = this.thrust
    return { x: [-t.left / m, t.right / m], y: [-t.down / m, t.up / m], z: [-t.forward / m, t.back / m] }
  }

  // How much harder the thrusters answer this pilot (V6: piloting levels)
  thrustScale = 1

  // The panels' charge over a time it was left parked
  recharge(seconds: number): void {
    if (this.store > 0 && seconds > 0) this.charge = Math.min(1, this.charge + (this.solar * seconds) / this.store)
  }

  // The share of what it asks for that its power gives, charging or
  // draining the battery by what it draws
  private supply(dt: number, demand: number): number {
    if (!this.powered) return 1
    let share = 1
    if (this.store > 0) {
      const have = this.charge * this.store
      if (demand > this.solar && (demand - this.solar) * dt > have) share = (this.solar + have / dt) / demand
      const c = (have + (this.solar - demand * share) * dt) / this.store
      this.charge = c < 1e-9 ? 0 : Math.min(1, c)
    } else if (demand > 0) {
      share = Math.min(1, this.solar / demand)
    }
    this.draw = demand * share
    this.power = share
    return share
  }

  step(dt: number, helm: Helm): void {
    const g = this.terrain.gravity(this.x, this.y, this.z, { x: 0, y: 0, z: 0 })
    const inv = conj(this.q)
    const gl = rotate(inv, g)
    const vl = rotate(inv, this.v)
    const r = this.reach()
    // Each local axis: the helm's push, or the dampeners holding it still against the pull
    const axis = (input: number, v: number, gv: number, [lo, hi]: [number, number]) => {
      const want = input !== 0 ? (input > 0 ? hi : -lo) * input : -v / 0.4 - gv
      return Math.max(lo, Math.min(hi, want))
    }
    const want = { x: axis(helm.right, vl.x, gl.x, r.x), y: axis(helm.up, vl.y, gl.y, r.y), z: axis(-helm.forward, vl.z, gl.z, r.z) }
    const turning = this.canTurn && (helm.yaw !== 0 || helm.pitch !== 0 || helm.roll !== 0)
    const pushing = ((Math.abs(want.x) + Math.abs(want.y) + Math.abs(want.z)) * this.mass) / THRUST
    const share = this.supply(dt, pushing * THRUSTER_KW + (turning ? GYRO_KW : 0))
    // Turning, about the ship's own axes, only with a gyroscope
    if (turning) {
      const k = TURN_RATE * dt * share
      const turn = mul(mul(axisAngle({ x: 0, y: 1, z: 0 }, -helm.yaw * k), axisAngle({ x: 1, y: 0, z: 0 }, helm.pitch * k)), axisAngle({ x: 0, y: 0, z: 1 }, -helm.roll * k))
      this.q = normalize(mul(this.q, turn))
    }
    const al = { x: want.x * share, y: want.y * share, z: want.z * share }
    const a = rotate(this.q, al)
    this.v = { x: this.v.x + (a.x + g.x) * dt, y: this.v.y + (a.y + g.y) * dt, z: this.v.z + (a.z + g.z) * dt }
    this.x += this.v.x * dt
    this.y += this.v.y * dt
    this.z += this.v.z * dt
    this.collide(g)
  }

  // Lifts it out of the ground along up (away from the pull), stopping
  // what drove it in, with a little friction
  private collide(g: Vec3): void {
    const up = unit(scale(g, -1))
    let deepest = 0
    let touching = false
    const hit: { index: number; depth: number }[] = []
    for (const [k, b] of this.placed.entries()) {
      const p = blockWorld(this, b, this.design.blockSize)
      // the block's lower face, half a block below its centre along up
      const base = sub(p, scale(up, 0.5 * this.design.blockSize))
      // resting within a few centimetres of the ground still counts as on it
      if (this.terrain.density(base.x - up.x * 0.03, base.y - up.y * 0.03, base.z - up.z * 0.03) < 0) touching = true
      if (this.terrain.density(base.x, base.y, base.z) >= 0) continue
      let lift = 0
      const solid = (k: number) => this.terrain.density(base.x + up.x * k, base.y + up.y * k, base.z + up.z * k) < 0
      while (lift < 1.5 && solid(lift)) lift += 0.05
      // then close in on the surface, so it rests on it rather than hopping
      let lo = Math.max(0, lift - 0.05)
      for (let i = 0; i < 6; i++) {
        const mid = (lo + lift) / 2
        if (solid(mid)) lo = mid
        else lift = mid
      }
      deepest = Math.max(deepest, lift)
      hit.push({ index: this.placedIdx[k], depth: lift })
    }
    this.grounded = touching || deepest > 0
    if (!deepest) return
    // A hard landing breaks the blocks that struck, the deepest first (V5)
    const impact = -dot(this.v, up)
    if (impact > SAFE_IMPACT && hit.length) {
      const count = Math.ceil(Math.min(1, (impact - SAFE_IMPACT) / (WRECK_IMPACT - SAFE_IMPACT)) * hit.length)
      const gone = hit.sort((a, b) => b.depth - a.depth).slice(0, count).map((h) => h.index)
      this.broken.push(...gone)
      this.refit(this.placedIdx.filter((i) => !gone.includes(i)))
    }
    this.x += up.x * deepest
    this.y += up.y * deepest
    this.z += up.z * deepest
    const vn = dot(this.v, up)
    if (vn < 0) this.v = sub(this.v, scale(up, vn))
    this.v = scale(this.v, 0.9)
  }

  state(): { x: number; y: number; z: number; q: Quat; v: [number, number, number] } {
    return { x: this.x, y: this.y, z: this.z, q: [...this.q] as Quat, v: [this.v.x, this.v.y, this.v.z] }
  }

  // Where its cockpit's seat is in the world, and the way the seat faces
  cockpit(): { at: Vec3; forward: Vec3; up: Vec3 } | null {
    const c = this.design.blocks.find((b) => b.type === 'cockpit')
    if (!c) return null
    return { at: blockWorld(this, c, this.design.blockSize), forward: rotate(this.q, PUSH.forward), up: rotate(this.q, PUSH.up) }
  }
}
