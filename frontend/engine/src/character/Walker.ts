// A character on voxel bodies in any gravity (forge 360 V2, a thin 362a
// and 362b): it walks on the ground of whichever body pulls it, with its
// feet toward the pull; it jumps; and with its thrust pack on it flies in
// the view's direction with dampeners that stop it when let go. It knows
// only a terrain to ask (signed distance and gravity), so tests run it
// without a renderer.

export interface Vec {
  x: number
  y: number
  z: number
}

export interface WalkerTerrain {
  // Signed distance in metres, negative inside the ground
  density(x: number, y: number, z: number): number
  gravity(x: number, y: number, z: number, out: Vec): Vec
}

export interface WalkIntent {
  forward: number // -1 to 1
  right: number // -1 to 1
  up: number // -1 to 1: jump on the ground; with the thrust pack, up or down
  sprint: boolean
}

export const WALK_SPEED = 4
export const SPRINT_SPEED = 7
export const JUMP_SPEED = 3.2
export const THRUST = 9 // m/s² from the thrust pack
const RADIUS = 0.35
const HEIGHT = 1.8
export const EYE = 1.6
const SUBSTEP = 1 / 120

const v = (x = 0, y = 0, z = 0): Vec => ({ x, y, z })
const dot = (a: Vec, b: Vec) => a.x * b.x + a.y * b.y + a.z * b.z
const len = (a: Vec) => Math.sqrt(dot(a, a))
const scale = (a: Vec, s: number): Vec => v(a.x * s, a.y * s, a.z * s)
const add = (a: Vec, b: Vec): Vec => v(a.x + b.x, a.y + b.y, a.z + b.z)
const sub = (a: Vec, b: Vec): Vec => v(a.x - b.x, a.y - b.y, a.z - b.z)
const norm = (a: Vec): Vec => {
  const l = len(a)
  return l > 1e-9 ? scale(a, 1 / l) : v(0, 1, 0)
}
const cross = (a: Vec, b: Vec): Vec => v(a.y * b.z - a.z * b.y, a.z * b.x - a.x * b.z, a.x * b.y - a.y * b.x)
// a rotated about the unit axis k by angle t (Rodrigues)
function rotate(a: Vec, k: Vec, t: number): Vec {
  const c = Math.cos(t)
  const s = Math.sin(t)
  return add(add(scale(a, c), scale(cross(k, a), s)), scale(k, dot(k, a) * (1 - c)))
}

export class Walker {
  // The feet
  pos: Vec
  vel = v()
  // Unit vectors: up (away from the pull) and forward along the ground
  up = v(0, 1, 0)
  forward = v(0, 0, -1)
  // The view's angle above the ground plane
  pitch = 0
  thrustPack = false
  grounded = false
  gravity = v()
  private readonly terrain: WalkerTerrain
  private acc = 0

  constructor(terrain: WalkerTerrain, at: Vec) {
    this.terrain = terrain
    this.pos = { ...at }
    this.updateUp(1)
  }

  get right(): Vec {
    return norm(cross(this.forward, this.up))
  }

  // Where the eyes are and which way they look
  eye(): Vec {
    return add(this.pos, scale(this.up, EYE))
  }
  look(): Vec {
    return norm(add(scale(this.forward, Math.cos(this.pitch)), scale(this.up, Math.sin(this.pitch))))
  }

  // Turns the view: yaw about up, pitch toward up (radians)
  turn(yaw: number, pitch: number): void {
    this.forward = norm(rotate(this.forward, this.up, -yaw))
    this.pitch = Math.max(-1.5, Math.min(1.5, this.pitch + pitch))
  }

  step(dt: number, intent: WalkIntent): void {
    this.acc += Math.min(dt, 0.1)
    while (this.acc >= SUBSTEP) {
      this.acc -= SUBSTEP
      this.substep(SUBSTEP, intent)
    }
  }

  private substep(dt: number, intent: WalkIntent): void {
    const t = this.terrain
    t.gravity(this.pos.x, this.pos.y, this.pos.z, this.gravity)
    const g = this.gravity
    this.updateUp(dt)
    const up = this.up
    const right = this.right
    if (this.thrustPack) {
      const dir = add(add(scale(this.look(), intent.forward), scale(right, intent.right)), scale(up, intent.up))
      const want = len(dir) > 1e-6
      this.vel = add(this.vel, scale(add(g, want ? scale(norm(dir), THRUST * (intent.sprint ? 1.6 : 1)) : v()), dt))
      // Dampeners: let go and it stops, gravity aside
      if (!want) this.vel = scale(this.vel, Math.exp(-dt * 2.2))
    } else {
      const speed = intent.sprint ? SPRINT_SPEED : WALK_SPEED
      const dir = add(scale(this.forward, intent.forward), scale(right, intent.right))
      const want = len(dir) > 1e-6 ? scale(norm(dir), speed) : v()
      if (this.grounded) {
        // On the ground: the velocity along it goes to what is wanted at
        // once, so it neither slides nor skates
        const along = sub(this.vel, scale(up, dot(this.vel, up)))
        const k = Math.min(1, dt * 20)
        this.vel = add(this.vel, scale(sub(want, along), k))
        if (intent.up > 0) {
          this.vel = add(this.vel, scale(up, JUMP_SPEED))
          this.grounded = false
        }
      } else if (len(want) > 0) {
        // A little control in the air, never past walking pace
        const along = sub(this.vel, scale(up, dot(this.vel, up)))
        const wn = norm(want)
        if (dot(along, wn) < speed) this.vel = add(this.vel, scale(wn, dt * 2))
      }
      this.vel = add(this.vel, scale(g, dt))
    }
    const wasGrounded = this.grounded
    this.pos = add(this.pos, scale(this.vel, dt))
    this.collide()
    // Walking over a crest or down a slope keeps the feet on the ground
    // rather than launching them in the weak pull
    if (wasGrounded && !this.grounded && !this.thrustPack && dot(this.vel, up) < 0.5) this.snapDown()
  }

  private snapDown(): void {
    const c = add(this.pos, scale(this.up, RADIUS))
    const d = this.terrain.density(c.x, c.y, c.z)
    if (d > RADIUS + 0.45) return
    const n = this.normalAt(c)
    if (dot(n, this.up) < 0.5) return
    this.pos = sub(this.pos, scale(n, d - RADIUS))
    const off = dot(this.vel, n)
    if (off > 0) this.vel = sub(this.vel, scale(n, off))
    this.grounded = true
  }

  // Up turns toward away-from-the-pull; forward follows, kept along the ground
  private updateUp(dt: number): void {
    const gl = len(this.gravity)
    if (gl < 0.02) return // weightless: keep the frame
    const target = scale(this.gravity, -1 / gl)
    const k = Math.min(1, dt * 6)
    this.up = norm(add(this.up, scale(sub(target, this.up), k)))
    const f = sub(this.forward, scale(this.up, dot(this.forward, this.up)))
    this.forward = len(f) > 1e-6 ? norm(f) : norm(cross(this.up, v(1, 0, 0)))
  }

  // Pushes the body out of the ground: spheres at the feet, middle and head
  private collide(): void {
    this.grounded = false
    const t = this.terrain
    for (let iter = 0; iter < 3; iter++) {
      let moved = false
      for (const h of [RADIUS, HEIGHT / 2, HEIGHT - RADIUS]) {
        const c = add(this.pos, scale(this.up, h))
        const d = t.density(c.x, c.y, c.z)
        if (d >= RADIUS + (h === RADIUS ? 0.06 : 0)) continue
        const n = this.normalAt(c)
        // standing on it, not rising away from it after a jump
        if (h === RADIUS && dot(n, this.up) > 0.5 && dot(this.vel, this.up) < 0.5) this.grounded = true
        if (d >= RADIUS) continue
        this.pos = add(this.pos, scale(n, RADIUS - d))
        const into = dot(this.vel, n)
        if (into < 0) this.vel = sub(this.vel, scale(n, into))
        moved = true
      }
      if (!moved) break
    }
  }

  private normalAt(c: Vec): Vec {
    const t = this.terrain
    const e = 0.1
    return norm(
      v(
        t.density(c.x + e, c.y, c.z) - t.density(c.x - e, c.y, c.z),
        t.density(c.x, c.y + e, c.z) - t.density(c.x, c.y - e, c.z),
        t.density(c.x, c.y, c.z + e) - t.density(c.x, c.y, c.z - e),
      ),
    )
  }
}

// A place to stand: the surface below a point, found by stepping toward
// the pull from it
export function surfaceBelow(terrain: WalkerTerrain, from: Vec, maxDistance = 400): Vec | null {
  const g = terrain.gravity(from.x, from.y, from.z, v())
  const down = norm(g)
  let p = { ...from }
  for (let s = 0; s < maxDistance; s += 0.25) {
    if (terrain.density(p.x, p.y, p.z) < 0.05) return add(p, scale(down, -0.1))
    p = add(p, scale(down, 0.25))
  }
  return null
}
