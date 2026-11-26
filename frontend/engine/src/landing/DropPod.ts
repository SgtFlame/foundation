import * as THREE from 'three/webgpu'
import type { Vec3 } from '../ship/Ship.ts'

// The drop pod (forge 360 V8): a new player's first arrival in a world.
// It falls from high above a point near the outpost, fires its retro
// rocket and settles on the ground; the player steps out beside it. The
// descent is a fixed curve, not physics: it only sets the scene.

export const DROP_HEIGHT = 160 // metres above the ground it starts
export const DROP_SECONDS = 9

// Height above the ground at a time: falling fast, braking hard at the end
export function dropHeight(t: number): number {
  const u = Math.min(1, Math.max(0, t / DROP_SECONDS))
  return DROP_HEIGHT * (1 - u) ** 2.2
}

export class DropPod {
  readonly group = new THREE.Group()
  private readonly flame: THREE.Mesh
  private ground: Vec3 = { x: 0, y: 0, z: 0 }
  private up: Vec3 = { x: 0, y: 1, z: 0 }
  private t = 0
  falling = false

  constructor() {
    const hull = new THREE.MeshStandardNodeMaterial({ color: 0xc8ccd2, roughness: 0.45, metalness: 0.4 })
    const body = new THREE.Mesh(new THREE.CylinderGeometry(0.9, 1.2, 2.4, 16), hull)
    body.position.y = 1.4
    const nose = new THREE.Mesh(new THREE.ConeGeometry(0.9, 1.1, 16), hull)
    nose.position.y = 3.15
    const band = new THREE.Mesh(new THREE.CylinderGeometry(1.22, 1.22, 0.18, 16), new THREE.MeshStandardNodeMaterial({ color: 0xe07a2a, roughness: 0.6 }))
    band.position.y = 0.5
    const hatch = new THREE.Mesh(new THREE.BoxGeometry(0.9, 1.3, 0.1), new THREE.MeshStandardNodeMaterial({ color: 0x2b3038, roughness: 0.7 }))
    hatch.position.set(0, 1.4, 1.02)
    this.group.add(body, nose, band, hatch)
    for (let i = 0; i < 3; i++) {
      const leg = new THREE.Mesh(new THREE.BoxGeometry(0.12, 0.9, 0.12), hull)
      const a = (i / 3) * Math.PI * 2
      leg.position.set(Math.cos(a) * 1.15, 0.35, Math.sin(a) * 1.15)
      leg.rotation.z = Math.cos(a) * 0.35
      leg.rotation.x = -Math.sin(a) * 0.35
      this.group.add(leg)
    }
    this.flame = new THREE.Mesh(new THREE.ConeGeometry(0.7, 3, 12, 1, true), new THREE.MeshBasicNodeMaterial({ color: 0xffb04a, transparent: true, opacity: 0.8, depthWrite: false }))
    this.flame.rotation.x = Math.PI
    this.flame.position.y = -1.3
    this.group.add(this.flame)
    this.group.visible = false
  }

  // Starts the fall onto a point of the ground, its up away from the pull
  start(ground: Vec3, up: Vec3): void {
    this.ground = { ...ground }
    this.up = { ...up }
    this.t = 0
    this.falling = true
    this.group.visible = true
    this.group.quaternion.setFromUnitVectors(new THREE.Vector3(0, 1, 0), new THREE.Vector3(up.x, up.y, up.z).normalize())
    this.place()
  }

  // One step of the fall; true once it has landed
  step(dt: number): boolean {
    if (!this.falling) return false
    this.t += dt
    this.place()
    if (this.t >= DROP_SECONDS) {
      this.falling = false
      this.flame.visible = false
      return true
    }
    // the retro rocket burns in the last third
    this.flame.visible = this.t > DROP_SECONDS * 0.55
    this.flame.scale.setScalar(0.8 + Math.random() * 0.4)
    return false
  }

  private place(): void {
    const h = dropHeight(this.t)
    const g = this.ground
    const u = this.up
    this.group.position.set(g.x + u.x * h, g.y + u.y * h, g.z + u.z * h)
  }

  position(): Vec3 {
    const p = this.group.position
    return { x: p.x, y: p.y, z: p.z }
  }

  // Where the player steps out: in front of the hatch
  hatch(): Vec3 {
    const p = new THREE.Vector3(0, 0.6, 2.4).applyMatrix4(this.group.matrixWorld)
    return { x: p.x, y: p.y, z: p.z }
  }
}
