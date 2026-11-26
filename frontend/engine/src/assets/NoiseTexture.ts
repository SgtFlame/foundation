import * as THREE from 'three/webgpu'
import { mulberry32 } from '../voxel/Noise.ts'

// Placeholder art as code: a tileable grey-scale noise texture tinted with
// a colour, for the voxel materials until real textures exist.
export interface NoiseTextureOptions {
  seed: number
  size?: number
  // The base colour and how far the noise darkens and lightens it
  color: [number, number, number]
  contrast?: number
  // The coarsest lattice period in texels; octaves halve it
  period?: number
  octaves?: number
  // Speckles: fraction of texels lightened or darkened sharply
  speckle?: number
}

// Periodic value noise, so the texture tiles
function valueNoise(size: number, period: number, rand: () => number): Float32Array {
  const cells = Math.max(1, Math.round(size / period))
  const lattice = new Float32Array(cells * cells)
  for (let i = 0; i < lattice.length; i++) lattice[i] = rand()
  const out = new Float32Array(size * size)
  const smooth = (t: number) => t * t * (3 - 2 * t)
  for (let y = 0; y < size; y++) {
    const fy = (y / size) * cells
    const y0 = Math.floor(fy)
    const ty = smooth(fy - y0)
    const ya = y0 % cells
    const yb = (y0 + 1) % cells
    for (let x = 0; x < size; x++) {
      const fx = (x / size) * cells
      const x0 = Math.floor(fx)
      const tx = smooth(fx - x0)
      const xa = x0 % cells
      const xb = (x0 + 1) % cells
      const a = lattice[ya * cells + xa]
      const b = lattice[ya * cells + xb]
      const c = lattice[yb * cells + xa]
      const d = lattice[yb * cells + xb]
      out[y * size + x] = a + (b - a) * tx + (c - a) * ty + (a - b - c + d) * tx * ty
    }
  }
  return out
}

export function noiseTexture(o: NoiseTextureOptions): THREE.DataTexture {
  const size = o.size ?? 256
  const rand = mulberry32(o.seed)
  const sum = new Float32Array(size * size)
  let amp = 1
  let norm = 0
  let period = o.period ?? 64
  for (let oct = 0; oct < (o.octaves ?? 5) && period >= 1; oct++) {
    const n = valueNoise(size, period, rand)
    for (let i = 0; i < sum.length; i++) sum[i] += n[i] * amp
    norm += amp
    amp *= 0.55
    period /= 2
  }
  const contrast = o.contrast ?? 0.35
  const speckle = o.speckle ?? 0
  const data = new Uint8Array(size * size * 4)
  for (let i = 0; i < sum.length; i++) {
    let v = 1 + (sum[i] / norm - 0.5) * 2 * contrast
    if (speckle > 0) {
      const r = rand()
      if (r < speckle / 2) v *= 0.6
      else if (r < speckle) v *= 1.3
    }
    data[i * 4] = Math.min(255, o.color[0] * v * 255)
    data[i * 4 + 1] = Math.min(255, o.color[1] * v * 255)
    data[i * 4 + 2] = Math.min(255, o.color[2] * v * 255)
    data[i * 4 + 3] = 255
  }
  const tex = new THREE.DataTexture(data, size, size, THREE.RGBAFormat)
  tex.wrapS = THREE.RepeatWrapping
  tex.wrapT = THREE.RepeatWrapping
  tex.colorSpace = THREE.SRGBColorSpace
  tex.magFilter = THREE.LinearFilter
  tex.minFilter = THREE.LinearMipmapLinearFilter
  tex.generateMipmaps = true
  tex.needsUpdate = true
  return tex
}
