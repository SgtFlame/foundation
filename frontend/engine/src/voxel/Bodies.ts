import { MoonGenerator } from './MoonGenerator.ts'
import { mulberry32 } from './Noise.ts'
import type { MoonParams } from './Types.ts'

// The bodies of a world (forge 360 V2, a thin 361a): the moon at the
// origin and, when the generator asks for them, small asteroids floating
// near it. Each body is a MoonGenerator in its own frame; the world's
// density is their union (the smallest signed distance), so the chunk
// sampler, the mesher and collisions treat them as one terrain.

export interface Body {
  kind: 'moon' | 'asteroid' | 'planet'
  // Centre in world metres
  x: number
  y: number
  z: number
  radius: number
  gen: MoonGenerator
  // Bound on the surface's distance from the centre
  reach: number
  // The pull at the surface, m/s²
  surfaceG: number
}

// Surface gravity of a moon of the default 100 m radius; it scales with
// size for bodies of one density (g = k·r). Asteroids are dense, iron-rich
// rock: at least ASTEROID_GRAVITY, so a player can walk on one.
export const MOON_GRAVITY = 1.62
export const ASTEROID_GRAVITY = 0.35
const DENSITY_K = MOON_GRAVITY / 100
// Beyond its highest ground a body's pull falls off steeply (as the 7th
// power of distance, as in engineering sandboxes), so a nearby asteroid's
// own pull wins over the moon's a short way up
const FALLOFF = 7

// The asteroids a seed places: each 8 to 16 m in radius, 50 to 90 m
// above the moon's mean surface, in directions spread from the seed
export function asteroidsOf(params: MoonParams): Body[] {
  const n = Math.max(0, Math.min(8, Math.floor(params.asteroids ?? 0)))
  const rand = mulberry32(params.seed * 104729 + 7)
  const out: Body[] = []
  for (let i = 0; i < n; i++) {
    const r = 8 + rand() * 8
    // The first one sits above the moon's north pole, where a new player
    // starts, so it is in sight at once; the rest anywhere
    const u = i === 0 ? 0.92 + rand() * 0.06 : rand() * 2 - 1
    const a = rand() * Math.PI * 2
    const s = Math.sqrt(1 - u * u)
    const d = params.radius + params.amplitude + 50 + rand() * 40 + r
    const gen = new MoonGenerator({
      seed: (params.seed * 31 + i * 977 + 1) >>> 0,
      voxelSize: params.voxelSize,
      radius: r,
      amplitude: r * 0.22,
      frequency: 0.09,
      octaves: 3,
      craters: 5,
      craterMaxRadius: r * 0.45,
      dustDepth: 0.6,
      // V8: the asteroids carry the ore the moon lacks
      ore: 0.6,
    })
    out.push(body('asteroid', s * Math.cos(a) * d, u * d, s * Math.sin(a) * d, r, gen))
  }
  return out
}

// V9: the planet with air, a short flight from the moon (a jump drive comes
// later): green soil, gentle hills, no craters, at PLANET's place for every
// world. The server's play graphs allow points near it too.
export const PLANET = { x: 1600, y: 0, z: 0, radius: 140, amplitude: 9, air: 60 }

export function planetOf(params: MoonParams): Body {
  const gen = new MoonGenerator({
    seed: (params.seed * 7919 + 3) >>> 0,
    voxelSize: params.voxelSize,
    radius: PLANET.radius,
    amplitude: PLANET.amplitude,
    frequency: 0.012,
    octaves: 4,
    craters: 0,
    craterMaxRadius: 1,
    dustDepth: 1.5,
    soil: true,
  })
  return body('planet', PLANET.x, PLANET.y, PLANET.z, PLANET.radius, gen)
}

function body(kind: Body['kind'], x: number, y: number, z: number, radius: number, gen: MoonGenerator): Body {
  const g = DENSITY_K * radius
  return { kind, x, y, z, radius, gen, reach: radius + gen.maxOutward, surfaceG: kind === 'asteroid' ? Math.max(ASTEROID_GRAVITY, g) : g }
}

// The world's terrain: every body's density joined. It has the
// generator's surface for the chunk sampler (params, beginRegion,
// density, material, endRegion).
export class WorldGenerator {
  readonly params: MoonParams
  readonly bodies: Body[]
  private active: Body[]
  private skipped: Body[] = []

  constructor(params: MoonParams) {
    this.params = params
    const moon = new MoonGenerator(params)
    this.bodies = [body('moon', 0, 0, 0, params.radius, moon), ...asteroidsOf(params), planetOf(params)]
    this.active = this.bodies
  }

  get moon(): MoonGenerator {
    return this.bodies[0].gen
  }

  // Bodies whose surface may cross an axis-aligned box (metres)
  mayHaveSurface(x0: number, y0: number, z0: number, x1: number, y1: number, z1: number): boolean {
    const cx = (x0 + x1) / 2
    const cy = (y0 + y1) / 2
    const cz = (z0 + z1) / 2
    const half = Math.sqrt((x1 - x0) ** 2 + (y1 - y0) ** 2 + (z1 - z0) ** 2) / 2
    for (const b of this.bodies) {
      const rho = Math.hypot(cx - b.x, cy - b.y, cz - b.z)
      const inward = b.radius - b.gen.maxInward
      if (rho - half > b.reach) continue
      if (rho + half < inward) continue
      return true
    }
    return false
  }

  beginRegion(x0: number, y0: number, z0: number, x1: number, y1: number, z1: number): void {
    const cx = (x0 + x1) / 2
    const cy = (y0 + y1) / 2
    const cz = (z0 + z1) / 2
    const half = Math.sqrt((x1 - x0) ** 2 + (y1 - y0) ** 2 + (z1 - z0) ** 2) / 2
    this.active = []
    this.skipped = []
    for (const b of this.bodies) {
      const rho = Math.hypot(cx - b.x, cy - b.y, cz - b.z)
      // A body more than a box away from its surface only matters as a
      // far positive distance
      if (rho - half > b.reach + 2) this.skipped.push(b)
      else {
        this.active.push(b)
        b.gen.beginRegion(x0 - b.x, y0 - b.y, z0 - b.z, x1 - b.x, y1 - b.y, z1 - b.z)
      }
    }
  }

  endRegion(): void {
    for (const b of this.active) b.gen.endRegion()
    this.active = this.bodies
    this.skipped = []
  }

  // The body nearest its surface at a point, and that distance
  nearest(x: number, y: number, z: number): { body: Body; d: number } {
    let best = this.active[0] ?? this.bodies[0]
    let bd = Infinity
    for (const b of this.active) {
      const d = b.gen.density(x - b.x, y - b.y, z - b.z)
      if (d < bd) {
        bd = d
        best = b
      }
    }
    for (const b of this.skipped) {
      const d = Math.hypot(x - b.x, y - b.y, z - b.z) - b.reach
      if (d < bd) {
        bd = d
        best = b
      }
    }
    return { body: best, d: bd }
  }

  density(x: number, y: number, z: number): number {
    if (this.active.length === 1 && this.skipped.length === 0) {
      const b = this.active[0]
      return b.gen.density(x - b.x, y - b.y, z - b.z)
    }
    return this.nearest(x, y, z).d
  }

  material(x: number, y: number, z: number, d: number): number {
    const { body: b } = this.nearest(x, y, z)
    return b.gen.material(x - b.x, y - b.y, z - b.z, d)
  }

  // Gravity at a point: each body pulls toward its centre with its surface
  // pull up to its highest ground, falling off steeply beyond it (and
  // linearly inside it, which only a deep hole shows)
  gravity(x: number, y: number, z: number, out: { x: number; y: number; z: number }): { x: number; y: number; z: number } {
    out.x = out.y = out.z = 0
    for (const b of this.bodies) {
      const dx = b.x - x
      const dy = b.y - y
      const dz = b.z - z
      const rho = Math.hypot(dx, dy, dz) || 1e-6
      const a = rho > b.reach ? b.surfaceG * (b.reach / rho) ** FALLOFF : rho >= b.radius ? b.surfaceG : (b.surfaceG * rho) / b.radius
      out.x += (dx / rho) * a
      out.y += (dy / rho) * a
      out.z += (dz / rho) * a
    }
    return out
  }
}
