import * as THREE from 'three/webgpu'
import type { Input } from '../input/Input.ts'

// A free-flying camera: mouse to look (while the pointer is locked), W A S D
// to move, Space and C (or Ctrl) to rise and sink, Shift to go fast. Moved in
// fixed steps; the rendered position is interpolated between them.
export class FlyCamera {
  readonly camera: THREE.PerspectiveCamera
  yaw = 0
  pitch = 0
  // Metres per second; Shift multiplies it
  speed = 12
  boost = 6
  sensitivity = 0.0022
  private readonly prev = new THREE.Vector3()
  readonly position = new THREE.Vector3()

  constructor(fov = 70, near = 0.1, far = 4000) {
    this.camera = new THREE.PerspectiveCamera(fov, 1, near, far)
    this.camera.rotation.order = 'YXZ'
  }

  // Place the camera and aim it at a point
  lookFrom(position: THREE.Vector3, target: THREE.Vector3): void {
    this.position.copy(position)
    this.prev.copy(position)
    const d = target.clone().sub(position).normalize()
    this.yaw = Math.atan2(-d.x, -d.z)
    this.pitch = Math.asin(Math.max(-1, Math.min(1, d.y)))
    this.apply(1)
  }

  forward(out = new THREE.Vector3()): THREE.Vector3 {
    return out.set(-Math.sin(this.yaw) * Math.cos(this.pitch), Math.sin(this.pitch), -Math.cos(this.yaw) * Math.cos(this.pitch))
  }

  step(dt: number, input: Input): void {
    this.prev.copy(this.position)
    if (input.locked) {
      const { dx, dy } = input.takeMouse()
      this.yaw -= dx * this.sensitivity
      this.pitch = Math.max(-1.55, Math.min(1.55, this.pitch - dy * this.sensitivity))
    }
    const f = this.forward()
    const right = new THREE.Vector3(Math.cos(this.yaw), 0, -Math.sin(this.yaw))
    const move = new THREE.Vector3()
    if (input.isDown('KeyW')) move.add(f)
    if (input.isDown('KeyS')) move.sub(f)
    if (input.isDown('KeyD')) move.add(right)
    if (input.isDown('KeyA')) move.sub(right)
    if (input.isDown('Space')) move.y += 1
    if (input.isDown('KeyC') || input.isDown('ControlLeft')) move.y -= 1
    if (move.lengthSq() > 0) {
      const v = this.speed * (input.isDown('ShiftLeft') || input.isDown('ShiftRight') ? this.boost : 1)
      this.position.addScaledVector(move.normalize(), v * dt)
    }
  }

  // Set the three.js camera between the last two steps
  apply(alpha: number): void {
    this.camera.position.lerpVectors(this.prev, this.position, alpha)
    this.camera.rotation.set(this.pitch, this.yaw, 0)
    this.camera.updateMatrixWorld()
  }
}
