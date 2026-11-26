import * as THREE from 'three/webgpu'
import type { Input } from '../input/Input.ts'

// A top-down orbit camera for the story game's areas (forge 375 S1): it
// looks down at a target on the ground from a distance, turned by yaw and
// tilted by pitch. The right button drags to turn, the middle button or
// W A S D (and the arrows) pan, Q and E turn, the wheel zooms.
export class OrbitCamera {
  readonly camera: THREE.PerspectiveCamera
  readonly target = new THREE.Vector3()
  yaw = Math.PI * 0.15
  pitch = 0.95 // radians above the horizon
  distance = 60
  minDistance = 12
  maxDistance = 400
  panSpeed = 1.1 // distance-relative units per second
  private lastX = 0
  private lastY = 0
  private dragging = -1

  constructor(fov = 45, near = 0.5, far = 3000) {
    this.camera = new THREE.PerspectiveCamera(fov, 1, near, far)
  }

  step(dt: number, input: Input): void {
    // Drag: right turns, middle pans
    const x = input.pointerX
    const y = input.pointerY
    const btn = input.isButtonDown(2) ? 2 : input.isButtonDown(1) ? 1 : -1
    if (btn !== -1 && this.dragging === btn) {
      const dx = x - this.lastX
      const dy = y - this.lastY
      if (btn === 2) {
        this.yaw -= dx * 0.006
        this.pitch = Math.max(0.35, Math.min(1.45, this.pitch + dy * 0.004))
      } else {
        this.pan(-dx * this.distance * 0.0016, dy * this.distance * 0.0016)
      }
    }
    this.dragging = btn
    this.lastX = x
    this.lastY = y
    const k = (code: string) => (input.isDown(code) ? 1 : 0)
    const right = k('KeyD') + k('ArrowRight') - k('KeyA') - k('ArrowLeft')
    const fwd = k('KeyW') + k('ArrowUp') - k('KeyS') - k('ArrowDown')
    if (right || fwd) {
      const s = this.distance * this.panSpeed * dt * (input.isDown('ShiftLeft') ? 2.5 : 1)
      this.pan(right * s, fwd * s)
    }
    this.yaw += (k('KeyE') - k('KeyQ')) * 1.6 * dt
    const wheel = input.takeWheel()
    if (wheel) this.distance = Math.max(this.minDistance, Math.min(this.maxDistance, this.distance * (wheel > 0 ? 1.12 : 1 / 1.12)))
    this.apply()
  }

  // Moves the target along the ground, right and forward as seen on screen
  pan(right: number, forward: number): void {
    const sin = Math.sin(this.yaw)
    const cos = Math.cos(this.yaw)
    this.target.x += right * cos - forward * sin
    this.target.z += -right * sin - forward * cos
  }

  apply(): void {
    const h = Math.cos(this.pitch) * this.distance
    this.camera.position.set(
      this.target.x + Math.sin(this.yaw) * h,
      this.target.y + Math.sin(this.pitch) * this.distance,
      this.target.z + Math.cos(this.yaw) * h,
    )
    this.camera.lookAt(this.target)
  }

  // The camera as presence carries it
  state(): { x: number; z: number; yaw: number; pitch: number; distance: number } {
    return { x: this.target.x, z: this.target.z, yaw: this.yaw, pitch: this.pitch, distance: this.distance }
  }
}
