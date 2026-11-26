import * as THREE from 'three/webgpu'
import { attribute, mix, normalWorld, positionWorld, texture, vec3 } from 'three/tsl'
import { noiseTexture } from '../assets/NoiseTexture.ts'

// The terrain's triplanar material: two placeholder materials (dust and
// rock) blended by each vertex's rock fraction, projected along the world
// axes so no UVs are needed and chunk seams line up.
export function terrainMaterial(): THREE.MeshStandardNodeMaterial {
  const dust = noiseTexture({ seed: 11, color: [0.62, 0.6, 0.56], contrast: 0.22, period: 64, speckle: 0.04 })
  const rock = noiseTexture({ seed: 23, color: [0.38, 0.37, 0.36], contrast: 0.5, period: 32, octaves: 6 })
  const mat = new THREE.MeshStandardNodeMaterial()
  // One texture repeat per 4 metres
  const p = positionWorld.mul(0.25)
  const n = normalWorld.abs().pow(vec3(4, 4, 4))
  const w = n.div(n.x.add(n.y).add(n.z))
  const tri = (t: THREE.Texture) =>
    texture(t, p.yz).mul(w.x).add(texture(t, p.zx).mul(w.y)).add(texture(t, p.xy).mul(w.z))
  mat.colorNode = mix(tri(dust), tri(rock), attribute('rock', 'float'))
  mat.roughness = 0.95
  mat.metalness = 0
  return mat
}
