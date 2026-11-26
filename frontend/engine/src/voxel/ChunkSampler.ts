import type { MoonGenerator } from './MoonGenerator.ts'
import { CHUNK_SIZE, type VoxelEdit } from './Types.ts'

// Samples one chunk: (CHUNK_SIZE + 2)³ points, from local index -1 to
// CHUNK_SIZE, so the chunk can mesh its own cells and the seam cells it
// shares with its neighbours without reading theirs. Neighbouring chunks
// compute the shared samples from the same function, so seams match.
export const SAMPLES = CHUNK_SIZE + 2

export interface ChunkSamples {
  density: Float32Array
  material: Uint8Array
  // True when every sample has the same sign (no surface)
  uniform: boolean
}

// Edit brushes blend into the ground over this many metres, so a dug
// hole has a soft lip instead of a crease
const BLEND = 0.35

function smoothMax(a: number, b: number, k: number): number {
  const h = Math.max(k - Math.abs(a - b), 0) / k
  return Math.max(a, b) + h * h * k * 0.25
}

function smoothMin(a: number, b: number, k: number): number {
  const h = Math.max(k - Math.abs(a - b), 0) / k
  return Math.min(a, b) - h * h * k * 0.25
}

// Does an edit's sphere (plus its blend) touch the chunk's sampled box?
export function editTouchesChunk(e: VoxelEdit, cx: number, cy: number, cz: number, voxelSize: number): boolean {
  const size = CHUNK_SIZE * voxelSize
  const pad = voxelSize + e.radius + BLEND
  const x0 = cx * size - pad
  const y0 = cy * size - pad
  const z0 = cz * size - pad
  return (
    e.x >= x0 && e.x <= x0 + size + 2 * pad && e.y >= y0 && e.y <= y0 + size + 2 * pad && e.z >= z0 && e.z <= z0 + size + 2 * pad
  )
}

export function sampleChunk(gen: MoonGenerator, cx: number, cy: number, cz: number, edits: readonly VoxelEdit[]): ChunkSamples {
  const vs = gen.params.voxelSize
  const n = SAMPLES
  const density = new Float32Array(n * n * n)
  const material = new Uint8Array(n * n * n)
  const bx = cx * CHUNK_SIZE - 1
  const by = cy * CHUNK_SIZE - 1
  const bz = cz * CHUNK_SIZE - 1
  let neg = 0
  let idx = 0
  gen.beginRegion(bx * vs, by * vs, bz * vs, (bx + n) * vs, (by + n) * vs, (bz + n) * vs)
  for (let k = 0; k < n; k++) {
    const z = (bz + k) * vs
    for (let j = 0; j < n; j++) {
      const y = (by + j) * vs
      for (let i = 0; i < n; i++, idx++) {
        const x = (bx + i) * vs
        let d = gen.density(x, y, z)
        let m = d < 0 ? gen.material(x, y, z, d) : 0
        for (const e of edits) {
          const dx = x - e.x
          const dy = y - e.y
          const dz = z - e.z
          const dist = Math.sqrt(dx * dx + dy * dy + dz * dz)
          if (dist > e.radius + BLEND * 2) continue
          const sphere = dist - e.radius
          if (e.kind === 'dig') {
            d = smoothMax(d, -sphere, BLEND)
          } else {
            const nd = smoothMin(d, sphere, BLEND)
            if (sphere < 0 && d >= 0) m = e.material ?? 0
            d = nd
          }
        }
        density[idx] = d
        material[idx] = m
        if (d < 0) neg++
      }
    }
  }
  gen.endRegion()
  return { density, material, uniform: neg === 0 || neg === n * n * n }
}
