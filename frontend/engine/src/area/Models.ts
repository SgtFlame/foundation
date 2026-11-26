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
  plank: 0x8c6a48,
  plankDark: 0x6f523a,
  plaster: 0xcdbfa6,
  fire: 0xff7a1a,
  cloth: 0x7d3b32,
  wool: 0xb9ad94,
  crock: 0xa86a4a,
  dusk: 0x6d86b0,
  milk: 0xf2efe6,
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
  const r = 0.6 * scale
  blockers(g, [[x - r, z - r, x + r, z + r]])
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

// What a character cannot walk through, as rectangles [minX, minZ, maxX,
// maxZ] in the model's own (unturned) metres (userData.blockers)
function blockers(g: THREE.Group, rects: [number, number, number, number][]): void {
  g.userData.blockers = [...((g.userData.blockers as [number, number, number, number][] | undefined) ?? []), ...rects]
}

// Windows and doors are lit from inside: emissive, and the viewport adds a
// point light where a model names one (userData.lights)
function lights(g: THREE.Group, points: [number, number, number][]): void {
  g.userData.lights = points
}

// Where a character stands to go through a model's door (S3), in the
// model's own metres (userData.door): the viewport walks there, then
// travels to the area the door leads to; arriving from that area, the
// character starts there
function door(g: THREE.Group, x: number, z: number): void {
  g.userData.door = [x, z]
}

// An interior's floor and the walls round its edge (S3), W × D metres with
// the area's corner at the origin: walls stand just outside the cells, so
// every cell is floor
export function interiorShell(W: number, D: number): THREE.Group {
  const g = new THREE.Group()
  const floor = box(W, 0.3, D, palette.plank, W / 2, -0.3, D / 2)
  floor.castShadow = false
  g.add(floor)
  // board seams along x
  for (let z = 0.5; z < D; z += 0.5) {
    const seam = box(W, 0.01, 0.03, palette.plankDark, W / 2, 0, z)
    seam.castShadow = false
    g.add(seam)
  }
  const t = 0.3
  const h = 2.8
  g.add(
    box(W + 2 * t, h, t, palette.plaster, W / 2, 0, -t / 2),
    box(W + 2 * t, h, t, palette.plaster, W / 2, 0, D + t / 2),
    box(t, h, D, palette.plaster, -t / 2, 0, D / 2),
    box(t, h, D, palette.plaster, W + t / 2, 0, D / 2),
  )
  // a dark beam along the top of each wall
  g.add(box(W + 2 * t, 0.2, t + 0.04, palette.timber, W / 2, h, -t / 2), box(W + 2 * t, 0.2, t + 0.04, palette.timber, W / 2, h, D + t / 2))
  g.add(box(t + 0.04, 0.2, D, palette.timber, -t / 2, h, D / 2), box(t + 0.04, 0.2, D, palette.timber, W + t / 2, h, D / 2))
  return g
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
      blockers(g, [[-W / 2, z - 0.2, W / 2, z + 0.2]])
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
      blockers(g, [[-7, -6, 5, 2]])
      g.add(box(12.2, 0.4, 8.2, palette.stoneDark, -1, 0, -2))
      g.add(gable(13, 3.5, 9.4, palette.thatch, -1, 4.5, -2))
      g.add(box(1.2, 3.5, 1.2, palette.stoneDark, -5.2, 5, -3.2)) // the hearth's chimney
      g.add(box(1.4, 2.2, 0.2, palette.timber, W / 4, 0, 2.05)) // the door, facing the path
      door(g, W / 4, 3.4)
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
      blockers(g, [[-8, -13, 6, 7]])
      g.add(gable(21.5, 4.5, 15, palette.roofSlate, -1, 6, -3).rotateY(Math.PI / 2))
      g.add(box(5, 4.2, 0.2, palette.wood, -W / 4, 0, 7.05))
      door(g, -W / 4, 8.6)
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
      blockers(g, [
        [-8.4, -7.4, -5.6, -4.6], // the well
        [-10.5, 7.5, -6.7, 8.5], // crates
        [7.5, -9.6, 10.5, -8.4], // the cart
      ])
      lights(g, [[-7, 3.2, -6]])
      break
    }
    // The farmhouse's rooms (S3): cells of 2 m, the wall at the model's
    // north (-z) for the pieces that stand against one
    case 'hearth': {
      const back = -D / 2
      g.add(box(W - 0.2, 1.4, 1.1, palette.stone, 0, 0, back + 0.55))
      g.add(box(W - 1.4, 0.8, 0.7, 0x1a1410, 0, 0.15, back + 0.5)) // the firebox
      g.add(box(W - 1.8, 0.35, 0.45, palette.fire, 0, 0.15, back + 0.55, 2.5)) // the fire
      g.add(box(W - 0.6, 1.5, 0.8, palette.stoneDark, 0, 1.4, back + 0.4)) // the chimney breast
      g.add(box(W, 0.12, 1.2, palette.stoneDark, 0, 0, back + 1.6)) // the hearthstone
      g.add(box(0.5, 0.35, 0.5, 0x2a2a2a, 0.9, 0, back + 1.6)) // a pot
      blockers(g, [[-W / 2, back, W / 2, back + 1.2]])
      lights(g, [[0, 0.8, back + 1.3]])
      break
    }
    case 'table': {
      g.add(box(W - 0.6, 0.1, D - 0.9, palette.wood, 0, 0.75, 0))
      for (const x of [-W / 2 + 0.5, W / 2 - 0.5]) for (const z of [-D / 2 + 0.6, D / 2 - 0.6]) g.add(box(0.12, 0.75, 0.12, palette.plankDark, x, 0, z))
      g.add(box(0.6, 0.12, 0.6, palette.barley, -0.6, 0.85, 0)) // hot cakes on a board
      g.add(box(0.25, 0.3, 0.25, palette.crock, 0.8, 0.85, 0.1))
      blockers(g, [[-W / 2 + 0.3, -D / 2 + 0.45, W / 2 - 0.3, D / 2 - 0.45]])
      break
    }
    case 'bench': {
      g.add(box(W - 0.4, 0.08, 0.4, palette.wood, 0, 0.42, 0))
      for (const x of [-W / 2 + 0.4, W / 2 - 0.4]) g.add(box(0.08, 0.42, 0.34, palette.plankDark, x, 0, 0))
      blockers(g, [[-W / 2 + 0.2, -0.2, W / 2 - 0.2, 0.2]])
      break
    }
    case 'bed': {
      const x0 = -W / 2 + 0.1
      g.add(box(W - 0.4, 0.6, D - 0.4, palette.timber, 0, 0, 0)) // the box
      g.add(box(W - 0.6, 0.15, D - 0.6, palette.wool, 0, 0.6, 0)) // the blanket
      g.add(box(W - 0.8, 0.15, 0.5, palette.milk, 0, 0.7, -D / 2 + 0.6)) // the pillow
      g.add(box(0.12, 2.2, D - 0.4, palette.timber, x0, 0, 0), box(W - 0.4, 0.15, D - 0.4, palette.timber, 0, 2.1, 0)) // its frame and canopy
      g.add(box(0.05, 1.5, D * 0.45, palette.cloth, W / 2 - 0.25, 0.6, -D / 4)) // the drawn curtain
      blockers(g, [[-W / 2 + 0.2, -D / 2 + 0.2, W / 2 - 0.2, D / 2 - 0.2]])
      break
    }
    case 'shelf': {
      const back = -D / 2
      g.add(box(W - 0.4, 2, 0.4, palette.plankDark, 0, 0, back + 0.2))
      for (const y of [0.6, 1.2, 1.8]) {
        g.add(box(W - 0.4, 0.05, 0.42, palette.wood, 0, y, back + 0.22))
        for (let i = 0; i < 3; i++) g.add(box(0.22, 0.28, 0.22, i === 1 && y === 1.2 ? palette.milk : palette.crock, -0.5 + i * 0.5, y + 0.05, back + 0.25))
      }
      blockers(g, [[-W / 2 + 0.2, back, W / 2 - 0.2, back + 0.5]])
      break
    }
    case 'barrel': {
      for (const [x, z] of [
        [-0.4, -0.2],
        [0.45, 0.3],
      ]) {
        const b = new THREE.Mesh(new THREE.CylinderGeometry(0.38, 0.32, 0.9, 10), material(palette.wood))
        b.position.set(x, 0.45, z)
        b.castShadow = true
        g.add(b)
        g.add(box(0.8, 0.05, 0.05, palette.stoneDark, x, 0.75, z), box(0.8, 0.05, 0.05, palette.stoneDark, x, 0.15, z))
      }
      blockers(g, [[-0.8, -0.6, 0.85, 0.7]])
      break
    }
    case 'door': {
      const back = -D / 2
      g.add(box(1.3, 2.2, 0.12, palette.timber, 0, 0, back + 0.08))
      g.add(box(1.5, 0.15, 0.16, palette.plankDark, 0, 2.2, back + 0.08)) // the lintel
      g.add(box(0.1, 0.1, 0.1, palette.stoneDark, 0.45, 1.05, back + 0.18)) // the latch
      g.add(box(1.2, 0.02, 0.8, palette.cloth, 0, 0, back + 0.7)) // a rag rug
      door(g, 0, back + 1)
      break
    }
    case 'window': {
      const back = -D / 2
      g.add(box(1, 0.9, 0.06, palette.dusk, 0, 1.1, back + 0.04, 0.6)) // the dusk outside
      g.add(box(1.2, 0.08, 0.3, palette.wood, 0, 1.05, back + 0.15)) // the sill
      g.add(box(0.06, 0.9, 0.08, palette.plankDark, 0, 1.1, back + 0.08), box(1, 0.06, 0.08, palette.plankDark, 0, 1.55, back + 0.08))
      g.add(box(0.14, 0.16, 0.14, palette.milk, 0.3, 1.13, back + 0.17)) // the cup of milk on the sill
      break
    }
    default:
      g.add(ground(W, D, 0xaa44aa))
  }
  return g
}

// A placeholder character (forge 375 S2): a cloaked figure in one colour,
// its legs swung by walk(phase). About 1.8 m tall, facing -z.
export class CharacterModel {
  readonly group = new THREE.Group()
  private readonly legs: THREE.Mesh[] = []
  private readonly body: THREE.Group

  constructor(color: number) {
    this.body = new THREE.Group()
    const cloak = new THREE.Mesh(new THREE.ConeGeometry(0.45, 1.25, 7), material(color))
    cloak.position.y = 1.15
    const head = new THREE.Mesh(new THREE.IcosahedronGeometry(0.22, 0), material(0xd9b38c))
    head.position.y = 1.95
    const hood = new THREE.Mesh(new THREE.ConeGeometry(0.27, 0.35, 7), material(color))
    hood.position.y = 2.15
    for (const m of [cloak, head, hood]) {
      m.castShadow = true
      this.body.add(m)
    }
    for (const x of [-0.14, 0.14]) {
      const leg = new THREE.Mesh(new THREE.BoxGeometry(0.16, 0.6, 0.16), material(0x3a2a1e))
      leg.geometry.translate(0, -0.3, 0)
      leg.position.set(x, 0.6, 0)
      leg.castShadow = true
      this.legs.push(leg)
      this.group.add(leg)
    }
    this.group.add(this.body)
  }

  // phase in radians; amount 0 (standing) to 1 (walking)
  walk(phase: number, amount: number): void {
    const swing = Math.sin(phase) * 0.6 * amount
    this.legs[0].rotation.x = swing
    this.legs[1].rotation.x = -swing
    this.body.position.y = Math.abs(Math.cos(phase)) * 0.06 * amount
  }
}
