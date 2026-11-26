import * as THREE from 'three/webgpu'
import { blockWorld, type DesignBlock, type Helm, PUSH, type Quat, rotate, ShipBody, type ShipDesign, type ShipState, type ShipTerrain, type Vec3 } from './Ship.ts'

// Ships in a voxel viewport (forge 360 V3): each ship's blocks drawn as
// boxes, the blocks still to place as a translucent outline the walker can
// aim at, and the one ship this window pilots flown locally by ShipBody
// while the others follow the server, carried on by their velocity.

const COLORS: Record<DesignBlock['type'], number> = {
  armor: 0x8d949c,
  cockpit: 0x4fc3f7,
  gyro: 0xffb300,
  light: 0xfff59d,
  thruster: 0x4e5b63,
  battery: 0x66bb6a,
  cargo: 0xa1887f,
  refinery: 0xc0583a,
  solar: 0x1a2a6c,
}

interface Shown {
  state: ShipState
  design: ShipDesign
  group: THREE.Group
  blocks: (THREE.Object3D | null)[]
  ghosts: (THREE.Mesh | null)[]
  placed: Set<number>
  receivedAt: number
  // Where it is drawn, eased toward where the server says it is
  at: THREE.Vector3
  q: THREE.Quaternion
}

export interface GhostHit {
  ship: string
  index: number
  distance: number
}

export class ShipLayer {
  readonly group = new THREE.Group()
  private readonly shown = new Map<string, Shown>()
  private designs = new Map<string, ShipDesign>()
  private readonly box = new THREE.BoxGeometry(0.96, 0.96, 0.96)
  private readonly nozzle = new THREE.ConeGeometry(0.34, 0.45, 12, 1, true)
  private readonly mats = new Map<string, THREE.Material>()
  private readonly ghostMat = new THREE.MeshBasicNodeMaterial({ color: 0x80deea, transparent: true, opacity: 0.14, depthWrite: false })
  private readonly aimMat = new THREE.MeshBasicNodeMaterial({ color: 0xa5ffb0, transparent: true, opacity: 0.5, depthWrite: false })
  private aimed: THREE.Mesh | null = null
  // The ship this window flies
  body: ShipBody | null = null
  pilotOf: string | null = null

  setDesigns(designs: readonly ShipDesign[]): void {
    this.designs = new Map(designs.map((d) => [d.id, d]))
    for (const s of this.shown.values()) this.setShip(s.state)
  }

  design(id: string): ShipDesign | undefined {
    return this.designs.get(id)
  }

  ships(): ShipState[] {
    return [...this.shown.values()].map((s) => s.state)
  }

  // The server's ships in the world; ships missing from the list are gone
  setShips(ships: readonly ShipState[]): void {
    const keep = new Set<string>()
    for (const s of ships) {
      keep.add(s.id)
      this.setShip(s)
    }
    for (const id of this.shown.keys()) if (!keep.has(id)) this.drop(id)
  }

  private material(type: DesignBlock['type']): THREE.Material {
    let m = this.mats.get(type)
    if (!m) {
      m = new THREE.MeshStandardNodeMaterial({
        color: COLORS[type],
        roughness: type === 'cockpit' ? 0.15 : 0.6,
        metalness: 0.1,
        emissive: type === 'light' ? 0xfff59d : type === 'cockpit' ? 0x0b3c55 : type === 'refinery' ? 0x3a0e04 : 0x000000,
      })
      this.mats.set(type, m)
    }
    return m
  }

  private setShip(st: ShipState): void {
    const design = this.designs.get(st.design)
    if (!design) return
    let s = this.shown.get(st.id)
    if (!s) {
      const group = new THREE.Group()
      group.name = `ship:${st.id}`
      this.group.add(group)
      s = {
        state: st,
        design,
        group,
        blocks: design.blocks.map(() => null),
        ghosts: design.blocks.map(() => null),
        placed: new Set(),
        receivedAt: performance.now(),
        at: new THREE.Vector3(st.x, st.y, st.z),
        q: new THREE.Quaternion(...st.q),
      }
      this.shown.set(st.id, s)
    }
    s.state = st
    s.receivedAt = performance.now()
    const placed = new Set(st.placed)
    design.blocks.forEach((b, i) => {
      const size = design.blockSize
      const local = new THREE.Vector3(b.at[0] * size, b.at[1] * size, b.at[2] * size)
      if (placed.has(i) && !s.blocks[i]) {
        const obj = this.blockMesh(b)
        obj.position.copy(local)
        s.group.add(obj)
        s.blocks[i] = obj
        const g = s.ghosts[i]
        if (g) {
          s.group.remove(g)
          s.ghosts[i] = null
        }
      } else if (!placed.has(i) && !s.ghosts[i]) {
        const g = new THREE.Mesh(this.box, this.ghostMat)
        g.position.copy(local)
        g.userData = { ship: st.id, index: i }
        s.group.add(g)
        s.ghosts[i] = g
      }
    })
    s.placed = placed
    // A ship the server moved a long way (or this window just saw) jumps
    if (this.pilotOf !== st.id && s.at.distanceTo(new THREE.Vector3(st.x, st.y, st.z)) > 30) {
      s.at.set(st.x, st.y, st.z)
      s.q.set(...st.q)
    }
    // The ship this window flies follows its body, unless it lost the seat
    if (this.pilotOf === st.id && this.body === null) this.pilotOf = null
  }

  private blockMesh(b: DesignBlock): THREE.Object3D {
    const mesh = new THREE.Mesh(this.box, this.material(b.type))
    // a solar panel is a thin slab on its block's floor
    if (b.type === 'solar') {
      mesh.scale.set(1, 0.12, 1)
      mesh.position.y = -0.42
      const g = new THREE.Group()
      g.add(mesh)
      return g
    }
    if (b.type !== 'thruster' || !b.push) return mesh
    // The nozzle opens away from where the thruster pushes
    const g = new THREE.Group()
    g.add(mesh)
    const out = PUSH[b.push]
    const n = new THREE.Mesh(this.nozzle, this.material('armor'))
    const d = new THREE.Vector3(-out.x, -out.y, -out.z)
    n.quaternion.setFromUnitVectors(new THREE.Vector3(0, -1, 0), d)
    n.position.copy(d.clone().multiplyScalar(0.6))
    g.add(n)
    return g
  }

  private drop(id: string): void {
    const s = this.shown.get(id)
    if (!s) return
    this.group.remove(s.group)
    this.shown.delete(id)
    if (this.pilotOf === id) {
      this.pilotOf = null
      this.body = null
    }
  }

  // The nearest outline block along a ray, within reach
  pickGhost(from: Vec3, dir: Vec3, reach: number): GhostHit | null {
    const ray = new THREE.Raycaster(new THREE.Vector3(from.x, from.y, from.z), new THREE.Vector3(dir.x, dir.y, dir.z).normalize(), 0, reach)
    const ghosts: THREE.Object3D[] = []
    for (const s of this.shown.values()) for (const g of s.ghosts) if (g) ghosts.push(g)
    const hit = ray.intersectObjects(ghosts, false)[0]
    if (!hit) return null
    const u = hit.object.userData as { ship: string; index: number }
    return { ship: u.ship, index: u.index, distance: hit.distance }
  }

  aim(hit: GhostHit | null): void {
    if (this.aimed) this.aimed.material = this.ghostMat
    this.aimed = null
    if (!hit) return
    const g = this.shown.get(hit.ship)?.ghosts[hit.index]
    if (g) {
      g.material = this.aimMat
      this.aimed = g
    }
  }

  // A ship whose placed cockpit is within reach of a point, nearest first
  cockpitNear(p: Vec3, reach = 3): string | null {
    let best: string | null = null
    let bestD = reach
    for (const s of this.shown.values()) {
      const i = s.design.blocks.findIndex((b) => b.type === 'cockpit')
      if (i < 0 || !s.placed.has(i)) continue
      const c = blockWorld({ x: s.at.x, y: s.at.y, z: s.at.z, q: [s.q.x, s.q.y, s.q.z, s.q.w] }, s.design.blocks[i], s.design.blockSize)
      const d = Math.hypot(c.x - p.x, c.y - p.y, c.z - p.z)
      if (d < bestD) {
        bestD = d
        best = s.state.id
      }
    }
    return best
  }

  // The ship with a block nearest a point, within reach
  shipNear(p: Vec3, reach = 4): ShipState | null {
    let best: ShipState | null = null
    let bestD = reach
    for (const s of this.shown.values()) {
      const pose = { x: s.at.x, y: s.at.y, z: s.at.z, q: [s.q.x, s.q.y, s.q.z, s.q.w] as Quat }
      for (const i of s.placed) {
        const c = blockWorld(pose, s.design.blocks[i], s.design.blockSize)
        const d = Math.hypot(c.x - p.x, c.y - p.y, c.z - p.z)
        if (d < bestD) {
          bestD = d
          best = s.state
        }
      }
    }
    return best
  }

  // Takes the seat: the ship is flown from here on by its body
  sit(id: string, terrain: ShipTerrain): ShipBody | null {
    const s = this.shown.get(id)
    if (!s) return null
    const st = s.state
    this.body = new ShipBody(s.design, st.placed, { x: s.at.x, y: s.at.y, z: s.at.z, q: [s.q.x, s.q.y, s.q.z, s.q.w], v: st.v, charge: st.charge }, terrain)
    // the panels charged it while it stood
    if (st.updatedAt) this.body.recharge(Math.max(0, (Date.now() - Date.parse(st.updatedAt)) / 1000))
    this.pilotOf = id
    return this.body
  }

  leave(): void {
    this.body = null
    this.pilotOf = null
  }

  // One fixed step: the flown ship by its body
  step(dt: number, helm: Helm): void {
    if (this.body) this.body.step(dt, helm)
  }

  // Each frame: the flown ship where its body is, the others eased toward
  // the server's place carried on by its velocity (at most half a second)
  frame(dt: number): void {
    const now = performance.now()
    for (const s of this.shown.values()) {
      if (this.pilotOf === s.state.id && this.body) {
        const b = this.body
        s.at.set(b.x, b.y, b.z)
        s.q.set(...b.q)
      } else {
        const ahead = Math.min(0.5, (now - s.receivedAt) / 1000)
        const st = s.state
        const target = new THREE.Vector3(st.x + st.v[0] * ahead, st.y + st.v[1] * ahead, st.z + st.v[2] * ahead)
        const k = 1 - Math.exp(-dt * 10)
        s.at.lerp(target, k)
        s.q.slerp(new THREE.Quaternion(...st.q), k)
      }
      s.group.position.copy(s.at)
      s.group.quaternion.copy(s.q)
    }
  }

  // Where the flown ship's chase camera sits and looks
  chase(): { eye: Vec3; look: Vec3; up: Vec3 } | null {
    const b = this.body
    if (!b) return null
    const q = b.q as Quat
    const off = rotate(q, { x: 0, y: 3.2, z: 9 })
    const ahead = rotate(q, { x: 0, y: 0.5, z: -6 })
    return {
      eye: { x: b.x + off.x, y: b.y + off.y, z: b.z + off.z },
      look: { x: b.x + ahead.x, y: b.y + ahead.y, z: b.z + ahead.z },
      up: rotate(q, { x: 0, y: 1, z: 0 }),
    }
  }

  // Where to stand after leaving the seat: beside the cockpit, on its left
  exitPoint(): Vec3 | null {
    const b = this.body
    const c = b?.cockpit()
    if (!b || !c) return null
    const left = rotate(b.q, { x: -2, y: 0.5, z: 0 })
    return { x: c.at.x + left.x, y: c.at.y + left.y, z: c.at.z + left.z }
  }

  dispose(): void {
    for (const id of this.shown.keys()) this.drop(id)
    this.box.dispose()
    this.nozzle.dispose()
    for (const m of this.mats.values()) m.dispose()
    this.ghostMat.dispose()
    this.aimMat.dispose()
  }
}
