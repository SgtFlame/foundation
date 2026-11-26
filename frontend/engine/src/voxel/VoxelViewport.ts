import * as THREE from 'three/webgpu'
import { surfaceBelow, Walker, type WalkerTerrain } from '../character/Walker.ts'
import { FlyCamera } from '../camera/FlyCamera.ts'
import { Loop } from '../core/Loop.ts'
import { StatsOverlay } from '../debug/StatsOverlay.ts'
import { Input } from '../input/Input.ts'
import { Renderer } from '../render/Renderer.ts'
import { mulberry32 } from './Noise.ts'
import { CARGO_PER_HOLD, IDLE_HELM, lookQuat, type Helm, type Quat, type ShipDesign, type ShipState } from '../ship/Ship.ts'
import { type GhostHit, ShipLayer } from '../ship/ShipLayer.ts'
import { DropPod } from '../landing/DropPod.ts'
import { firstHour, FirstHourPanel } from '../objectives/FirstHour.ts'
import { atOutpost, ContractPanel, handInProblem, OutpostModel, thrustFor, type ContractList, type Progress } from '../outpost/Outpost.ts'
import type { EditKind, MoonParams, VoxelEdit } from './Types.ts'
import { VoxelWorld } from './VoxelWorld.ts'

// What the player asks for; the host sends it to the server, which decides
export interface EditIntent {
  kind: EditKind
  x: number
  y: number
  z: number
  radius: number
  // What a dig took (m³ by material) or a fill needs, as this viewport
  // reckons it from the terrain it has; the server bounds it (V2)
  took?: { dust: number; rock: number; ore?: number }
  needs?: number
}

// What the player carries, as the server keeps it (V2)
export interface Carried {
  dust: number
  rock: number
  ore?: number // V8: from the asteroids
  capacity: number
}

export type ViewMode = 'fly' | 'walk'

// What the player asks of a ship (V3); the host sends each as a command
export type ShipIntent =
  | { kind: 'start'; id: string; design: string; x: number; y: number; z: number; q: Quat }
  | { kind: 'place'; id: string; index: number }
  | { kind: 'fly'; id: string; x: number; y: number; z: number; q: Quat; v: [number, number, number]; pilot: boolean; charge?: number }
  | { kind: 'load'; id: string } // V4: empty the pack into its hold
  | { kind: 'refine'; id: string } // V4: one run of its refinery
  | { kind: 'damage'; id: string; blocks: number[] } // V5: blocks a crash broke
  | { kind: 'handIn'; id: string; contract: string } // V6: a contract at the outpost
  | { kind: 'crew'; id: string; aboard: boolean } // V7: board or leave as crew
  | { kind: 'stand'; x: number; y: number; z: number } // V8: save where the player stands

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
  // Start by flying the camera (the editor's view) or walking (V2)
  mode?: ViewMode
  onModeChange?: (m: ViewMode) => void
  // Ships (V3): what the player asks of them, and who the player is, to
  // know which ship they fly
  onShipIntent?: (i: ShipIntent) => void
  userId?: string
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
  mode: ViewMode
  // Walking: on the ground, the thrust pack, the pull (m/s²) and speed (m/s)
  grounded: boolean
  thrustPack: boolean
  gravity: number
  speed: number
  ships: number
  crashes: number
  piloting: string | null
  // V6: the outpost's panel is open, and the player's skill levels
  outpost: boolean
  levels: Record<string, number>
  // V7: the ship this player rides as crew
  crew: string | null
  // V8: the first hour's steps done, and whether the drop pod is coming down
  firstHour: string[]
  landing: boolean
}

const MIN_BRUSH = 0.75
const MAX_BRUSH = 6
const REACH = 80
// A hand tool reaches this far from the eyes
const HAND_REACH = 8

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
  private mode: ViewMode = 'fly'
  private walker: Walker | null = null
  private carried: Carried | null = null
  private readonly pack: HTMLDivElement
  readonly shipLayer = new ShipLayer()
  private ghostHit: GhostHit | null = null
  private flyTick = 0
  // The outpost (V6)
  private readonly outpost = new OutpostModel()
  private readonly contracts: ContractPanel
  private contractList: ContractList | null = null
  private progress: Progress | null = null
  private outpostPlaced = false
  // V8: the first hour, and what this window saw of it
  private readonly journal: FirstHourPanel
  private walked = false
  private flew = false
  private journalAt = 0
  // V8: the landing and the saved place
  private readonly pod = new DropPod()
  private placed = false // the walker has been put where it belongs this session
  private savedAt: { x: number; y: number; z: number } | null = null
  private saveTick = 15
  // V7: the ship this player rides as crew, as the server says
  private riding: string | null = null
  private nearGiver = false

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
    this.scene.add(this.shipLayer.group)
    this.scene.add(this.outpost.group)
    this.scene.add(this.pod.group)
    this.contracts = new ContractPanel(container)
    this.journal = new FirstHourPanel(container)
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
    this.pack = document.createElement('div')
    this.pack.style.cssText =
      'position:absolute;left:10px;bottom:44px;padding:4px 10px;border-radius:4px;z-index:2;display:none;' +
      'font:12px system-ui,sans-serif;color:#eee;background:rgba(0,0,0,0.55);pointer-events:none;white-space:pre'
    this.pack.dataset.pack = ''
    container.appendChild(this.pack)
    if (opts.mode === 'walk') this.setMode('walk')
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
    if (this.shipLayer.pilotOf) {
      this.hint.textContent = this.input.locked
        ? 'W/S forward and back · A/D sideways · Space up · C down · mouse to turn · Q/E roll · F to leave the seat · Esc: release'
        : 'Click to fly the ship'
      return
    }
    if (this.pod.falling) {
      this.hint.textContent = 'Your drop pod is coming down by the outpost…'
      return
    }
    if (this.riding) {
      const s = this.shipLayer.ships().find((x) => x.id === this.riding)
      this.hint.textContent = this.input.locked
        ? `Riding as crew${s?.pilot ? '' : ' (nobody flies it yet)'} · R: refine rock · B: leave the ship · Esc: release`
        : 'Click to ride along'
      return
    }
    if (this.mode === 'walk') {
      this.hint.textContent = this.input.locked
        ? `${this.nearGiver ? `T: talk to ${this.contractList?.giver.name ?? 'the quartermaster'} · ` : ''}${this.ghostHit ? 'Left: place this block · ' : ''}${this.nearSeat ? 'F: sit in the cockpit · ' : ''}${this.nearShipHint()}Left: dig · Right: fill · Wheel: brush ${this.brushRadius.toFixed(2)} m · WASD walk · Space jump · Shift run · X thrust pack${this.walker?.thrustPack ? ' (on: Space up, C down)' : ''} · G fly · Esc: release`
        : 'Click to walk · G to fly the camera instead'
      return
    }
    this.hint.textContent = this.input.locked
      ? `Left: dig · Right: fill · Wheel: brush ${this.brushRadius.toFixed(2)} m · WASD, Space, C, Shift · Esc: release`
      : 'Click to fly · WASD to move · mouse to look · G to walk'
  }

  // --- Walking (V2) ---

  private terrain(): WalkerTerrain {
    const w = this.world
    return { density: (x, y, z) => w.densityAt(x, y, z), gravity: (x, y, z, out) => w.generator.gravity(x, y, z, out) }
  }

  get viewMode(): ViewMode {
    return this.mode
  }

  // Flying the camera, or walking: walking drops the character on the
  // ground below the camera (or onto the moon's north pole)
  setMode(m: ViewMode): void {
    if (m === this.mode) return
    if (m === 'walk') {
      const t = this.terrain()
      const p = this.fly.position
      const R = this.world.generator.params.radius
      const at = surfaceBelow(t, { x: p.x, y: p.y, z: p.z }) ?? surfaceBelow(t, { x: 0, y: R * 1.5, z: 0 }) ?? { x: 0, y: R + 2, z: 0 }
      this.walker = new Walker(t, at)
      this.placed = false
      // Face the way the camera faced, along the ground
      const f = this.fly.forward()
      const u = this.walker.up
      const along = f.clone().addScaledVector(new THREE.Vector3(u.x, u.y, u.z), -f.dot(new THREE.Vector3(u.x, u.y, u.z)))
      if (along.lengthSq() > 1e-6) {
        along.normalize()
        this.walker.forward = { x: along.x, y: along.y, z: along.z }
      }
      this.fly.camera.near = 0.05
    } else if (this.walker) {
      const e = this.walker.eye()
      const l = this.walker.look()
      this.fly.lookFrom(new THREE.Vector3(e.x, e.y, e.z), new THREE.Vector3(e.x + l.x, e.y + l.y, e.z + l.z))
      this.fly.camera.up.set(0, 1, 0)
      this.fly.camera.near = 0.1
      this.walker = null
    }
    this.fly.camera.updateProjectionMatrix()
    this.mode = m
    if (m === 'walk') this.arrive()
    this.updateHint()
    this.drawPack()
    this.opts.onModeChange?.(m)
  }

  // What the player carries, from the server
  setCarried(c: Carried | null): void {
    this.carried = c
    this.drawPack()
  }

  // Where the walker stands, for tests and tools
  walkerState(): { x: number; y: number; z: number; grounded: boolean; thrustPack: boolean; up: { x: number; y: number; z: number } } | null {
    const w = this.walker
    return w ? { ...w.pos, grounded: w.grounded, thrustPack: w.thrustPack, up: { ...w.up } } : null
  }

  private drawPack(): void {
    const c = this.carried
    const w = this.walker
    const aboard = this.shipLayer.pilotOf ?? this.riding
    const flying = aboard ? (this.shipLayer.ships().find((s) => s.id === aboard) ?? null) : null
    const ship = this.shipLine(flying ?? this.nearShip)
    if (this.mode !== 'walk' && !c && !ship) {
      this.pack.style.display = 'none'
      return
    }
    const lines: string[] = []
    if (ship) lines.push(ship)
    if (c) {
      const ore = c.ore ?? 0
      const total = c.dust + c.rock + ore
      lines.push(`Pack ${total.toFixed(2)} / ${c.capacity.toFixed(1)} m³ · dust ${c.dust.toFixed(2)} · rock ${c.rock.toFixed(2)}${ore > 0 ? ` · ore ${ore.toFixed(2)}` : ''}`)
    }
    if (w && !this.riding && !this.pod.falling) {
      const g = Math.hypot(w.gravity.x, w.gravity.y, w.gravity.z)
      lines.push(`Pull ${g.toFixed(2)} m/s² · ${Math.hypot(w.vel.x, w.vel.y, w.vel.z).toFixed(1)} m/s · thrust pack ${w.thrustPack ? 'on' : 'off'}${w.grounded ? '' : ' · in the air'}`)
    }
    this.pack.textContent = lines.join('\n')
    this.pack.style.display = lines.length ? 'block' : 'none'
  }

  private stepWalker(dt: number): void {
    const w = this.walker as Walker
    const input = this.input
    if (input.locked) {
      const { dx, dy } = input.takeMouse()
      w.turn(dx * 0.0022, -dy * 0.0022)
    }
    if (input.pressed('KeyX')) {
      w.thrustPack = !w.thrustPack
      this.updateHint()
    }
    const k = (code: string) => (input.isDown(code) ? 1 : 0)
    w.step(dt, {
      forward: k('KeyW') - k('KeyS'),
      right: k('KeyD') - k('KeyA'),
      up: k('Space') - (w.thrustPack ? k('KeyC') + k('ControlLeft') : 0),
      sprint: input.isDown('ShiftLeft') || input.isDown('ShiftRight'),
    })
    // The camera at the eyes, its up the walker's
    const e = w.eye()
    const l = w.look()
    const cam = this.fly.camera
    cam.position.set(e.x, e.y, e.z)
    cam.up.set(w.up.x, w.up.y, w.up.z)
    cam.lookAt(e.x + l.x, e.y + l.y, e.z + l.z)
    this.fly.position.copy(cam.position)
    this.packTick -= dt
    if (this.packTick <= 0) {
      this.packTick = 0.2
      this.drawPack()
    }
    this.saveTick -= dt
    if (this.saveTick <= 0) {
      this.saveTick = 15
      this.saveStand()
    }
  }
  private packTick = 0

  private step(dt: number): void {
    const input = this.input
    if (this.shipLayer.pilotOf && this.shipLayer.body) {
      this.stepPilot(dt)
      input.endStep()
      return
    }
    if (this.riding) {
      this.stepRider(dt)
      input.endStep()
      return
    }
    if (this.pod.falling) {
      this.stepLanding(dt)
      input.endStep()
      return
    }
    if (input.pressed('KeyG') && input.locked) this.setMode(this.mode === 'fly' ? 'walk' : 'fly')
    if (input.pressed('KeyJ') && input.locked) {
      this.journal.shown = !this.journal.shown
      this.drawJournal()
    }
    if (this.mode === 'walk' && this.walker) this.stepWalker(dt)
    else this.fly.step(dt, input)
    this.stepShipsOnFoot()
    const wheel = input.takeWheel()
    if (wheel !== 0) {
      this.brushRadius = Math.max(MIN_BRUSH, Math.min(MAX_BRUSH, this.brushRadius * (wheel < 0 ? 1.15 : 1 / 1.15)))
      this.updateHint()
    }
    // Aim the brush along the view
    if (input.locked) {
      const from = this.walker ? this.fly.camera.position : this.fly.position
      const dir = this.walker ? (({ x, y, z }) => new THREE.Vector3(x, y, z))(this.walker.look()) : this.fly.forward()
      const hit = this.world.raycast(from, dir, this.walker ? HAND_REACH : REACH)
      this.brushHit = hit ? hit.point.clone() : null
      // An outline block nearer than the ground is placed rather than dug
      if (this.ghostHit && (!hit || this.ghostHit.distance < hit.distance)) {
        this.brushHit = null
        if (input.clicked(0)) {
          this.opts.onShipIntent?.({ kind: 'place', id: this.ghostHit.ship, index: this.ghostHit.index })
        }
        input.endStep()
        return
      }
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

  // --- Ships (V3) ---

  private nearSeat: string | null = null

  // What a ship within reach offers: its hold and its refinery (V4)
  private nearShip: ShipState | null = null
  private nearShipHint(): string {
    const s = this.nearShip
    if (!s) return ''
    const d = this.shipLayer.design(s.design)
    const has = (type: string) => d?.blocks.some((b, i) => b.type === type && s.placed.includes(i))
    return `${has('cargo') ? 'L: load your pack into the hold · ' : ''}${has('refinery') ? 'R: refine rock · ' : ''}${this.canBoard(s) ? 'B: board as crew · ' : ''}`
  }

  // A line about a ship: its charge, hold and power
  private shipLine(s: ShipState | null): string | null {
    if (!s) return null
    const d = this.shipLayer.design(s.design)
    if (!d) return null
    const holds = d.blocks.filter((b, i) => b.type === 'cargo' && s.placed.includes(i)).length
    const body = this.shipLayer.pilotOf === s.id ? this.shipLayer.body : null
    const parts = [d.title]
    if (d.blocks.some((b) => b.type === 'battery' || b.type === 'solar')) {
      const charge = body ? body.charge : (s.charge ?? 0)
      parts.push(`charge ${Math.round(charge * 100)}%${body && body.power < 0.999 ? ` · power ${Math.round(body.power * 100)}%` : ''}`)
    }
    if (holds) {
      const c = s.cargo ?? { dust: 0, rock: 0, iron: 0 }
      const ore = c.ore ?? 0
      parts.push(`hold ${(c.dust + c.rock + c.iron + ore).toFixed(2)} / ${holds * CARGO_PER_HOLD} m³ · dust ${c.dust.toFixed(2)} · rock ${c.rock.toFixed(2)} · iron ${c.iron.toFixed(2)}${ore > 0 ? ` · ore ${ore.toFixed(2)}` : ''}`)
    }
    return parts.join(' · ')
  }

  // On foot: aim at outline blocks, and F sits in a cockpit within reach
  private stepShipsOnFoot(): void {
    const input = this.input
    let ghost: GhostHit | null = null
    if (input.locked && this.walker) {
      const l = this.walker.look()
      const p = this.fly.camera.position
      ghost = this.shipLayer.pickGhost({ x: p.x, y: p.y, z: p.z }, l, HAND_REACH)
    }
    const seat = this.walker ? this.shipLayer.cockpitNear(this.walker.eye()) : null
    if ((ghost?.index ?? -1) !== (this.ghostHit?.index ?? -1) || ghost?.ship !== this.ghostHit?.ship || seat !== this.nearSeat) {
      this.ghostHit = ghost
      this.nearSeat = seat
      this.shipLayer.aim(ghost)
      this.updateHint()
    }
    if (seat && input.locked && input.pressed('KeyF')) this.sit(seat)
    const near = this.walker ? this.shipLayer.shipNear(this.walker.eye()) : null
    if (near?.id !== this.nearShip?.id || near?.seq !== this.nearShip?.seq) {
      this.nearShip = near
      this.updateHint()
      this.drawPack()
    }
    if (near && input.locked && input.pressed('KeyL')) this.opts.onShipIntent?.({ kind: 'load', id: near.id })
    if (near && input.locked && input.pressed('KeyR')) this.opts.onShipIntent?.({ kind: 'refine', id: near.id })
    if (near && input.locked && input.pressed('KeyB') && this.canBoard(near)) this.opts.onShipIntent?.({ kind: 'crew', id: near.id, aboard: true })
    this.stepOutpost()
  }

  // --- Landing and the saved place (V8) ---

  // Once a walk starts and the player's progress is known: back where they
  // last stood, or, the first time in this world, down in a drop pod
  private arrive(): void {
    const w = this.walker
    if (!w || this.placed || !this.progress) return
    this.placed = true
    const at = this.progress.at
    if (at) {
      w.pos = { x: at[0], y: at[1] + 0.3, z: at[2] }
      w.vel = { x: 0, y: 0, z: 0 }
      this.savedAt = { x: at[0], y: at[1], z: at[2] }
      return
    }
    const g = this.world.generator.params
    const ground = surfaceBelow(this.terrain(), { x: 22, y: g.radius + g.amplitude + 20, z: 14 }, 80)
    if (!ground) return
    const gr = this.terrain().gravity(ground.x, ground.y, ground.z, { x: 0, y: 0, z: 0 })
    const gl = Math.hypot(gr.x, gr.y, gr.z) || 1
    this.pod.start(ground, { x: -gr.x / gl, y: -gr.y / gl, z: -gr.z / gl })
    this.updateHint()
  }

  // The pod's fall, the camera watching it from beside the landing point
  private stepLanding(dt: number): void {
    const landed = this.pod.step(dt)
    const p = this.pod.position()
    const cam = this.fly.camera
    const g = this.terrain().gravity(p.x, p.y, p.z, { x: 0, y: 0, z: 0 })
    const gl = Math.hypot(g.x, g.y, g.z) || 1
    const up = { x: -g.x / gl, y: -g.y / gl, z: -g.z / gl }
    const base = this.pod.hatch()
    cam.position.set(base.x + up.x * 3 + 10, base.y + up.y * 3, base.z + up.z * 3 + 14)
    cam.up.set(up.x, up.y, up.z)
    cam.lookAt(p.x, p.y + 1.5, p.z)
    this.fly.position.copy(cam.position)
    this.input.takeMouse()
    if (landed && this.walker) {
      const out = this.pod.hatch()
      this.walker.pos = { ...out }
      this.walker.vel = { x: 0, y: 0, z: 0 }
      this.walker.forward = { x: -0.8, y: 0, z: -0.6 }
      this.saveStand(true)
      this.hint.textContent = 'Landed. The beacon at the north pole is the outpost: walk there and press T by the quartermaster.'
      setTimeout(() => this.updateHint(), 5000)
    }
  }

  // Saves where the player stands now and then, when they have moved
  private saveStand(force = false): void {
    const w = this.walker
    if (!w || this.pod.falling) return
    const s = this.savedAt
    if (!force && s && Math.hypot(w.pos.x - s.x, w.pos.y - s.y, w.pos.z - s.z) < 3) return
    if (!force && !w.grounded) return
    const r = (v: number) => Math.round(v * 100) / 100
    this.savedAt = { ...w.pos }
    this.opts.onShipIntent?.({ kind: 'stand', x: r(w.pos.x), y: r(w.pos.y), z: r(w.pos.z) })
  }

  // --- The first hour (V8) ---

  private journalSteps() {
    const blocks: Record<string, number> = {}
    for (const id of ['skiff', 'prospector', 'hauler']) {
      const d = this.shipLayer.design(id)
      if (d) blocks[id] = d.blocks.length
    }
    return firstHour({ walked: this.walked, flew: this.flew, pack: this.carried, ships: this.shipLayer.ships(), blocks, progress: this.progress })
  }

  private drawJournal(): void {
    if (this.mode === 'walk') this.walked = true
    if (this.shipLayer.pilotOf && this.shipLayer.body && Math.hypot(this.shipLayer.body.v.x, this.shipLayer.body.v.y, this.shipLayer.body.v.z) > 1) this.flew = true
    const playing = this.mode === 'walk' || !!this.shipLayer.pilotOf || !!this.riding
    this.journal.show(this.journalSteps(), playing && !this.contracts.open)
  }

  // --- Crew (V7) ---

  // A finished ship someone else may fly with its crew seat free
  private canBoard(s: ShipState): boolean {
    const me = this.opts.userId ?? ''
    const d = this.shipLayer.design(s.design)
    return !!d && s.placed.length >= d.blocks.length && s.pilot !== me && !(s.crew ?? []).length
  }

  // Riding as crew: the camera on the ship as it is flown, R refines, B leaves
  private stepRider(dt: number): void {
    const id = this.riding as string
    const input = this.input
    if (input.locked && input.pressed('KeyB')) this.opts.onShipIntent?.({ kind: 'crew', id, aboard: false })
    if (input.locked && input.pressed('KeyR')) this.opts.onShipIntent?.({ kind: 'refine', id })
    input.takeMouse()
    this.packTick -= dt
    if (this.packTick <= 0) {
      this.packTick = 0.2
      this.drawPack()
    }
  }

  // Follows the ship's eased pose each frame, smoother than the fixed step
  private placeRider(): void {
    const c = this.riding ? this.shipLayer.ride(this.riding) : null
    if (!c) return
    const cam = this.fly.camera
    cam.position.set(c.eye.x, c.eye.y, c.eye.z)
    cam.up.set(c.up.x, c.up.y, c.up.z)
    cam.lookAt(c.look.x, c.look.y, c.look.z)
    this.fly.position.copy(cam.position)
  }

  // Boarding or leaving, as the server's ships say
  private followCrew(ships: readonly ShipState[]): void {
    const me = this.opts.userId ?? ''
    const now = me ? (ships.find((s) => (s.crew ?? []).includes(me))?.id ?? null) : null
    if (now === this.riding) return
    const was = this.riding
    this.riding = now
    if (!now && was) {
      const out = this.shipLayer.exitOf(was)
      if (this.walker && out) {
        this.walker.pos = { ...out }
        this.walker.vel = { x: 0, y: 0, z: 0 }
      }
    }
    if (now) {
      this.ghostHit = null
      this.shipLayer.aim(null)
      this.brushHit = null
      this.contracts.hide()
    }
    this.updateHint()
    this.drawPack()
  }

  // --- The outpost (V6) ---

  // Stands the beacon on the ground at the north pole once the terrain is there
  private placeOutpost(): void {
    if (this.outpostPlaced) return
    const g = this.world.generator.params
    const t = this.terrain()
    const at = surfaceBelow(t, { x: 0, y: g.radius + g.amplitude + 10, z: 0 }, 60)
    if (!at) return
    this.outpost.place(at, { x: 0, y: 1, z: 0 })
    this.outpostPlaced = true
  }

  // T beside the quartermaster opens or closes the contracts
  private stepOutpost(): void {
    const w = this.walker
    let near = false
    if (w && this.outpostPlaced) {
      const g = this.outpost.giver()
      const e = w.eye()
      near = Math.hypot(e.x - g.x, e.y - g.y, e.z - g.z) < 4
    }
    if (near !== this.nearGiver) {
      this.nearGiver = near
      if (!near) this.contracts.hide()
      this.updateHint()
    }
    if (near && this.input.locked && this.input.pressed('KeyT')) {
      if (this.contracts.open) this.contracts.hide()
      else this.openContracts()
    }
  }

  // The panel, its rows worked out from the ship parked nearest the beacon
  openContracts(): boolean {
    const list = this.contractList
    if (!list) return false
    const g = this.world.generator.params
    const parked = this.shipLayer.ships().filter((s) => atOutpost(s, g.radius, g.amplitude))
    const rows = list.contracts.map((c) => {
      // the first ship that qualifies, else the first parked, decides the row
      const blocks = (s: ShipState) => this.shipLayer.design(s.design)?.blocks.length ?? 0
      const ok = parked.find((s) => !handInProblem(c.id, s, this.progress, blocks(s), c))
      const s = ok ?? parked[0] ?? null
      return { id: c.id, ship: s?.id ?? '', problem: handInProblem(c.id, s, this.progress, s ? blocks(s) : 0, c) }
    })
    this.contracts.show(list, rows, this.progress, (id) => {
      const r = rows.find((x) => x.id === id)
      if (r?.ship) this.opts.onShipIntent?.({ kind: 'handIn', id: r.ship, contract: id })
    })
    return true
  }

  // The contracts offered and the player's progress, from the server
  setOutpost(list: ContractList | null): void {
    this.contractList = list
    if (this.contracts.open) this.openContracts()
  }

  setProgress(p: Progress | null): void {
    const before = this.progress
    this.progress = p
    if (before && p) {
      for (const [skill, lv] of Object.entries(p.levels)) {
        if (lv > (before.levels[skill] ?? 1)) {
          this.hint.textContent = `Your ${skill} is now level ${lv}${skill === 'piloting' ? ': your thrusters answer you harder' : ''}.`
          setTimeout(() => this.updateHint(), 3000)
        }
      }
    }
    if (this.shipLayer.body) this.shipLayer.body.thrustScale = thrustFor(p?.levels)
    if (this.mode === 'walk') this.arrive()
    if (this.contracts.open) this.openContracts()
  }

  // Sits in a ship's cockpit and flies it from here on
  sit(id: string): boolean {
    const st = this.shipLayer.ships().find((s) => s.id === id)
    if (!st || (st.pilot && st.pilot !== (this.opts.userId ?? ''))) return false
    const body = this.shipLayer.sit(id, this.terrain())
    if (!body) return false
    body.thrustScale = thrustFor(this.progress?.levels)
    this.contracts.hide()
    this.ghostHit = null
    this.shipLayer.aim(null)
    this.brushHit = null
    this.sendFly(true)
    this.updateHint()
    return true
  }

  // Leaves the seat, standing beside the cockpit
  leaveSeat(): void {
    const id = this.shipLayer.pilotOf
    if (!id) return
    const out = this.shipLayer.exitPoint()
    this.sendFly(false)
    this.shipLayer.leave()
    if (this.walker && out) {
      this.walker.pos = { ...out }
      this.walker.vel = { x: 0, y: 0, z: 0 }
    }
    this.updateHint()
  }

  private sendFly(pilot: boolean): void {
    const b = this.shipLayer.body
    const id = this.shipLayer.pilotOf
    if (!b || !id) return
    const s = b.state()
    const r = (v: number) => Math.round(v * 1000) / 1000
    this.opts.onShipIntent?.({
      kind: 'fly',
      id,
      x: r(s.x),
      y: r(s.y),
      z: r(s.z),
      q: s.q.map((c) => Math.round(c * 1e5) / 1e5) as Quat,
      v: s.v.map(r) as [number, number, number],
      pilot,
      ...(b.powered ? { charge: Math.round(b.charge * 1e4) / 1e4 } : {}),
    })
  }

  // A hard landing broke blocks (V5): tell the server, throw the pieces,
  // and put the pilot out when the cockpit broke; true when they were
  private crashed(): boolean {
    const b = this.shipLayer.body
    const id = this.shipLayer.pilotOf
    if (!b || !id) return false
    const broken = b.takeBroken()
    if (!broken.length) return false
    this.opts.onShipIntent?.({ kind: 'damage', id, blocks: broken })
    const at = { x: b.x, y: b.y, z: b.z }
    this.shipLayer.debris(id, broken, this.terrain().gravity(at.x, at.y, at.z, { x: 0, y: 0, z: 0 }))
    this.crashes++
    const d = this.shipLayer.design(this.shipLayer.ships().find((s) => s.id === id)?.design ?? '')
    const cockpit = d?.blocks.findIndex((x) => x.type === 'cockpit') ?? -1
    this.hint.textContent = `Crunch: ${broken.length} block${broken.length === 1 ? '' : 's'} broken. Place them again from the outline.`
    if (broken.includes(cockpit)) {
      // thrown out: the server already seats nobody, so no flight is sent
      const out = this.shipLayer.exitPoint()
      this.shipLayer.leave()
      if (this.walker && out) {
        this.walker.pos = { ...out }
        this.walker.vel = { x: 0, y: 0, z: 0 }
      }
      this.hint.textContent = 'The cockpit is smashed and you are thrown clear. Place it again from the outline.'
      return true
    }
    setTimeout(() => this.updateHint(), 2500)
    return false
  }
  private crashes = 0

  // The helm from the keys and mouse: the mouse turns at a rate
  private helm(): Helm {
    const input = this.input
    if (!input.locked) return IDLE_HELM
    const k = (code: string) => (input.isDown(code) ? 1 : 0)
    const { dx, dy } = input.takeMouse()
    const c = (v: number) => Math.max(-1, Math.min(1, v))
    return {
      forward: k('KeyW') - k('KeyS'),
      right: k('KeyD') - k('KeyA'),
      up: k('Space') - k('KeyC') - k('ControlLeft'),
      yaw: c(dx * 0.08),
      pitch: c(-dy * 0.08),
      roll: k('KeyE') - k('KeyQ'),
    }
  }

  private stepPilot(dt: number): void {
    const input = this.input
    if (input.locked && input.pressed('KeyF')) {
      this.leaveSeat()
      return
    }
    this.shipLayer.step(dt, this.helm())
    if (this.crashed()) return
    this.packTick -= dt
    if (this.packTick <= 0) {
      this.packTick = 0.2
      this.drawPack()
    }
    this.flyTick -= dt
    if (this.flyTick <= 0) {
      this.flyTick = 0.25
      this.sendFly(true)
    }
    const c = this.shipLayer.chase()
    if (c) {
      const cam = this.fly.camera
      cam.position.set(c.eye.x, c.eye.y, c.eye.z)
      cam.up.set(c.up.x, c.up.y, c.up.z)
      cam.lookAt(c.look.x, c.look.y, c.look.z)
      this.fly.position.copy(cam.position)
    }
  }

  setShipDesigns(designs: readonly ShipDesign[]): void {
    this.shipLayer.setDesigns(designs)
  }

  // The world's ships from the server
  setShips(ships: readonly ShipState[]): void {
    this.shipLayer.setShips(ships)
    this.followCrew(ships)
    // Someone else took the seat (or the ship is gone): stand up
    const id = this.shipLayer.pilotOf
    if (id) {
      const st = ships.find((s) => s.id === id)
      if (!st || (st.pilot && st.pilot !== (this.opts.userId ?? ''))) this.leaveSeat()
    }
  }

  // Starts a ship of a design a few metres ahead, on the ground, facing
  // the way the player faces; gives its id, or null without a design
  startShip(design: string): string | null {
    if (!this.shipLayer.design(design)) return null
    const t = this.terrain()
    const w = this.walker
    const f = w ? w.forward : (({ x, y, z }) => ({ x, y, z }))(this.fly.forward())
    const from = w ? w.pos : (({ x, y, z }) => ({ x, y, z }))(this.fly.position)
    const ahead = { x: from.x + f.x * 7, y: from.y + f.y * 7, z: from.z + f.z * 7 }
    const g = t.gravity(ahead.x, ahead.y, ahead.z, { x: 0, y: 0, z: 0 })
    const gl = Math.hypot(g.x, g.y, g.z) || 1
    const up = { x: -g.x / gl, y: -g.y / gl, z: -g.z / gl }
    const lift = { x: ahead.x + up.x * 20, y: ahead.y + up.y * 20, z: ahead.z + up.z * 20 }
    const ground = surfaceBelow(t, lift, 80) ?? ahead
    // its lowest blocks are one below block 0 0 0: stand them on the ground
    const at = { x: ground.x + up.x * 2.05, y: ground.y + up.y * 2.05, z: ground.z + up.z * 2.05 }
    const q = lookQuat(f, up)
    const id = 's' + Date.now().toString(36) + Math.floor(Math.random() * 1296).toString(36)
    const r = (v: number) => Math.round(v * 1000) / 1000
    this.opts.onShipIntent?.({ kind: 'start', id, design, x: r(at.x), y: r(at.y), z: r(at.z), q: q.map((c) => Math.round(c * 1e5) / 1e5) as Quat })
    return id
  }

  private sendEdit(kind: EditKind, at: THREE.Vector3): void {
    // A fill sits on the surface; a dig is centred a little above it, so a
    // click carves a bowl rather than a buried bubble
    // Up at that spot: away from the pull of the body it is on
    const g = this.world.generator.gravity(at.x, at.y, at.z, { x: 0, y: 0, z: 0 })
    const n = new THREE.Vector3(-g.x, -g.y, -g.z).normalize()
    const c = kind === 'dig' ? at.clone().addScaledVector(n, this.brushRadius * 0.35) : at.clone().addScaledVector(n, -this.brushRadius * 0.2)
    const radius = Number(this.brushRadius.toFixed(2))
    const has = this.world.contentsOf(c.x, c.y, c.z, radius)
    const r2 = (v: number) => Math.round(v * 100) / 100
    this.sentAt.push(performance.now())
    if (this.sentAt.length > 64) this.sentAt.shift()
    this.opts.onEditIntent?.({
      kind,
      x: c.x,
      y: c.y,
      z: c.z,
      radius,
      ...(kind === 'dig' ? { took: { dust: r2(has.dust), rock: r2(has.rock), ...(has.ore > 0 ? { ore: r2(has.ore) } : {}) } } : { needs: r2(has.empty) }),
    })
  }

  private lastFrame = performance.now()
  private render(alpha: number): void {
    if (this.mode === 'fly' && !this.shipLayer.pilotOf && !this.riding) this.fly.apply(alpha)
    const now = performance.now()
    this.shipLayer.frame(Math.min(0.1, (now - this.lastFrame) / 1000))
    this.placeOutpost()
    this.placeRider()
    if (now - this.journalAt > 500) {
      this.journalAt = now
      this.drawJournal()
    }
    this.outpost.blink(now / 1000)
    this.lastFrame = now
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
    let yaw = this.fly.yaw
    let pitch = this.fly.pitch
    if (this.walker) {
      const l = this.walker.look()
      yaw = Math.atan2(-l.x, -l.z)
      pitch = Math.asin(Math.max(-1, Math.min(1, l.y)))
    }
    const c: CameraState = { x: round(p.x), y: round(p.y), z: round(p.z), yaw: round(yaw, 3), pitch: round(pitch, 3) }
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
      mode: this.mode,
      grounded: this.walker?.grounded ?? false,
      thrustPack: this.walker?.thrustPack ?? false,
      gravity: this.walker ? Math.hypot(this.walker.gravity.x, this.walker.gravity.y, this.walker.gravity.z) : 0,
      speed: this.shipLayer.body
        ? Math.hypot(this.shipLayer.body.v.x, this.shipLayer.body.v.y, this.shipLayer.body.v.z)
        : this.walker
          ? Math.hypot(this.walker.vel.x, this.walker.vel.y, this.walker.vel.z)
          : 0,
      ships: this.shipLayer.ships().length,
      crashes: this.crashes,
      piloting: this.shipLayer.pilotOf,
      outpost: this.contracts.open,
      levels: { ...(this.progress?.levels ?? {}) },
      crew: this.riding,
      firstHour: this.journalSteps().filter((x) => x.done).map((x) => x.id),
      landing: this.pod.falling,
    }
  }

  dispose(): void {
    if (this.mode === 'walk' && !this.shipLayer.pilotOf && !this.riding) this.saveStand()
    this.loop.stop()
    this.renderer.canvas.removeEventListener('click', this.onCanvasClick)
    document.removeEventListener('pointerlockchange', this.onLockChange)
    this.input.dispose()
    this.world.dispose()
    this.shipLayer.dispose()
    this.contracts.dispose()
    this.journal.dispose()
    this.setRemoteCameras([])
    this.overlay?.dispose()
    this.crosshair.remove()
    this.hint.remove()
    this.pack.remove()
    this.renderer.canvas.remove()
    this.renderer.dispose()
  }
}

function round(v: number, places = 2): number {
  const f = 10 ** places
  return Math.round(v * f) / f
}
