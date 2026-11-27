import * as THREE from 'three/webgpu'
import { blockWorld, buildingDistance, cargoOf, type DesignBlock, type Helm, PUSH, type Quat, rotate, ShipBody, type ShipDesign, type ShipState, type ShipTerrain, type Vec3 } from './Ship.ts'

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
  log: 0xa8743f,
  door: 0x4a3020,
  bed: 0xa8463a,
  bench: 0x8a6a44,
  forge: 0x5a5a60,
  fabricator: 0x7c8794,
  concrete: 0xb8b4aa, // V33
  deck: 0x6e7884, // V34: the bay floor's
  clamp: 0xd9a33a,
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
        emissive: type === 'light' ? 0xfff59d : type === 'cockpit' ? 0x0b3c55 : type === 'refinery' ? 0x3a0e04 : type === 'log' || type === 'door' ? 0x3a2410 : 0x000000,
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
      // a block broken in a crash (V5) goes back to the outline
      if (!placed.has(i) && s.blocks[i]) {
        s.group.remove(s.blocks[i] as THREE.Object3D)
        s.blocks[i] = null
      }
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
    // V13: a door is an open frame; a bed a low frame with a blanket and a pillow
    if (b.type === 'door') {
      const g = new THREE.Group()
      for (const sx of [-0.45, 0.45]) {
        const post = new THREE.Mesh(this.box, this.material('door'))
        post.scale.set(0.1, 1, 0.3)
        post.position.x = sx
        g.add(post)
      }
      return g
    }
    if (b.type === 'bed') {
      const g = new THREE.Group()
      mesh.scale.set(0.9, 0.5, 0.95)
      const pillow = new THREE.Mesh(this.box, this.material('light'))
      pillow.scale.set(0.6, 0.12, 0.22)
      pillow.position.set(0, 0.3, -0.33)
      g.add(mesh, pillow)
      return g
    }
    // V14: a field bench is a table top on four legs, a vice at one end
    if (b.type === 'bench') {
      const g = new THREE.Group()
      mesh.scale.set(0.95, 0.12, 0.6)
      mesh.position.y = 0.28
      g.add(mesh)
      for (const sx of [-0.4, 0.4])
        for (const sz of [-0.22, 0.22]) {
          const leg = new THREE.Mesh(this.box, this.material('door'))
          leg.scale.set(0.08, 0.7, 0.08)
          leg.position.set(sx, -0.07, sz)
          g.add(leg)
        }
      const vice = new THREE.Mesh(this.box, this.material('armor'))
      vice.scale.set(0.18, 0.16, 0.2)
      vice.position.set(0.3, 0.42, 0)
      g.add(vice)
      return g
    }
    // V20: a forge is a squat stone furnace with a glowing mouth
    if (b.type === 'forge') {
      const g = new THREE.Group()
      mesh.scale.set(0.9, 0.8, 0.9)
      mesh.position.y = -0.1
      g.add(mesh)
      const mouth = new THREE.Mesh(this.box, new THREE.MeshBasicMaterial({ color: 0xff7a2a }))
      mouth.scale.set(0.4, 0.25, 0.05)
      mouth.position.set(0, -0.2, -0.46)
      const chimney = new THREE.Mesh(this.box, this.material('forge'))
      chimney.scale.set(0.25, 0.6, 0.25)
      chimney.position.set(0.2, 0.55, 0.2)
      g.add(mouth, chimney)
      return g
    }
    // V23: a fabricator is a grey cabinet with a green-lit screen and a tool arm
    if (b.type === 'fabricator') {
      const g = new THREE.Group()
      mesh.scale.set(0.85, 0.9, 0.7)
      mesh.position.y = -0.05
      g.add(mesh)
      const screen = new THREE.Mesh(this.box, new THREE.MeshBasicMaterial({ color: 0x5cff9c }))
      screen.scale.set(0.4, 0.22, 0.04)
      screen.position.set(0, 0.2, 0.37)
      const arm = new THREE.Mesh(this.box, this.material('armor'))
      arm.scale.set(0.08, 0.08, 0.4)
      arm.position.set(0.25, 0.48, 0.1)
      g.add(screen, arm)
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

  // Pieces of broken blocks thrown from where they were, falling and fading
  private readonly bits: { mesh: THREE.Mesh; v: THREE.Vector3; g: THREE.Vector3; life: number }[] = []
  private readonly bitGeo = new THREE.BoxGeometry(0.3, 0.3, 0.3)
  debris(id: string, indices: readonly number[], gravity: Vec3): void {
    const s = this.shown.get(id)
    if (!s) return
    const pose = { x: s.at.x, y: s.at.y, z: s.at.z, q: [s.q.x, s.q.y, s.q.z, s.q.w] as Quat }
    const up = new THREE.Vector3(-gravity.x, -gravity.y, -gravity.z).normalize()
    for (const i of indices) {
      const b = s.design.blocks[i]
      if (!b) continue
      const c = blockWorld(pose, b, s.design.blockSize)
      for (let k = 0; k < 5; k++) {
        const mesh = new THREE.Mesh(this.bitGeo, this.material(b.type))
        mesh.position.set(c.x + (Math.random() - 0.5) * 0.6, c.y + (Math.random() - 0.5) * 0.6, c.z + (Math.random() - 0.5) * 0.6)
        mesh.rotation.set(Math.random() * 3, Math.random() * 3, Math.random() * 3)
        const v = new THREE.Vector3(Math.random() - 0.5, Math.random() - 0.5, Math.random() - 0.5).multiplyScalar(5).addScaledVector(up, 2.5)
        this.group.add(mesh)
        this.bits.push({ mesh, v, g: new THREE.Vector3(gravity.x, gravity.y, gravity.z), life: 2.5 })
      }
    }
  }

  private stepDebris(dt: number): void {
    for (let i = this.bits.length - 1; i >= 0; i--) {
      const b = this.bits[i]
      b.life -= dt
      b.v.addScaledVector(b.g, dt)
      b.mesh.position.addScaledVector(b.v, dt)
      b.mesh.rotation.x += dt * 3
      b.mesh.scale.setScalar(Math.max(0.01, Math.min(1, b.life)))
      if (b.life <= 0) {
        this.group.remove(b.mesh)
        this.bits.splice(i, 1)
      }
    }
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

  // V13: a placed bed within reach of a point: the building's id
  bedNear(p: Vec3, reach = 2.2): string | null {
    return this.blockNear('bed', p, reach)
  }

  // V14: a placed field bench within reach of a point: the building's id
  benchNear(p: Vec3, reach = 2.2): string | null {
    return this.blockNear('bench', p, reach)
  }

  // V20: a placed forge within reach of a point: the building's id
  forgeNear(p: Vec3, reach = 2.2): string | null {
    return this.blockNear('forge', p, reach)
  }

  // V23: a placed fabricator within reach of a point: the building's id
  fabricatorNear(p: Vec3, reach = 1.7): string | null {
    return this.blockNear('fabricator', p, reach)
  }

  // Whether a shown ship has a block of a type placed (V23: the cabin's solar panel)
  hasPlaced(id: string, type: string): boolean {
    const s = this.shown.get(id)
    if (!s) return false
    return s.design.blocks.some((b, i) => b.type === type && s.placed.has(i))
  }

  private blockNear(type: string, p: Vec3, reach: number): string | null {
    for (const s of this.shown.values()) {
      const i = s.design.blocks.findIndex((b) => b.type === type)
      if (i < 0 || !s.placed.has(i)) continue
      const c = blockWorld({ x: s.at.x, y: s.at.y, z: s.at.z, q: [s.q.x, s.q.y, s.q.z, s.q.w] }, s.design.blocks[i], s.design.blockSize)
      if (Math.hypot(c.x - p.x, c.y - p.y, c.z - p.z) < reach) return s.state.id
    }
    return null
  }

  // V13: where to wake by a building's bed: just above it
  bedPoint(id: string): Vec3 | null {
    const s = this.shown.get(id)
    const i = s ? s.design.blocks.findIndex((b) => b.type === 'bed') : -1
    if (!s || i < 0) return null
    const pose = { x: s.at.x, y: s.at.y, z: s.at.z, q: [s.q.x, s.q.y, s.q.z, s.q.w] as Quat }
    const b = s.design.blocks[i]
    return blockWorld(pose, { ...b, at: [b.at[0] - 1, b.at[1] + 0.2, b.at[2]] as [number, number, number] }, s.design.blockSize)
  }

  // V13: the signed distance from a point to the nearest placed block of a
  // building (static designs), doors left open; Infinity with none near.
  // The walker takes the lesser of this and the terrain's, so walls stop it
  // and a roof or floor holds it up.
  densityAt(p: Vec3): number {
    let best = Infinity
    for (const s of this.shown.values()) {
      if (!s.design.static) continue
      if (s.at.distanceTo(new THREE.Vector3(p.x, p.y, p.z)) > 6 * s.design.blockSize + 4) continue
      const d = buildingDistance(s.design, s.placed, { x: s.at.x, y: s.at.y, z: s.at.z, q: [s.q.x, s.q.y, s.q.z, s.q.w] }, p)
      if (d < best) best = d
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

  // Takes the seat: the ship is flown from here on by its body (V26: weighing
  // what its hold carries)
  sit(id: string, terrain: ShipTerrain): ShipBody | null {
    const s = this.shown.get(id)
    if (!s) return null
    const st = s.state
    this.body = new ShipBody(s.design, st.placed, { x: s.at.x, y: s.at.y, z: s.at.z, q: [s.q.x, s.q.y, s.q.z, s.q.w], v: st.v, charge: st.charge, load: cargoOf(st) }, terrain)
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
    this.stepDebris(dt)
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

  // V7: where a crew member's camera rides on a ship someone else flies,
  // behind and above it as the pilot's chase camera, from its eased pose
  ride(id: string): { eye: Vec3; look: Vec3; up: Vec3 } | null {
    const s = this.shown.get(id)
    if (!s) return null
    const q: Quat = [s.q.x, s.q.y, s.q.z, s.q.w]
    const off = rotate(q, { x: 1.5, y: 3.6, z: 10 })
    const ahead = rotate(q, { x: 0, y: 0.5, z: -6 })
    const a = s.at
    return {
      eye: { x: a.x + off.x, y: a.y + off.y, z: a.z + off.z },
      look: { x: a.x + ahead.x, y: a.y + ahead.y, z: a.z + ahead.z },
      up: rotate(q, { x: 0, y: 1, z: 0 }),
    }
  }

  // V7: where to stand after leaving a ship ridden as crew: on its right
  exitOf(id: string): Vec3 | null {
    const s = this.shown.get(id)
    if (!s) return null
    const q: Quat = [s.q.x, s.q.y, s.q.z, s.q.w]
    const right = rotate(q, { x: 3, y: 0.5, z: 0 })
    return { x: s.at.x + right.x, y: s.at.y + right.y, z: s.at.z + right.z }
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
    this.bitGeo.dispose()
    this.nozzle.dispose()
    for (const m of this.mats.values()) m.dispose()
    this.ghostMat.dispose()
    this.aimMat.dispose()
  }
}
