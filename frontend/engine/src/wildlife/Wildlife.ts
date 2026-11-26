import * as THREE from 'three/webgpu'
import { mulberry32 } from '../voxel/Noise.ts'
import { PLANET } from '../voxel/Bodies.ts'

// Wildlife and water on the planet with air (forge 360 V12): a sea that
// fills its hollows, fish to catch from the shore, and herds of grazers
// to hunt (the user, 2026-11-26: plant food, fish and hunt on planets with
// air). The grazers are the same for everyone, placed and moved from the
// world's seed and the clock; one taken is gone for everyone a while (the
// server's game.wildlife).

export const SEA_RADIUS = PLANET.radius - 2.5
export const RESPAWN_MS = 300_000 // a grazer taken comes back after five minutes
export const HUNT_REACH = 2.6
export const FLEE_RANGE = 9

export interface Vec3 {
  x: number
  y: number
  z: number
}

export interface Grazer {
  id: string
  home: Vec3 // a unit direction from the planet's centre
  phase: number
  speed: number // radians a second round its circle
  roam: number // metres
}

// The herd: 12 grazers in three groups, from the world's seed
export function herdOf(seed: number): Grazer[] {
  const rand = mulberry32((seed * 2654435761 + 17) >>> 0)
  const out: Grazer[] = []
  for (let g = 0; g < 3; g++) {
    // a group's centre in the northern half, where the outpost-side landings are
    const lat = 0.25 + rand() * 0.9
    const lon = rand() * Math.PI * 2
    for (let i = 0; i < 4; i++) {
      const la = lat + (rand() - 0.5) * 0.12
      const lo = lon + (rand() - 0.5) * 0.12
      out.push({
        id: `g${g}${i}`,
        home: { x: Math.cos(la) * Math.cos(lo), y: Math.sin(la), z: Math.cos(la) * Math.sin(lo) },
        phase: rand() * Math.PI * 2,
        speed: 0.06 + rand() * 0.05,
        roam: 4 + rand() * 5,
      })
    }
  }
  return out
}

// Where a grazer stands at a time, as a direction from the planet's centre
export function grazerDir(g: Grazer, t: number): Vec3 {
  const h = g.home
  // a frame on the tangent plane
  const ref = Math.abs(h.y) < 0.9 ? { x: 0, y: 1, z: 0 } : { x: 1, y: 0, z: 0 }
  const e1 = norm(cross(ref, h))
  const e2 = cross(h, e1)
  const a = g.phase + t * g.speed
  const r = (g.roam / PLANET.radius) * (0.7 + 0.3 * Math.sin(a * 2.3))
  return norm({ x: h.x + (e1.x * Math.cos(a) + e2.x * Math.sin(a)) * r, y: h.y + (e1.y * Math.cos(a) + e2.y * Math.sin(a)) * r, z: h.z + (e1.z * Math.cos(a) + e2.z * Math.sin(a)) * r })
}

// Whether a grazer taken at a time is back
export function available(takenAt: number | undefined, now: number): boolean {
  return takenAt === undefined || now - takenAt >= RESPAWN_MS
}

// Fishing: cast, wait for a bite, then strike within the window
export type FishEvent = 'bite' | 'escaped' | 'caught' | 'missed'

export class Fishing {
  state: 'idle' | 'waiting' | 'bite' = 'idle'
  private t = 0
  private wait = 0
  private readonly rand: () => number
  static readonly WINDOW = 1.6

  constructor(seed = 1) {
    this.rand = mulberry32(seed)
  }

  cast(): void {
    this.state = 'waiting'
    this.t = 0
    this.wait = 3 + this.rand() * 5
  }

  step(dt: number): FishEvent | null {
    if (this.state === 'idle') return null
    this.t += dt
    if (this.state === 'waiting' && this.t >= this.wait) {
      this.state = 'bite'
      this.t = 0
      return 'bite'
    }
    if (this.state === 'bite' && this.t > Fishing.WINDOW) {
      this.state = 'idle'
      return 'escaped'
    }
    return null
  }

  // The player strikes: a catch on a bite, else the line comes up empty
  strike(): FishEvent {
    const ev = this.state === 'bite' ? 'caught' : 'missed'
    this.state = 'idle'
    return ev
  }

  reel(): void {
    this.state = 'idle'
  }
}

// The sea: a sphere just under the planet's middle ground, filling its hollows
export function seaMesh(): THREE.Mesh {
  const m = new THREE.Mesh(
    new THREE.SphereGeometry(SEA_RADIUS, 96, 64),
    new THREE.MeshStandardNodeMaterial({ color: 0x2f6f9a, roughness: 0.15, metalness: 0.1, transparent: true, opacity: 0.78 }),
  )
  m.position.set(PLANET.x, PLANET.y, PLANET.z)
  m.renderOrder = 1
  return m
}

// A grazer: a placeholder six-legged beast with a pale back
export function grazerModel(): THREE.Group {
  const g = new THREE.Group()
  const hide = new THREE.MeshStandardNodeMaterial({ color: 0x8a6a48, roughness: 0.9 })
  const pale = new THREE.MeshStandardNodeMaterial({ color: 0xd8c8a0, roughness: 0.9 })
  const body = new THREE.Mesh(new THREE.BoxGeometry(0.7, 0.55, 1.4), hide)
  body.position.y = 0.75
  const back = new THREE.Mesh(new THREE.BoxGeometry(0.5, 0.12, 1.2), pale)
  back.position.y = 1.08
  const head = new THREE.Mesh(new THREE.BoxGeometry(0.4, 0.4, 0.45), hide)
  head.position.set(0, 0.95, 0.85)
  g.add(body, back, head)
  for (const z of [-0.5, 0, 0.5]) {
    for (const x of [-0.28, 0.28]) {
      const leg = new THREE.Mesh(new THREE.CylinderGeometry(0.06, 0.05, 0.5, 6), hide)
      leg.position.set(x, 0.25, z)
      g.add(leg)
    }
  }
  return g
}

// A fishing float on the water
export function floatModel(): THREE.Mesh {
  return new THREE.Mesh(new THREE.SphereGeometry(0.12, 10, 8), new THREE.MeshStandardNodeMaterial({ color: 0xff4a2a, roughness: 0.5 }))
}

function cross(a: Vec3, b: Vec3): Vec3 {
  return { x: a.y * b.z - a.z * b.y, y: a.z * b.x - a.x * b.z, z: a.x * b.y - a.y * b.x }
}

function norm(a: Vec3): Vec3 {
  const l = Math.hypot(a.x, a.y, a.z) || 1
  return { x: a.x / l, y: a.y / l, z: a.z / l }
}
