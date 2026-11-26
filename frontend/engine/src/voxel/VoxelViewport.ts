import * as THREE from 'three/webgpu'
import { FlyCamera } from '../camera/FlyCamera.ts'
import { Loop } from '../core/Loop.ts'
import { StatsOverlay } from '../debug/StatsOverlay.ts'
import { Input } from '../input/Input.ts'
import { Renderer } from '../render/Renderer.ts'
import { mulberry32 } from './Noise.ts'
import type { EditKind, MoonParams, VoxelEdit } from './Types.ts'
import { VoxelWorld } from './VoxelWorld.ts'

// What the player asks for; the host sends it to the server, which decides
export interface EditIntent {
  kind: EditKind
  x: number
  y: number
  z: number
  radius: number
}

export interface CameraState {
  x: number
  y: number
  z: number
  yaw: number
  pitch: number
}

export interface RemoteCamera extends CameraState {
  id: string
  name: string
  color: string
}

export interface VoxelViewportOptions {
  params: MoonParams
  onEditIntent?: (e: EditIntent) => void
  // Called at most 10 times a second while the camera moves
  onCameraMove?: (c: CameraState) => void
  forceWebGL?: boolean
  viewDistance?: number
  // Show the frame-time overlay
  stats?: boolean
}

export interface ViewportStats {
  backend: string
  fps: number
  frameMs: number
  worstMs: number
  workMs: number
  drawCalls: number
  triangles: number
  chunksLoaded: number
  chunksMeshed: number
  queued: number
  sampleMs: number
  meshMs: number
  meshMB: number
  heapMB: number | null
  firstLoadMs: number | null
  edits: number
  editRoundTripMs: number | null
}

const MIN_BRUSH = 0.75
const MAX_BRUSH = 6
const REACH = 80

// A viewport onto a voxel moon: the renderer, the loop, a fly camera, the
// terrain, a brush that turns clicks into edit intents, other people's
// cameras and the stats overlay. Used by the Forge World tab and the
// engine's demo page; it knows nothing of React or the live connection.
export class VoxelViewport {
  readonly world: VoxelWorld
  readonly fly = new FlyCamera()
  readonly scene = new THREE.Scene()
  private readonly loop: Loop
  private readonly input: Input
  private readonly overlay: StatsOverlay | null
  private readonly brush: THREE.Mesh
  private readonly crosshair: HTMLDivElement
  private readonly hint: HTMLDivElement
  private brushRadius = 2
  private brushKind: EditKind = 'dig'
  private brushHit: THREE.Vector3 | null = null
  private repeat = 0
  // When each edit still on its way was asked for, oldest first
  private sentAt: number[] = []
  private readonly remotes = new Map<string, { mesh: THREE.Mesh; label: HTMLDivElement; state: RemoteCamera }>()
  private editRoundTripMs: number | null = null
  private lastCamera: CameraState | null = null

  private readonly container: HTMLElement
  private readonly renderer: Renderer
  private readonly opts: VoxelViewportOptions

  private constructor(container: HTMLElement, renderer: Renderer, opts: VoxelViewportOptions) {
    this.container = container
    this.renderer = renderer
    this.opts = opts
    const canvas = renderer.canvas
    canvas.tabIndex = 0
    this.input = new Input(canvas)
    this.world = new VoxelWorld({ params: opts.params, viewDistance: opts.viewDistance })
    this.scene.add(this.world.group)
    this.scene.background = new THREE.Color(0x05060a)
    this.addLightsAndSky()
    renderer.track(this.fly.camera)
    const R = opts.params.radius
    this.fly.lookFrom(new THREE.Vector3(R * 0.35, R * 1.25, R * 0.55), new THREE.Vector3(0, R * 0.9, 0))

    const brushMat = new THREE.MeshBasicNodeMaterial({ color: 0xff7043, transparent: true, opacity: 0.25, depthWrite: false })
    this.brush = new THREE.Mesh(new THREE.SphereGeometry(1, 24, 16), brushMat)
    this.brush.visible = false
    this.scene.add(this.brush)

    this.overlay = opts.stats === false ? null : new StatsOverlay(container)
    this.crosshair = document.createElement('div')
    this.crosshair.style.cssText =
      'position:absolute;left:50%;top:50%;width:14px;height:14px;margin:-7px 0 0 -7px;pointer-events:none;z-index:2;' +
      'border:2px solid rgba(255,255,255,0.75);border-radius:50%;display:none'
    container.appendChild(this.crosshair)
    this.hint = document.createElement('div')
    this.hint.style.cssText =
      'position:absolute;bottom:10px;left:50%;transform:translateX(-50%);padding:4px 10px;border-radius:4px;z-index:2;' +
      'font:13px system-ui,sans-serif;color:#eee;background:rgba(0,0,0,0.55);pointer-events:none;white-space:nowrap'
    container.appendChild(this.hint)
    this.updateHint()
    canvas.addEventListener('click', this.onCanvasClick)
    document.addEventListener('pointerlockchange', this.onLockChange)

    this.loop = new Loop({ step: (dt) => this.step(dt), render: (a) => this.render(a) })
    this.loop.start()
  }

  static async create(container: HTMLElement, opts: VoxelViewportOptions): Promise<VoxelViewport> {
    if (getComputedStyle(container).position === 'static') container.style.position = 'relative'
    const canvas = document.createElement('canvas')
    canvas.style.cssText = 'width:100%;height:100%;display:block;outline:none'
    container.appendChild(canvas)
    const renderer = await Renderer.create(canvas, { forceWebGL: opts.forceWebGL })
    return new VoxelViewport(container, renderer, opts)
  }

  private addLightsAndSky(): void {
    const sun = new THREE.DirectionalLight(0xfff4e0, 2.6)
    sun.position.set(0.6, 0.8, 0.35)
    this.scene.add(sun)
    this.scene.add(new THREE.HemisphereLight(0x8090b0, 0x201812, 0.35))
    // Stars: points on a far sphere
    const rand = mulberry32(42)
    const n = 2500
    const pos = new Float32Array(n * 3)
    for (let i = 0; i < n; i++) {
      const u = rand() * 2 - 1
      const a = rand() * Math.PI * 2
      const s = Math.sqrt(1 - u * u)
      pos.set([s * Math.cos(a) * 3000, u * 3000, s * Math.sin(a) * 3000], i * 3)
    }
    const g = new THREE.BufferGeometry()
    g.setAttribute('position', new THREE.BufferAttribute(pos, 3))
    const stars = new THREE.Points(g, new THREE.PointsNodeMaterial({ color: 0xcfd8ff, size: 1.5, sizeAttenuation: false }))
    stars.frustumCulled = false
    this.scene.add(stars)
  }

  private onCanvasClick = () => {
    this.renderer.canvas.focus()
    if (!this.input.locked) this.input.lock()
  }

  private onLockChange = () => {
    this.crosshair.style.display = this.input.locked ? 'block' : 'none'
    this.updateHint()
  }

  private updateHint(): void {
    this.hint.textContent = this.input.locked
      ? `Left: dig · Right: fill · Wheel: brush ${this.brushRadius.toFixed(2)} m · WASD, Space, C, Shift · Esc: release`
      : 'Click to fly · WASD to move · mouse to look'
  }

  private step(dt: number): void {
    const input = this.input
    this.fly.step(dt, input)
    const wheel = input.takeWheel()
    if (wheel !== 0) {
      this.brushRadius = Math.max(MIN_BRUSH, Math.min(MAX_BRUSH, this.brushRadius * (wheel < 0 ? 1.15 : 1 / 1.15)))
      this.updateHint()
    }
    // Aim the brush along the view
    if (input.locked) {
      const hit = this.world.raycast(this.fly.position, this.fly.forward(), REACH)
      this.brushHit = hit ? hit.point.clone() : null
      const digging = input.isButtonDown(0)
      const filling = input.isButtonDown(2)
      this.brushKind = filling && !digging ? 'fill' : digging ? 'dig' : this.brushKind
      if ((digging || filling) && this.brushHit) {
        this.repeat -= dt
        if (input.clicked(0) || input.clicked(2) || this.repeat <= 0) {
          this.repeat = 0.18
          this.sendEdit(filling && !digging ? 'fill' : 'dig', this.brushHit)
        }
      } else {
        this.repeat = 0
      }
    } else {
      this.brushHit = null
    }
    input.endStep()
  }

  private sendEdit(kind: EditKind, at: THREE.Vector3): void {
    // A fill sits on the surface; a dig is centred a little above it, so a
    // click carves a bowl rather than a buried bubble
    const n = at.clone().normalize()
    const c = kind === 'dig' ? at.clone().addScaledVector(n, this.brushRadius * 0.35) : at.clone().addScaledVector(n, -this.brushRadius * 0.2)
    this.sentAt.push(performance.now())
    if (this.sentAt.length > 64) this.sentAt.shift()
    this.opts.onEditIntent?.({ kind, x: c.x, y: c.y, z: c.z, radius: Number(this.brushRadius.toFixed(2)) })
  }

  private render(alpha: number): void {
    this.fly.apply(alpha)
    this.world.update(this.fly.camera.position)
    if (this.brushHit) {
      this.brush.visible = true
      this.brush.position.copy(this.brushHit)
      this.brush.scale.setScalar(this.brushRadius)
      ;(this.brush.material as THREE.MeshBasicNodeMaterial).color.set(this.brushKind === 'fill' ? 0x66bb6a : 0xff7043)
    } else {
      this.brush.visible = false
    }
    this.placeRemoteLabels()
    this.renderer.render(this.scene, this.fly.camera)
    this.reportCamera()
    if (this.overlay) this.updateOverlay()
  }

  private cameraTick = 0
  private reportCamera(): void {
    const now = performance.now()
    if (!this.opts.onCameraMove || now - this.cameraTick < 100) return
    const p = this.fly.position
    const c: CameraState = { x: round(p.x), y: round(p.y), z: round(p.z), yaw: round(this.fly.yaw, 3), pitch: round(this.fly.pitch, 3) }
    const l = this.lastCamera
    if (l && l.x === c.x && l.y === c.y && l.z === c.z && l.yaw === c.yaw && l.pitch === c.pitch) return
    this.cameraTick = now
    this.lastCamera = c
    this.opts.onCameraMove(c)
  }

  private updateOverlay(): void {
    const s = this.stats()
    const o = this.overlay as StatsOverlay
    o.set('backend', s.backend)
    o.set('fps', `${s.fps}  frame ${s.frameMs.toFixed(1)} ms  worst ${s.worstMs.toFixed(0)} ms  cpu ${s.workMs.toFixed(1)} ms`)
    o.set('draw', `${s.drawCalls} calls  ${(s.triangles / 1000).toFixed(0)}k tris`)
    o.set('chunks', `${s.chunksMeshed}/${s.chunksLoaded} meshed  ${s.queued} queued`)
    o.set('worker', `sample ${s.sampleMs.toFixed(1)} ms  mesh ${s.meshMs.toFixed(1)} ms per chunk`)
    o.set('memory', `mesh ${s.meshMB.toFixed(1)} MB${s.heapMB !== null ? `  heap ${s.heapMB.toFixed(0)} MB` : ''}`)
    o.set('load', s.firstLoadMs !== null ? `${(s.firstLoadMs / 1000).toFixed(2)} s to mesh the view` : 'loading…')
    o.set('edits', `${s.edits}${s.editRoundTripMs !== null ? `  round trip ${s.editRoundTripMs.toFixed(0)} ms` : ''}`)
  }

  // --- The host's side ---

  setParams(params: MoonParams): void {
    this.world.setParams(params)
  }

  setEdits(edits: readonly VoxelEdit[]): void {
    this.world.setEdits(edits)
  }

  // New edits from the server. mine counts those this viewport asked for,
  // to measure the round trip from the click to the edit coming back.
  addEdits(edits: readonly VoxelEdit[], mine = 0): void {
    for (let i = 0; i < mine; i++) {
      const t = this.sentAt.shift()
      if (t !== undefined) this.editRoundTripMs = performance.now() - t
    }
    this.world.addEdits(edits)
  }

  setRemoteCameras(cams: readonly RemoteCamera[]): void {
    const keep = new Set(cams.map((c) => c.id))
    for (const [id, r] of this.remotes) {
      if (keep.has(id)) continue
      this.scene.remove(r.mesh)
      r.mesh.geometry.dispose()
      r.label.remove()
      this.remotes.delete(id)
    }
    for (const c of cams) {
      let r = this.remotes.get(c.id)
      if (!r) {
        const mesh = new THREE.Mesh(new THREE.ConeGeometry(0.8, 2.2, 12), new THREE.MeshBasicNodeMaterial({ color: c.color }))
        const label = document.createElement('div')
        label.style.cssText =
          'position:absolute;transform:translate(-50%,-130%);padding:1px 6px;border-radius:3px;font:12px system-ui,sans-serif;' +
          `color:#fff;background:${c.color};pointer-events:none;white-space:nowrap;z-index:2`
        this.container.appendChild(label)
        this.scene.add(mesh)
        r = { mesh, label, state: c }
        this.remotes.set(c.id, r)
      }
      r.state = c
      r.label.textContent = c.name
      r.mesh.position.set(c.x, c.y, c.z)
      // Point the cone along the camera's view
      const f = new THREE.Vector3(-Math.sin(c.yaw) * Math.cos(c.pitch), Math.sin(c.pitch), -Math.cos(c.yaw) * Math.cos(c.pitch))
      r.mesh.quaternion.setFromUnitVectors(new THREE.Vector3(0, -1, 0), f.negate())
    }
  }

  private placeRemoteLabels(): void {
    const w = this.renderer.canvas.clientWidth
    const h = this.renderer.canvas.clientHeight
    const v = new THREE.Vector3()
    for (const r of this.remotes.values()) {
      v.copy(r.mesh.position).project(this.fly.camera)
      const visible = v.z < 1 && Math.abs(v.x) <= 1.1 && Math.abs(v.y) <= 1.1
      r.label.style.display = visible ? 'block' : 'none'
      if (visible) {
        r.label.style.left = `${((v.x + 1) / 2) * w}px`
        r.label.style.top = `${((1 - v.y) / 2) * h}px`
      }
    }
  }

  cameraState(): CameraState {
    const p = this.fly.position
    return { x: p.x, y: p.y, z: p.z, yaw: this.fly.yaw, pitch: this.fly.pitch }
  }

  setCamera(c: CameraState): void {
    this.fly.position.set(c.x, c.y, c.z)
    this.fly.yaw = c.yaw
    this.fly.pitch = c.pitch
    this.fly.lookFrom(this.fly.position.clone(), this.fly.position.clone().add(this.fly.forward()))
  }

  // A PNG data URL of the current view, rendered now
  screenshot(): string {
    this.renderer.render(this.scene, this.fly.camera)
    return this.renderer.screenshot()
  }

  stats(): ViewportStats {
    const l = this.loop.stats()
    const w = this.world.stats()
    const info = this.renderer.info()
    const mem = (performance as unknown as { memory?: { usedJSHeapSize: number } }).memory
    return {
      backend: this.renderer.backend,
      fps: l.fps,
      frameMs: l.frameMs,
      worstMs: l.worstMs,
      workMs: l.workMs,
      drawCalls: info.drawCalls,
      triangles: info.triangles,
      chunksLoaded: w.loaded,
      chunksMeshed: w.meshed,
      queued: w.queued + w.inFlight,
      sampleMs: w.sampleMs,
      meshMs: w.meshMs,
      meshMB: w.meshBytes / 1048576,
      heapMB: mem ? mem.usedJSHeapSize / 1048576 : null,
      firstLoadMs: w.firstFullLoadMs,
      edits: w.edits,
      editRoundTripMs: this.editRoundTripMs,
    }
  }

  dispose(): void {
    this.loop.stop()
    this.renderer.canvas.removeEventListener('click', this.onCanvasClick)
    document.removeEventListener('pointerlockchange', this.onLockChange)
    this.input.dispose()
    this.world.dispose()
    this.setRemoteCameras([])
    this.overlay?.dispose()
    this.crosshair.remove()
    this.hint.remove()
    this.renderer.canvas.remove()
    this.renderer.dispose()
  }
}

function round(v: number, places = 2): number {
  const f = 10 ** places
  return Math.round(v * f) / f
}
