import * as THREE from 'three/webgpu'
import { attribute, clamp, mix, normalWorld, positionWorld, texture, vec3 } from 'three/tsl'
import { noiseTexture } from '../assets/NoiseTexture.ts'

// The terrain's triplanar material: two placeholder materials (dust and
// rock) blended by each vertex's rock fraction, projected along the world
// axes so no UVs are needed and chunk seams line up.
export function terrainMaterial(): THREE.MeshStandardNodeMaterial {
  const dust = noiseTexture({ seed: 11, color: [0.62, 0.6, 0.56], contrast: 0.22, period: 64, speckle: 0.04 })
  const rock = noiseTexture({ seed: 23, color: [0.38, 0.37, 0.36], contrast: 0.5, period: 32, octaves: 6 })
  // V8: ore, a blue-grey with bright flecks
  const ore = noiseTexture({ seed: 37, color: [0.3, 0.36, 0.48], contrast: 0.55, period: 16, octaves: 5, speckle: 0.12 })
  const mat = new THREE.MeshStandardNodeMaterial()
  // One texture repeat per 4 metres
  const p = positionWorld.mul(0.25)
  const n = normalWorld.abs().pow(vec3(4, 4, 4))
  const w = n.div(n.x.add(n.y).add(n.z))
  const tri = (t: THREE.Texture) =>
    texture(t, p.yz).mul(w.x).add(texture(t, p.zx).mul(w.y)).add(texture(t, p.xy).mul(w.z))
  // the attribute runs 0 (dust) to 1 (rock) to 2 (ore)
  const a = attribute('rock', 'float')
  mat.colorNode = mix(mix(tri(dust), tri(rock), clamp(a, 0, 1)), tri(ore), clamp(a.sub(1), 0, 1))
  mat.roughness = 0.95
  mat.metalness = 0
  return mat
}
