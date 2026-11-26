import { Simplex3, mulberry32 } from './Noise.ts'
import { MATERIAL_DUST, MATERIAL_ROCK, type MoonParams } from './Types.ts'

interface Crater {
  // Unit direction from the moon's centre
  x: number
  y: number
  z: number
  // Radius in metres along the surface, and the bowl's depth
  r: number
  depth: number
  // cos of the angle beyond which the crater has no effect
  cosLimit: number
}

// A small moon: a sphere roughened by fractal noise and pocked by craters.
// density(x, y, z) is a signed distance in metres (negative inside), close
// enough to a true distance near the surface for surface nets and brushes.
export class MoonGenerator {
  readonly params: MoonParams
  private readonly noise: Simplex3
  private readonly detail: Simplex3
  private readonly craters: Crater[] = []
  // The craters that may touch the current region (all of them outside one)
  private active: Crater[] = this.craters
  // Bounds on how far the surface strays from the mean radius
  readonly maxOutward: number
  readonly maxInward: number

  constructor(params: MoonParams) {
    this.params = params
    this.noise = new Simplex3(params.seed)
    this.detail = new Simplex3(params.seed ^ 0x5bd1e995)
    const rand = mulberry32(params.seed * 7919 + 13)
    let deepest = 0
    for (let i = 0; i < params.craters; i++) {
      // A uniform direction on the sphere
      const u = rand() * 2 - 1
      const a = rand() * Math.PI * 2
      const s = Math.sqrt(1 - u * u)
      // Many small craters, few large ones
      const r = params.craterMaxRadius * (0.2 + 0.8 * Math.pow(rand(), 2.5))
      const depth = r * (0.18 + 0.1 * rand())
      deepest = Math.max(deepest, depth)
      this.craters.push({ x: s * Math.cos(a), y: u, z: s * Math.sin(a), r, depth, cosLimit: Math.cos(Math.min(Math.PI, (1.6 * r) / params.radius)) })
    }
    // A rim rises at most a quarter of its crater's depth; allow for a few
    // overlapping craters
    this.maxOutward = params.amplitude + deepest * 0.75
    this.maxInward = params.amplitude + deepest * 2
  }

  // Height of the surface above the mean radius along a unit direction
  private craterHeight(dx: number, dy: number, dz: number): number {
    let h = 0
    const R = this.params.radius
    for (const c of this.active) {
      const dot = dx * c.x + dy * c.y + dz * c.z
      if (dot <= c.cosLimit) continue
      const s = Math.acos(Math.min(1, dot)) * R
      const q = s / c.r
      if (q >= 1.6) continue
      if (q < 1) {
        // The bowl, rising to a quarter of the depth at the rim
        h += c.depth * (1.25 * q * q - 1)
      } else {
        // The rim falling away outside
        const t = (q - 1) / 0.6
        h += c.depth * 0.25 * (1 - t) * (1 - t)
      }
    }
    return h
  }

  // The hills are fractal noise sampled on a lattice twice as coarse as the
  // voxels and interpolated, which costs an eighth of sampling every voxel
  // and loses nothing visible (the finest octave spans several metres)
  private latticeStep(): number {
    return this.params.voxelSize * 2
  }

  private latticeValue(gx: number, gy: number, gz: number): number {
    const L = this.latticeStep()
    const f = this.params.frequency
    return this.noise.fbm(gx * L * f, gy * L * f, gz * L * f, this.params.octaves) * this.params.amplitude
  }

  // A cache of lattice values over a box, set by beginRegion
  private region: Float64Array | null = null
  private rx = 0
  private ry = 0
  private rz = 0
  private rn = 0

  // Precompute the lattice over a box of sample positions (metres), so a
  // run of density() calls inside it skips the noise
  beginRegion(x0: number, y0: number, z0: number, x1: number, y1: number, z1: number): void {
    const L = this.latticeStep()
    this.rx = Math.floor(x0 / L)
    this.ry = Math.floor(y0 / L)
    this.rz = Math.floor(z0 / L)
    const n = Math.max(Math.floor(x1 / L) - this.rx, Math.floor(y1 / L) - this.ry, Math.floor(z1 / L) - this.rz) + 2
    const vals = new Float64Array(n * n * n)
    let i = 0
    for (let k = 0; k < n; k++) for (let j = 0; j < n; j++) for (let h = 0; h < n; h++) vals[i++] = this.latticeValue(this.rx + h, this.ry + j, this.rz + k)
    this.rn = n
    this.region = vals
    // Keep only the craters whose reach overlaps the box's cone of directions
    const cx = (x0 + x1) / 2
    const cy = (y0 + y1) / 2
    const cz = (z0 + z1) / 2
    const dist = Math.sqrt(cx * cx + cy * cy + cz * cz)
    const half = Math.sqrt((x1 - x0) ** 2 + (y1 - y0) ** 2 + (z1 - z0) ** 2) / 2
    if (dist <= half * 1.5) {
      this.active = this.craters
      return
    }
    const boxAngle = Math.asin(Math.min(1, half / dist))
    this.active = this.craters.filter((c) => {
      const dot = (cx * c.x + cy * c.y + cz * c.z) / dist
      const angle = Math.acos(Math.max(-1, Math.min(1, dot)))
      return angle <= Math.acos(c.cosLimit) + boxAngle + 1e-6
    })
  }

  endRegion(): void {
    this.region = null
    this.active = this.craters
  }

  private lat(gx: number, gy: number, gz: number): number {
    const r = this.region
    if (r) {
      const h = gx - this.rx
      const j = gy - this.ry
      const k = gz - this.rz
      const n = this.rn
      if (h >= 0 && j >= 0 && k >= 0 && h < n && j < n && k < n) return r[h + n * (j + n * k)]
    }
    return this.latticeValue(gx, gy, gz)
  }

  hills(x: number, y: number, z: number): number {
    const L = this.latticeStep()
    const fx = x / L
    const fy = y / L
    const fz = z / L
    const gx = Math.floor(fx)
    const gy = Math.floor(fy)
    const gz = Math.floor(fz)
    const tx = fx - gx
    const ty = fy - gy
    const tz = fz - gz
    const c000 = this.lat(gx, gy, gz)
    const c100 = this.lat(gx + 1, gy, gz)
    const c010 = this.lat(gx, gy + 1, gz)
    const c110 = this.lat(gx + 1, gy + 1, gz)
    const c001 = this.lat(gx, gy, gz + 1)
    const c101 = this.lat(gx + 1, gy, gz + 1)
    const c011 = this.lat(gx, gy + 1, gz + 1)
    const c111 = this.lat(gx + 1, gy + 1, gz + 1)
    const a = c000 + (c100 - c000) * tx
    const b = c010 + (c110 - c010) * tx
    const c = c001 + (c101 - c001) * tx
    const d = c011 + (c111 - c011) * tx
    const e = a + (b - a) * ty
    const g = c + (d - c) * ty
    return e + (g - e) * tz
  }

  density(x: number, y: number, z: number): number {
    const p = this.params
    const len = Math.sqrt(x * x + y * y + z * z) || 1e-9
    return len - (p.radius + this.hills(x, y, z) + this.craterHeight(x / len, y / len, z / len))
  }

  // The material at a point whose density is d (negative inside)
  material(x: number, y: number, z: number, d: number): number {
    if (-d > this.params.dustDepth) return MATERIAL_ROCK
    // Rocky outcrops poke through the dust here and there
    const n = this.detail.noise(x * 0.08, y * 0.08, z * 0.08)
    return n > 0.45 ? MATERIAL_ROCK : MATERIAL_DUST
  }
}
