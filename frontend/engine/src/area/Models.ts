import * as THREE from 'three/webgpu'
import { mulberry32 } from '../voxel/Noise.ts'

// Placeholder piece models made in code (forge 375 S1, models-as-code: plan
// 300 › 11): low poly, flat shaded, one colour per part, in the art style's
// palette (375 › Proposed art style). Each model fills its unturned
// footprint, w × d cells of `cell` metres, centred on the origin with x east,
// z south and y up; the viewport turns and places it.

const palette = {
  grass: 0x5f7d3c,
  grassDark: 0x4e6a31,
  dirt: 0x8a6a45,
  earth: 0x9a8160,
  stone: 0x9a958c,
  stoneDark: 0x77726a,
  thatch: 0x7a5a32,
  roofSlate: 0x4b4f58,
  timber: 0x6e3b26,
  wood: 0x7b5a3a,
  bark: 0x5a4030,
  leaf: 0x3f6b35,
  leafDark: 0x2f5530,
  barley: 0xc7a54a,
  furrow: 0x6d5236,
  window: 0xffb347,
  water: 0x3b5d7a,
}

const materials = new Map<string, THREE.MeshStandardNodeMaterial>()

// One shared material per colour (and glow), so pieces batch well
export function material(color: number, emissive = 0): THREE.MeshStandardNodeMaterial {
  const k = `${color}:${emissive}`
  let m = materials.get(k)
  if (!m) {
    m = new THREE.MeshStandardNodeMaterial({ color, roughness: 0.9, metalness: 0, flatShading: true })
    if (emissive) {
      m.emissive = new THREE.Color(color)
      m.emissiveIntensity = emissive
    }
    materials.set(k, m)
  }
  return m
}

function box(w: number, h: number, d: number, color: number, x: number, y: number, z: number, emissive = 0): THREE.Mesh {
  const m = new THREE.Mesh(new THREE.BoxGeometry(w, h, d), material(color, emissive))
  m.position.set(x, y + h / 2, z)
  m.castShadow = h > 0.2
  m.receiveShadow = true
  return m
}

// A gable roof: a triangular prism along x, its ridge h above its base
function gable(w: number, h: number, d: number, color: number, x: number, y: number, z: number): THREE.Mesh {
  const hw = w / 2
  const hd = d / 2
  // prettier-ignore
  const v = new Float32Array([
    -hw, 0, -hd,  hw, 0, -hd,  hw, h, 0,   -hw, 0, -hd,  hw, h, 0,  -hw, h, 0, // north slope
    -hw, 0, hd,  -hw, h, 0,   hw, h, 0,    -hw, 0, hd,   hw, h, 0,   hw, 0, hd, // south slope
    -hw, 0, -hd, -hw, h, 0,  -hw, 0, hd, // west gable
    hw, 0, -hd,   hw, 0, hd,  hw, h, 0, // east gable
  ])
  const g = new THREE.BufferGeometry()
  g.setAttribute('position', new THREE.BufferAttribute(v, 3))
  g.computeVertexNormals()
  const m = new THREE.Mesh(g, material(color))
  m.position.set(x, y, z)
  m.castShadow = true
  m.receiveShadow = true
  return m
}

function ground(W: number, D: number, color = palette.grass): THREE.Mesh {
  const m = box(W, 0.3, D, color, 0, -0.3, 0)
  m.castShadow = false
  return m
}

// A road strip from (x1, z1) to (x2, z2), axis-aligned
function strip(x1: number, z1: number, x2: number, z2: number, width: number, color = palette.dirt): THREE.Mesh {
  const w = Math.abs(x2 - x1) + width
  const d = Math.abs(z2 - z1) + width
  const m = box(w, 0.06, d, color, (x1 + x2) / 2, 0, (z1 + z2) / 2)
  m.castShadow = false
  return m
}

function tree(g: THREE.Group, x: number, z: number, scale: number, rand: () => number): void {
  const trunk = new THREE.Mesh(new THREE.CylinderGeometry(0.25 * scale, 0.35 * scale, 2.4 * scale, 6), material(palette.bark))
  trunk.position.set(x, 1.2 * scale, z)
  trunk.castShadow = true
  g.add(trunk)
  const leaf = rand() < 0.5 ? palette.leaf : palette.leafDark
  for (let i = 0; i < 2; i++) {
    const crown = new THREE.Mesh(new THREE.IcosahedronGeometry((2.1 - i * 0.6) * scale, 0), material(leaf))
    crown.position.set(x + (rand() - 0.5) * 0.4, (3.2 + i * 1.5) * scale, z + (rand() - 0.5) * 0.4)
    crown.rotation.y = rand() * Math.PI
    crown.castShadow = true
    crown.receiveShadow = true
    g.add(crown)
  }
}

function tufts(g: THREE.Group, W: number, D: number, n: number, rand: () => number): void {
  for (let i = 0; i < n; i++) {
    const t = new THREE.Mesh(new THREE.ConeGeometry(0.25, 0.5, 4), material(palette.grassDark))
    t.position.set((rand() - 0.5) * (W - 1), 0.25, (rand() - 0.5) * (D - 1))
    g.add(t)
  }
}

// Windows and doors are lit from inside: emissive, and the viewport adds a
// point light where a model names one (userData.lights)
function lights(g: THREE.Group, points: [number, number, number][]): void {
  g.userData.lights = points
}

export function buildModel(model: string, w: number, d: number, cell: number, seed = 1): THREE.Group {
  const W = w * cell
  const D = d * cell
  const g = new THREE.Group()
  const rand = mulberry32(seed)
  const road = cell * 0.35
  switch (model) {
    case 'grass':
      g.add(ground(W, D))
      tufts(g, W, D, 6, rand)
      break
    case 'road':
      g.add(ground(W, D), strip(0, -D / 2 + road / 2, 0, D / 2 - road / 2, road))
      break
    case 'roadCorner':
      g.add(ground(W, D), strip(0, -D / 2 + road / 2, 0, 0, road), strip(0, 0, W / 2 - road / 2, 0, road))
      break
    case 'roadT':
      g.add(ground(W, D), strip(0, -D / 2 + road / 2, 0, 0, road), strip(-W / 2 + road / 2, 0, W / 2 - road / 2, 0, road))
      break
    case 'roadEnd': {
      g.add(ground(W, D), strip(0, -D / 2 + road / 2, 0, 0, road))
      const end = new THREE.Mesh(new THREE.CylinderGeometry(road / 2, road / 2, 0.06, 10), material(palette.dirt))
      end.position.set(0, 0.03, 0)
      g.add(end)
      break
    }
    case 'fence': {
      g.add(ground(W, D))
      tufts(g, W, D, 4, rand)
      const z = -D / 2 + 0.4
      for (let x = -W / 2 + 0.3; x <= W / 2 - 0.2; x += W / 4) g.add(box(0.25, 1.3, 0.25, palette.wood, x, 0, z))
      g.add(box(W - 0.4, 0.12, 0.1, palette.wood, 0, 0.55, z), box(W - 0.4, 0.12, 0.1, palette.wood, 0, 1.0, z))
      break
    }
    case 'tree':
      g.add(ground(W, D))
      tree(g, (rand() - 0.5) * 2, (rand() - 0.5) * 2, 1.2, rand)
      break
    case 'copse':
      g.add(ground(W, D, palette.grassDark))
      for (let i = 0; i < 7; i++) tree(g, (rand() - 0.5) * (W - 4), (rand() - 0.5) * (D - 4), 0.9 + rand() * 0.6, rand)
      break
    case 'field': {
      g.add(ground(W, D, palette.furrow))
      const rows = 7
      for (let i = 0; i < rows; i++) {
        const x = -W / 2 + 1 + (i * (W - 2)) / (rows - 1)
        g.add(box(0.7, 0.55, D - 1.6, palette.barley, x, 0, 0))
      }
      g.add(box(W, 0.15, 0.3, palette.grassDark, 0, 0, -D / 2 + 0.15), box(W, 0.15, 0.3, palette.grassDark, 0, 0, D / 2 - 0.15))
      break
    }
    case 'farmhouse': {
      g.add(ground(W, D))
      tufts(g, W, D, 5, rand)
      // Its path leaves the south-east cell's south edge
      g.add(strip(W / 4, 2, W / 4, D / 2 - road / 2, road * 0.7))
      g.add(box(12, 4.5, 8, palette.stone, -1, 0, -2))
      g.add(box(12.2, 0.4, 8.2, palette.stoneDark, -1, 0, -2))
      g.add(gable(13, 3.5, 9.4, palette.thatch, -1, 4.5, -2))
      g.add(box(1.2, 3.5, 1.2, palette.stoneDark, -5.2, 5, -3.2)) // the hearth's chimney
      g.add(box(1.4, 2.2, 0.2, palette.timber, W / 4, 0, 2.05)) // the door, facing the path
      for (const x of [-5, -1.5, 2]) g.add(box(1.2, 1, 0.15, palette.window, x, 2, 2.05, 1.6)) // windows
      lights(g, [
        [-1.5, 2.2, 3.2],
        [-5.2, 9, -3.2],
      ])
      break
    }
    case 'barn': {
      g.add(ground(W, D))
      tufts(g, W, D, 6, rand)
      // Its doors open onto the south-west cell's south edge
      g.add(strip(-W / 4, 6, -W / 4, D / 2 - road / 2, road))
      g.add(box(14, 6, 20, palette.timber, -1, 0, -3))
      g.add(gable(21.5, 4.5, 15, palette.roofSlate, -1, 6, -3).rotateY(Math.PI / 2))
      g.add(box(5, 4.2, 0.2, palette.wood, -W / 4, 0, 7.05))
      g.add(box(1, 0.8, 0.15, palette.window, 3, 3.5, 7.05, 0.8))
      lights(g, [[3, 3.5, 8]])
      break
    }
    case 'yard': {
      g.add(ground(W, D, palette.earth))
      g.add(strip(0, -D / 2 + road / 2, 0, D / 2 - road / 2, road), strip(0, 0, W / 2 - road / 2, 0, road))
      const well = new THREE.Mesh(new THREE.CylinderGeometry(1.2, 1.3, 1, 10), material(palette.stone))
      well.position.set(-7, 0.5, -6)
      well.castShadow = true
      g.add(well)
      const water = new THREE.Mesh(new THREE.CylinderGeometry(0.95, 0.95, 0.05, 10), material(palette.water))
      water.position.set(-7, 0.95, -6)
      g.add(water)
      g.add(box(0.2, 2.6, 0.2, palette.wood, -8.1, 0, -6), box(0.2, 2.6, 0.2, palette.wood, -5.9, 0, -6), gable(2.8, 0.9, 1.8, palette.thatch, -7, 2.6, -6))
      for (let i = 0; i < 3; i++) g.add(box(1, 1, 1, palette.wood, -10 + i * 1.3, 0, 8)) // crates
      g.add(box(3, 0.9, 1.2, palette.wood, 9, 0, -9)) // a cart
      lights(g, [[-7, 3.2, -6]])
      break
    }
    default:
      g.add(ground(W, D, 0xaa44aa))
  }
  return g
}
