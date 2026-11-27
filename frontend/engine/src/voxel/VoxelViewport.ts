import { forgeAction, SMELT_MS, type ForgeState } from '../craft/Forge.ts'
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
import { PLANET } from './Bodies.ts'
import { available, Fishing, FLEE_RANGE, floatModel, grazerDir, grazerModel, herdOf, HUNT_REACH, SEA_RADIUS, seaMesh, type Grazer } from '../wildlife/Wildlife.ts'
import { eat, FRESH, hungry, NeedsHud, ratesFor, rounded, stepNeeds, type Needs } from '../survival/Needs.ts'
import { countdown, DRONE_HIT, DRONE_SCRAP, dronesOf, shotHits, surgeAt, TURRET_RANGE, TURRET_RELOAD, turretPick, type Drone } from '../surge/Surge.ts'
import { MANUAL_BLOCK, nearestSalvage, SALVAGE_ORE, salvageKey, wreckSites, type Vec3 as WVec3 } from '../wreck/Wreck.ts'
import { digLimits, missing, named, nextRecipe, spend, type DigLimits } from '../craft/Crafting.ts'
import { scan, scanLine, SCAN_COOLDOWN, SCAN_SHOW } from '../craft/Scanner.ts'
import { MATERIAL_ORE } from './Types.ts'
import { Hold, secondsFor, spotKey, toolPower, workFor, type Contents } from '../craft/Mining.ts'

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
  food?: number // V9: rations harvested on the planet
  iron?: number // V20: smelted at a cabin's forge
  capacity: number
}

// V9: a crop in the planet's soil, as game.crops keeps it
export interface CropState {
  x: number
  y: number
  z: number
  at: number // planted, Unix ms
}

// A crop ripens this long after it is planted (the server's RipeSeconds)
export const RIPE_MS = 90_000

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
  | { kind: 'stand'; x: number; y: number; z: number; needs?: Needs } // V8: save where the player stands (V10: and their needs)
  | { kind: 'eat' } // V10: one ration from the pack
  | { kind: 'downDrone'; surge: number; drone: number } // V16: a swarm surge's drone shot down
  | { kind: 'placeTurret'; x: number; y: number; z: number } // V17: the player's sentry turret set down on the moon
  | { kind: 'salvage'; wreck: string; index: number; x: number; y: number; z: number } // V15: a wreck's block torn off for scrap
  | { kind: 'forage'; what: 'fish' | 'hunt'; animal?: string; x: number; y: number; z: number } // V12: a fish caught, a grazer hunted
  | { kind: 'sleep'; id: string } // V13: a night in a cabin's bed
  | { kind: 'craft'; id: string; item: string } // V14: a tool made at a cabin's bench
  | { kind: 'smelt'; id: string } // V20: ore from the pack put in a cabin's forge
  | { kind: 'takeIngots'; id: string } // V20: the forge's iron taken into the pack
  | { kind: 'plant'; x: number; y: number; z: number } // V9: barley in the planet's soil
  | { kind: 'harvest'; crop: string } // V9: a ripe crop, as food

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
  // V9: the crops shown, and how deep in the planet's air the camera is (0 out of it, 1 on the ground)
  crops: number
  air: number
  // V10: the body's needs while playing, and how often the player blacked out
  needs: Needs | null
  breathable: boolean
  blackouts: number
  // V12: the grazers shown near the player, and the fishing line
  grazers: number
  fishing: 'idle' | 'waiting' | 'bite'
}

const MIN_BRUSH = 0.75
const MAX_BRUSH = 6
const HARVEST_SECONDS = 1.5 // holding H on a ripe crop
const SALVAGE_WORK = 3 // work to cut a wreck's plate free (3 s by hand)
const CHIP_GEOMETRY = new THREE.BoxGeometry(0.07, 0.07, 0.07)
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
  // Working the ground by holding (the user, 2026-11-26): the spot held, its
  // contents, the ring that fills and the chips that fly
  private readonly mining = new Hold()
  private miningSpot: { key: string; has: Contents & { empty: number }; centre: THREE.Vector3; radius: number } | null = null
  private readonly workRing: HTMLDivElement
  private readonly workLabel: HTMLDivElement
  private readonly chips = new THREE.Group()
  private chipList: { mesh: THREE.Mesh; v: THREE.Vector3; life: number }[] = []
  private chipTick = 0
  // Hold actions by key (salvaging, harvesting): the target held and when it is done
  private readonly actionHold = new Hold()
  private frameDt = 0
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
  // V9: the planet with air seen from afar, its glow, the crops and the sky's depth of air
  private readonly cropsGroup = new THREE.Group()
  private readonly crops = new Map<string, { state: CropState; group: THREE.Group; ripe: boolean }>()
  private airDepth = 0
  private stars: THREE.Points | null = null
  private nearCrop: { id: string; ripe: boolean; left: number } | null = null
  // V8: the landing and the saved place
  private readonly pod = new DropPod()
  private placed = false // the walker has been put where it belongs this session
  private savedAt: { x: number; y: number; z: number } | null = null
  private saveTick = 15
  // V7: the ship this player rides as crew, as the server says
  private riding: string | null = null
  private nearGiver = false
  // V10: the body's needs, shown while on foot or aboard
  private needs: Needs = { ...FRESH }
  private savedNeeds: Needs | null = null
  private readonly needsHud: NeedsHud
  private breathable = true
  private blackouts = 0
  // V12: the sea, the herd and the fishing line
  private readonly wildGroup = new THREE.Group()
  private readonly wreckGroup = new THREE.Group() // V15
  private wrecks: { id: string; meshes: THREE.Mesh[]; centres: WVec3[] }[] = []
  private salvaged: Record<string, boolean> = {}
  private nearWreck: { wreck: string; index: number } | null = null
  // V16: swarm surges
  surgeOffset = 0 // ms added to the clock (tests)
  private readonly surgeGroup = new THREE.Group()
  private surgeHud: HTMLDivElement | null = null
  private surgeIndex = -1
  private swarm: { d: Drone; mesh: THREE.Mesh; at: THREE.Vector3 | null; gone: boolean }[] = []
  private surgeTick = 0
  // V17: sentry turrets, by user id, and when each fires next
  private turrets: Record<string, { at: THREE.Vector3; mesh: THREE.Group; head: THREE.Object3D; reload: number }> = {}
  private readonly turretGroup = new THREE.Group()
  // V21: the ore scanner's marks and its timers
  private readonly scanGroup = new THREE.Group()
  private scanLeft = 0
  private scanCool = 0
  myUserId = ''
  private herd: { g: Grazer; model: THREE.Group; shown: boolean; run: number; at: THREE.Vector3 }[] = []
  private taken: Record<string, number> = {}
  private readonly fishing = new Fishing(7)
  private readonly float = floatModel()
  private wildTick = 0
  private nearGrazer: string | null = null
  private facingWater: THREE.Vector3 | null = null

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
    this.needsHud = new NeedsHud(container)
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
    this.workRing = document.createElement('div')
    this.workRing.dataset.work = ''
    this.workRing.style.cssText =
      'position:absolute;left:50%;top:50%;width:38px;height:38px;margin:-19px 0 0 -19px;pointer-events:none;z-index:2;border-radius:50%;display:none;' +
      '-webkit-mask:radial-gradient(circle,transparent 13px,#000 14px);mask:radial-gradient(circle,transparent 13px,#000 14px)'
    container.appendChild(this.workRing)
    this.workLabel = document.createElement('div')
    this.workLabel.style.cssText =
      'position:absolute;left:50%;top:50%;transform:translate(-50%,26px);pointer-events:none;z-index:2;display:none;font:11px system-ui,sans-serif;color:#eee;text-shadow:0 0 3px #000;white-space:nowrap'
    container.appendChild(this.workLabel)
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
    this.stars = stars
    // V9: the planet with air, a short flight away: a sphere seen from afar
    // (just under its lowest ground, so its terrain covers it close up) and
    // a thin glow of air round it
    const globe = new THREE.Mesh(new THREE.SphereGeometry(PLANET.radius - 4, 48, 32), new THREE.MeshStandardNodeMaterial({ color: 0x4f6b3a, roughness: 1 }))
    globe.position.set(PLANET.x, PLANET.y, PLANET.z)
    const glow = new THREE.Mesh(
      new THREE.SphereGeometry(PLANET.radius + PLANET.air, 48, 32),
      new THREE.MeshBasicNodeMaterial({ color: 0x7fb0ff, transparent: true, opacity: 0.12, side: THREE.BackSide, depthWrite: false }),
    )
    glow.position.copy(globe.position)
    this.scene.add(globe, glow, this.cropsGroup)
    // V12: the sea in the planet's hollows, its herd and a fishing float
    this.float.visible = false
    this.scene.add(seaMesh(), this.wildGroup, this.float, this.wreckGroup, this.surgeGroup, this.turretGroup, this.scanGroup, this.chips)
  }

  // V9: the sky turns blue and the stars fade as the camera goes down into the planet's air
  private stepSky(): void {
    const c = this.fly.camera.position
    const alt = Math.hypot(c.x - PLANET.x, c.y - PLANET.y, c.z - PLANET.z) - PLANET.radius
    const t = Math.min(1, Math.max(0, 1 - (alt - 15) / PLANET.air))
    if (Math.abs(t - this.airDepth) < 0.005) return
    this.airDepth = t
    ;(this.scene.background as THREE.Color).setRGB(0.02 + t * 0.46, 0.024 + t * 0.64, 0.04 + t * 0.86)
    if (this.stars) this.stars.visible = t < 0.6
  }

  // V9: the world's crops, as game.crops keeps them
  setCrops(crops: Record<string, CropState>): void {
    for (const [id, c] of this.crops) {
      if (crops[id]) continue
      this.cropsGroup.remove(c.group)
      this.crops.delete(id)
    }
    for (const [id, st] of Object.entries(crops)) {
      if (this.crops.has(id)) continue
      const group = cropModel()
      const up = new THREE.Vector3(st.x - PLANET.x, st.y - PLANET.y, st.z - PLANET.z).normalize()
      group.position.set(st.x, st.y, st.z)
      group.quaternion.setFromUnitVectors(new THREE.Vector3(0, 1, 0), up)
      this.cropsGroup.add(group)
      this.crops.set(id, { state: st, group, ripe: false })
    }
    this.growCrops()
  }

  // Each crop's size and colour from its age
  private growCrops(): void {
    const now = Date.now()
    for (const c of this.crops.values()) {
      const f = Math.min(1, Math.max(0, (now - c.state.at) / RIPE_MS))
      c.group.scale.setScalar(0.3 + 0.7 * f)
      const ripe = f >= 1
      if (ripe !== c.ripe) {
        c.ripe = ripe
        for (const m of c.group.children) ((m as THREE.Mesh).material as THREE.MeshStandardNodeMaterial).color.set(ripe ? 0xd9b44a : 0x6f9a3c)
      }
    }
  }

  // On the planet's soil?
  private onPlanet(p: THREE.Vector3): boolean {
    const r = Math.hypot(p.x - PLANET.x, p.y - PLANET.y, p.z - PLANET.z)
    return r > PLANET.radius - 15 && r < PLANET.radius + 16
  }

  // P plants where the player looks, H harvests the ripe crop beside them
  private stepFarming(): void {
    const at = this.fly.camera.position
    let best: { id: string; ripe: boolean; left: number } | null = null
    let bd = 2.8
    for (const [id, c] of this.crops) {
      const d = Math.hypot(c.state.x - at.x, c.state.y - at.y, c.state.z - at.z) - 1.6
      if (d < bd) {
        bd = d
        best = { id, ripe: c.ripe, left: Math.max(0, Math.ceil((c.state.at + RIPE_MS - Date.now()) / 1000)) }
      }
    }
    const changed = (best?.id ?? '') + (best?.ripe ? 'r' : '') !== (this.nearCrop?.id ?? '') + (this.nearCrop?.ripe ? 'r' : '')
    this.nearCrop = best
    if (changed) this.updateHint()
    if (!this.input.locked) return
    if (best?.ripe && this.holdKey('KeyH', `crop:${best.id}`, HARVEST_SECONDS, 'cutting the barley', '#e8c95a')) this.opts.onShipIntent?.({ kind: 'harvest', crop: best.id })
    if (this.input.pressed('KeyP') && this.brushHit && this.onPlanet(this.brushHit)) {
      const h = this.brushHit
      this.opts.onShipIntent?.({ kind: 'plant', x: round2(h.x), y: round2(h.y), z: round2(h.z) })
    }
  }

  // --- Fishing and hunting (V12) ---

  // The grazers taken lately, as game.wildlife keeps them
  setWildlife(w: { taken: Record<string, number> } | null): void {
    this.taken = { ...(w?.taken ?? {}) }
  }

  private buildHerd(): void {
    for (const h of this.herd) this.wildGroup.remove(h.model)
    this.herd = herdOf(this.world.generator.params.seed).map((g) => {
      const model = grazerModel()
      model.visible = false
      this.wildGroup.add(model)
      return { g, model, shown: false, run: 0, at: new THREE.Vector3() }
    })
  }

  // The ground's radius along a direction from the planet's centre (the generator's, so far ones show too)
  private groundRadius(d: { x: number; y: number; z: number }): number {
    const gen = this.world.generator
    let lo = PLANET.radius - 15
    let hi = PLANET.radius + 15
    for (let i = 0; i < 14; i++) {
      const mid = (lo + hi) / 2
      if (gen.density(PLANET.x + d.x * mid, PLANET.y + d.y * mid, PLANET.z + d.z * mid) < 0) lo = mid
      else hi = mid
    }
    return lo
  }

  private stepWild(dt: number): void {
    const w = this.walker as Walker
    const me = new THREE.Vector3(w.pos.x, w.pos.y, w.pos.z)
    const near = Math.hypot(w.pos.x - PLANET.x, w.pos.y - PLANET.y, w.pos.z - PLANET.z) < PLANET.radius + 80
    if (!this.herd.length) this.buildHerd()
    this.wildTick -= dt
    const t = Date.now() / 1000
    let nearest: string | null = null
    let nd = HUNT_REACH
    for (const h of this.herd) {
      const free = near && available(this.taken[h.g.id], Date.now())
      if (!free) {
        h.model.visible = false
        h.shown = false
        continue
      }
      // a grazer runs along its round when the player comes close
      if (h.shown && h.at.distanceTo(me) < FLEE_RANGE) h.run += dt * 6
      if (this.wildTick <= 0 || h.run > 0) {
        const d = grazerDir(h.g, t + h.run)
        const r = this.groundRadius(d)
        h.shown = r > SEA_RADIUS + 0.3
        h.at.set(PLANET.x + d.x * r, PLANET.y + d.y * r, PLANET.z + d.z * r)
        const up = new THREE.Vector3(d.x, d.y, d.z)
        const ahead = new THREE.Vector3().copy(h.at).sub(h.model.position)
        h.model.position.copy(h.at)
        h.model.quaternion.setFromUnitVectors(new THREE.Vector3(0, 1, 0), up)
        if (ahead.lengthSq() > 1e-6) {
          // face the way it moves
          const fwd = ahead.sub(up.clone().multiplyScalar(ahead.dot(up))).normalize()
          const local = fwd.applyQuaternion(h.model.quaternion.clone().invert())
          h.model.rotateY(Math.atan2(local.x, local.z))
        }
      }
      h.model.visible = h.shown
      const dist = h.at.distanceTo(me)
      if (h.shown && dist < nd) {
        nd = dist
        nearest = h.g.id
      }
    }
    if (this.wildTick <= 0) this.wildTick = 0.25
    // the water the player looks at, within a cast
    const eye = this.fly.camera.position
    const look = w.look()
    const water = this.waterHit(eye, look, 14)
    const ground = this.brushHit ? this.brushHit.distanceTo(eye) : Infinity
    const facing = water && water.distanceTo(eye) < ground ? water : null
    const changed = nearest !== this.nearGrazer || !!facing !== !!this.facingWater
    this.nearGrazer = nearest
    this.facingWater = facing
    if (changed) this.updateHint()
    const ev = this.fishing.step(dt)
    if (ev === 'bite') this.flashHint('A bite! Press N to strike')
    if (ev === 'escaped') {
      this.float.visible = false
      this.flashHint('It got away')
    }
    if (this.fishing.state === 'bite') this.float.position.addScaledVector(this.float.up, Math.sin(t * 30) * 0.02)
    if (!this.input.locked) return
    if (this.input.pressed('KeyV') && nearest) {
      const h = this.herd.find((x) => x.g.id === nearest)!
      this.taken[nearest] = Date.now()
      this.opts.onShipIntent?.({ kind: 'forage', what: 'hunt', animal: nearest, x: round2(w.pos.x), y: round2(w.pos.y), z: round2(w.pos.z) })
      h.model.visible = false
      this.flashHint('You bring the grazer down: three rations')
    }
    if (this.input.pressed('KeyN')) this.fish(facing)
  }

  // N: cast toward the water, strike on a bite, or reel in
  private fish(at: THREE.Vector3 | null): void {
    const w = this.walker as Walker
    if (this.fishing.state === 'idle') {
      if (!at) return this.flashHint('Look at the water within 14 m to cast')
      this.fishing.cast()
      const up = new THREE.Vector3(at.x - PLANET.x, at.y - PLANET.y, at.z - PLANET.z).normalize()
      this.float.position.copy(at)
      this.float.up.copy(up)
      this.float.visible = true
      this.flashHint('The float settles on the water. Wait for a bite…')
      return
    }
    const ev = this.fishing.strike()
    this.float.visible = false
    if (ev === 'caught') {
      this.opts.onShipIntent?.({ kind: 'forage', what: 'fish', x: round2(w.pos.x), y: round2(w.pos.y), z: round2(w.pos.z) })
      this.flashHint('A fish! One ration')
    } else this.flashHint('Too soon: the line comes up empty')
  }

  // Where a ray meets the sea's surface from above, within a distance
  private waterHit(o: THREE.Vector3, d: { x: number; y: number; z: number }, max: number): THREE.Vector3 | null {
    const ox = o.x - PLANET.x
    const oy = o.y - PLANET.y
    const oz = o.z - PLANET.z
    const b = ox * d.x + oy * d.y + oz * d.z
    const c = ox * ox + oy * oy + oz * oz - SEA_RADIUS * SEA_RADIUS
    if (c < 0) return null // under the water
    const disc = b * b - c
    if (disc < 0) return null
    const t = -b - Math.sqrt(disc)
    if (t < 0 || t > max) return null
    return new THREE.Vector3(o.x + d.x * t, o.y + d.y * t, o.z + d.z * t)
  }

  private onCanvasClick = () => {
    this.renderer.canvas.focus()
    if (!this.input.locked) this.input.lock()
  }

  private onLockChange = () => {
    this.crosshair.style.display = this.input.locked ? 'block' : 'none'
    this.updateHint()
  }

  // V9: what P and H do here
  private farmHint(): string {
    const c = this.nearCrop
    if (c) return c.ripe ? 'Hold H: harvest the barley · ' : `Barley: ripe in ${c.left} s · `
    const wild = `${this.nearGrazer ? 'V: hunt the grazer · ' : ''}${this.fishing.state !== 'idle' ? 'N: strike · ' : this.facingWater ? 'N: cast a line · ' : ''}`
    return wild + (this.walker && this.onPlanet(this.fly.camera.position) ? 'P: plant barley where you look · ' : '')
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
        ? `${this.nearGiver ? `T: talk to ${this.contractList?.giver.name ?? 'the quartermaster'} · ` : ''}${this.ghostHit ? 'Left: place this block · ' : ''}${this.nearSeat ? 'F: sit in the cockpit · ' : ''}${this.nearBed ? 'F: sleep in the bed · ' : ''}${this.forgeHint()}${this.benchHint()}${this.nearWreck ? 'Hold Z: cut scrap from the wreck · ' : ''}${this.swarm.some((x) => x.at && !x.gone) ? 'Q: shoot a drone · ' : ''}${this.progress?.tools?.turret && this.onMoon() ? 'Y: set your turret down here · ' : ''}${this.progress?.tools?.scanner ? 'K: scan for ore · ' : ''}${this.nearShipHint()}${this.farmHint()}${(this.carried?.food ?? 0) >= 1 && hungry(this.needs) ? 'E: eat · ' : ''}Hold left: ${this.dig().name} · Right: fill · Wheel: brush ${this.brushRadius.toFixed(2)} m · WASD walk · Space jump · Shift run · X thrust pack${this.walker?.thrustPack ? ' (on: Space up, C down)' : ''} · G fly · Esc: release`
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
    // V13: buildings' blocks are solid to the walker as the ground is
    const ships = this.shipLayer
    return { density: (x, y, z) => Math.min(w.densityAt(x, y, z), ships.densityAt({ x, y, z })), gravity: (x, y, z, out) => w.generator.gravity(x, y, z, out) }
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
      this.brushRadius = Math.min(this.brushRadius, digLimits(this.progress?.tools).maxBrush)
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
      const iron = c.iron ?? 0
      const total = c.dust + c.rock + ore + iron
      lines.push(`Pack ${total.toFixed(2)} / ${c.capacity.toFixed(1)} m³ · dust ${c.dust.toFixed(2)} · rock ${c.rock.toFixed(2)}${ore > 0 ? ` · ore ${ore.toFixed(2)}` : ''}${iron > 0 ? ` · iron ${iron.toFixed(2)}` : ''}${(c.food ?? 0) > 0 ? ` · food ${c.food}` : ''}`)
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
    this.frameDt = dt
    const input = this.input
    this.stepNeeds(dt)
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
    if (this.mode === 'walk' && this.walker) this.stepFarming()
    if (this.mode === 'walk' && this.walker) this.stepWild(dt)
    if (this.mode === 'walk' && this.walker && input.locked && input.pressed('KeyE')) this.eatRation()
    else this.fly.step(dt, input)
    this.stepShipsOnFoot()
    this.stepWrecks()
    this.stepSurge(dt)
    this.stepScan(dt)
    const wheel = input.takeWheel()
    if (wheel !== 0) {
      this.brushRadius = Math.max(MIN_BRUSH, Math.min(this.maxBrush(), this.brushRadius * (wheel < 0 ? 1.15 : 1 / 1.15)))
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
      if (digging && !filling && this.brushHit && this.mode === 'walk') {
        this.workGround(dt, this.brushHit)
      } else if ((digging || filling) && this.brushHit) {
        this.endWork()
        this.repeat -= dt
        if (input.clicked(0) || input.clicked(2) || this.repeat <= 0) {
          this.repeat = this.mode === 'walk' ? this.dig().repeat : 0.18
          this.sendEdit(filling && !digging ? 'fill' : 'dig', this.brushHit)
        }
      } else {
        this.repeat = 0
        this.endWork()
      }
    } else {
      this.brushHit = null
      this.endWork()
    }
    this.stepChips(dt)
    input.endStep()
  }

  // On foot, the dig button held on a spot works it with the tool in hand:
  // harder ground takes longer; when the bite has had its work, it comes away
  private workGround(dt: number, at: THREE.Vector3): void {
    const { centre, radius } = this.editCentre('dig', at)
    const key = `${spotKey(centre, Math.max(0.3, radius * 0.45))}@${radius}`
    if (this.miningSpot?.key !== key) {
      const has = this.world.contentsOf(centre.x, centre.y, centre.z, radius)
      this.miningSpot = { key, has, centre, radius }
    }
    const spot = this.miningSpot
    const { work, main } = workFor(spot.has)
    const tools = this.progress?.tools
    const done = this.mining.step(dt, key, work, toolPower(tools))
    if (main) {
      const left = (1 - this.mining.fraction) * secondsFor(spot.has, tools)
      const color = main === 'ore' ? '#5fd3ff' : main === 'rock' ? '#c9b48a' : '#e8e2d6'
      this.workRing.style.display = 'block'
      this.workRing.style.background = `conic-gradient(${color} ${Math.round(this.mining.fraction * 360)}deg, rgba(255,255,255,0.18) 0)`
      this.workLabel.style.display = 'block'
      this.workLabel.textContent = `${this.dig().name} · ${main} · ${left.toFixed(1)} s`
      this.chipTick -= dt
      if (this.chipTick <= 0) {
        this.chipTick = 0.12
        this.chip(at, color)
      }
    } else this.hideWork()
    if (done) {
      this.sendEditAt('dig', spot.centre, spot.radius, spot.has)
      this.miningSpot = null
    }
  }

  // A key held on one target for a while: the ring fills, and it is done
  // once; letting go or turning to another target starts over
  private holdKey(code: string, target: string, seconds: number, label: string, color: string): boolean {
    const key = `${code}:${target}`
    if (!this.input.isDown(code)) {
      if (this.actionHold.key.startsWith(code + ':')) {
        this.actionHold.release()
        this.hideWork()
      }
      return false
    }
    const done = this.actionHold.step(this.frameDt, key, seconds, 1)
    this.workRing.style.display = done ? 'none' : 'block'
    this.workRing.style.background = `conic-gradient(${color} ${Math.round(this.actionHold.fraction * 360)}deg, rgba(255,255,255,0.18) 0)`
    this.workLabel.style.display = done ? 'none' : 'block'
    this.workLabel.textContent = `${label} · ${((1 - this.actionHold.fraction) * seconds).toFixed(1)} s`
    if (done) this.actionHold.release()
    return done
  }

  // How long a wreck's plate takes to cut free with the tool in hand
  private salvageSeconds(): number {
    return SALVAGE_WORK / toolPower(this.progress?.tools)
  }

  private endWork(): void {
    if (!this.mining.key && !this.miningSpot) return
    this.mining.release()
    this.miningSpot = null
    this.hideWork()
  }

  private hideWork(): void {
    this.workRing.style.display = 'none'
    this.workLabel.style.display = 'none'
  }

  // A chip of ground flying off the tool
  private chip(at: THREE.Vector3, color: string): void {
    if (this.chipList.length > 40) return
    const mesh = new THREE.Mesh(CHIP_GEOMETRY, new THREE.MeshBasicNodeMaterial({ color }))
    mesh.position.copy(at)
    const up = at.clone().normalize()
    const v = new THREE.Vector3(Math.random() - 0.5, Math.random() - 0.5, Math.random() - 0.5).multiplyScalar(2.5).addScaledVector(up, 2.5)
    this.chips.add(mesh)
    this.chipList.push({ mesh, v, life: 0.6 })
  }

  private stepChips(dt: number): void {
    if (!this.chipList.length) return
    for (const c of this.chipList) {
      c.life -= dt
      c.v.addScaledVector(c.mesh.position.clone().normalize(), -4 * dt)
      c.mesh.position.addScaledVector(c.v, dt)
    }
    for (const c of this.chipList.filter((x) => x.life <= 0)) {
      this.chips.remove(c.mesh)
      ;(c.mesh.material as THREE.Material).dispose()
    }
    this.chipList = this.chipList.filter((x) => x.life > 0)
  }

  // Where a hold or click at a surface point acts, and how wide
  private editCentre(kind: EditKind, at: THREE.Vector3): { centre: THREE.Vector3; radius: number } {
    const g = this.world.generator.gravity(at.x, at.y, at.z, { x: 0, y: 0, z: 0 })
    const n = new THREE.Vector3(-g.x, -g.y, -g.z).normalize()
    const centre = kind === 'dig' ? at.clone().addScaledVector(n, this.brushRadius * 0.35) : at.clone().addScaledVector(n, -this.brushRadius * 0.2)
    return { centre, radius: Number(this.brushRadius.toFixed(2)) }
  }

  // --- Ships (V3) ---

  private nearSeat: string | null = null
  private nearBed: string | null = null
  private nearBench: string | null = null
  private nearForge: string | null = null
  private forgeShown = ''

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

  // --- Wrecks (V15) ---

  // The blocks salvaged from the moon's wrecks, as game.wrecks keeps them
  setWrecks(w: { salvaged: Record<string, boolean> } | null): void {
    this.salvaged = { ...(w?.salvaged ?? {}) }
    this.showSalvaged()
  }

  private showSalvaged(): void {
    for (const w of this.wrecks) w.meshes.forEach((m, i) => (m.visible = !this.salvaged[salvageKey(w.id, i)]))
  }

  // Lays the two derelict skiffs on the moon once the skiff's design is known
  private buildWrecks(): void {
    const d = this.shipLayer.design('skiff')
    if (!d || this.wrecks.length) return
    const g = this.world.generator.params
    const t = this.terrain()
    const rust = [0x6b4a3a, 0x5a4034, 0x7a5a44].map((color) => new THREE.MeshStandardNodeMaterial({ color, roughness: 1, metalness: 0.2 }))
    const box = new THREE.BoxGeometry(d.blockSize * 0.96, d.blockSize * 0.96, d.blockSize * 0.96)
    // V18: the cockpit block holding the manual shows a faint light through its seams
    const lit = new THREE.MeshStandardNodeMaterial({ color: 0x6b4a3a, emissive: 0x3a5a2a, roughness: 1, metalness: 0.2 })
    for (const site of wreckSites(g.seed)) {
      const top = { x: site.dir.x * (g.radius + g.amplitude + 15), y: site.dir.y * (g.radius + g.amplitude + 15), z: site.dir.z * (g.radius + g.amplitude + 15) }
      const ground = surfaceBelow(t, top, 80)
      if (!ground) continue
      const up = new THREE.Vector3(site.dir.x, site.dir.y, site.dir.z)
      const group = new THREE.Group()
      group.name = `wreck:${site.id}`
      group.quaternion
        .setFromUnitVectors(new THREE.Vector3(0, 1, 0), up)
        .multiply(new THREE.Quaternion().setFromAxisAngle(new THREE.Vector3(0, 1, 0), site.spin))
        .multiply(new THREE.Quaternion().setFromAxisAngle(new THREE.Vector3(1, 0, 0), site.tilt))
      group.position.set(ground.x, ground.y, ground.z).addScaledVector(up, 0.4)
      const meshes = d.blocks.map((b, i) => {
        const m = new THREE.Mesh(box, i === MANUAL_BLOCK ? lit : rust[(i * 7) % 3])
        const j = ((i * 37) % 11) / 11 - 0.5
        m.position.set(b.at[0] * d.blockSize + j * 0.12, b.at[1] * d.blockSize - Math.abs(j) * 0.2, b.at[2] * d.blockSize + j * 0.08)
        m.rotation.set(j * 0.3, j * 0.5, -j * 0.2)
        group.add(m)
        return m
      })
      this.wreckGroup.add(group)
      group.updateMatrixWorld(true)
      const centres = meshes.map((m) => {
        const c = m.getWorldPosition(new THREE.Vector3())
        return { x: c.x, y: c.y, z: c.z }
      })
      this.wrecks.push({ id: site.id, meshes, centres })
    }
    this.showSalvaged()
  }

  // On foot by a wreck: Z tears off the nearest block still there
  private stepWrecks(): void {
    this.buildWrecks()
    const w = this.walker
    const near = this.mode === 'walk' && w ? nearestSalvage(this.wrecks.map((x) => ({ id: x.id, blocks: x.centres })), this.salvaged, w.pos) : null
    if (near?.wreck !== this.nearWreck?.wreck || near?.index !== this.nearWreck?.index) {
      this.nearWreck = near ? { wreck: near.wreck, index: near.index } : null
      this.updateHint()
    }
    if (near && w && this.input.locked && this.holdKey('KeyZ', `wreck:${near.wreck}.${near.index}`, this.salvageSeconds(), 'cutting a plate free', '#c98a5a')) this.salvage(near.wreck, near.index)
  }

  salvage(wreck: string, index: number): void {
    const c = this.carried
    const held = (c?.dust ?? 0) + (c?.rock ?? 0) + (c?.ore ?? 0)
    if (held + SALVAGE_ORE > (c?.capacity ?? 4)) {
      this.flashHint('Your pack is full: load it into a ship\'s hold')
      return
    }
    const p = this.walker?.pos ?? { x: 0, y: 0, z: 0 }
    this.salvaged = { ...this.salvaged, [salvageKey(wreck, index)]: true }
    this.showSalvaged()
    if (c) this.carried = { ...c, ore: Math.round(((c.ore ?? 0) + SALVAGE_ORE) * 1000) / 1000 }
    this.drawPack()
    this.opts.onShipIntent?.({ kind: 'salvage', wreck, index, x: round2(p.x), y: round2(p.y), z: round2(p.z) })
    if (index === MANUAL_BLOCK) {
      // V18: the cockpit block's manual
      if (this.progress) this.progress = { ...this.progress, manuals: { ...this.progress.manuals, powerDrill: true } }
      this.flashHint('Behind the cockpit plate, a waterlogged binder: a fabricator\'s manual for a powered drill. A cabin\'s bench can build it now')
      this.updateHint()
      return
    }
    this.flashHint(`You wrench a plate off the wreck: ${SALVAGE_ORE} m³ of scrap for the refinery, counted as ore`)
  }

  // --- The ore scanner (V21) ---

  // K with a scanner sends a pulse; its marks fade after SCAN_SHOW s
  private stepScan(dt: number): void {
    this.scanCool = Math.max(0, this.scanCool - dt)
    if (this.scanLeft > 0) {
      this.scanLeft -= dt
      const k = Math.max(0, this.scanLeft / SCAN_SHOW)
      for (const m of this.scanGroup.children) {
        m.rotation.y += dt * 1.5
        const mat = (m as THREE.Mesh).material as THREE.MeshBasicNodeMaterial
        mat.opacity = 0.35 + 0.6 * k
      }
      if (this.scanLeft <= 0) this.clearScan()
    }
    const w = this.walker
    if (!w || this.mode !== 'walk' || !this.progress?.tools?.scanner || !this.input.locked || !this.input.pressed('KeyK')) return
    if (this.scanCool > 0) return this.flashHint(`The scanner is still charging: ${Math.ceil(this.scanCool)} s`)
    this.pulseScan(w.pos)
  }

  private pulseScan(at: { x: number; y: number; z: number }): void {
    this.scanCool = SCAN_COOLDOWN
    const found = scan((x, y, z) => {
      const [d, m] = this.world.sampleAt(x, y, z)
      return d < 0 && m === MATERIAL_ORE
    }, at)
    this.clearScan()
    for (const p of found) {
      const size = 0.5 + Math.min(1.5, p.cells * 0.12)
      const mat = new THREE.MeshBasicNodeMaterial({ color: 0x4fd6ff, transparent: true, opacity: 0.95, depthTest: false, wireframe: true })
      const m = new THREE.Mesh(new THREE.OctahedronGeometry(size, 0), mat)
      m.renderOrder = 10
      m.position.set(p.x, p.y, p.z)
      this.scanGroup.add(m)
    }
    this.scanLeft = found.length ? SCAN_SHOW : 0
    this.flashHint(scanLine(found))
  }

  private clearScan(): void {
    for (const m of [...this.scanGroup.children]) {
      const mesh = m as THREE.Mesh
      mesh.geometry.dispose()
      ;(mesh.material as THREE.Material).dispose()
      this.scanGroup.remove(m)
    }
  }

  // --- Swarm surges (V16) ---

  // Each frame: the countdown on the moon, and while a surge is on its
  // drones coming down at the player in the open; Q shoots
  private stepSurge(dt: number): void {
    const w = this.walker
    const onMoon = this.onMoon()
    const st = surgeAt(Date.now() + this.surgeOffset)
    this.surgeTick -= dt
    if (this.surgeTick <= 0) {
      this.surgeTick = 0.5
      this.drawSurgeHud(onMoon, st.active, st.startsIn)
    }
    if (onMoon && w && this.progress?.tools?.turret && this.input.locked && this.input.pressed('KeyY')) this.placeTurret()
    if (!st.active || !onMoon || !w) {
      if (this.swarm.length && !st.active) this.endSurge()
      return
    }
    if (st.index !== this.surgeIndex) this.beginSurge(st.index)
    const me = new THREE.Vector3(w.pos.x, w.pos.y, w.pos.z)
    const up = me.clone().normalize()
    const eye = this.fly.camera.position
    let changed = false
    for (const x of this.swarm) {
      if (x.gone) continue
      if (!x.at) {
        if (st.into < x.d.wakes) continue
        // it wakes: high in the sky on its side, 30 m off
        const side = new THREE.Vector3(Math.cos(x.d.azimuth), 0, Math.sin(x.d.azimuth)).projectOnPlane(up).normalize()
        x.at = me.clone().addScaledVector(up, 30).addScaledVector(side, 30)
        x.mesh.visible = true
        changed = true
        this.flashHint('A drone wakes in the black and drops toward you. Q: shoot it down')
      }
      const to = eye.clone().sub(x.at)
      const d = to.length()
      if (d < 1.4) {
        x.gone = true
        x.mesh.visible = false
        changed = true
        this.needs = { ...this.needs, health: Math.max(0, this.needs.health - DRONE_HIT) }
        this.flashHint(`A drone slams into you: health -${DRONE_HIT}`)
        continue
      }
      x.at.addScaledVector(to, Math.min(1, (x.d.speed * dt) / d))
      x.mesh.position.copy(x.at)
      x.mesh.rotation.y += dt * 3
    }
    if (this.stepTurrets(dt)) changed = true
    if (changed) this.updateHint()
    if (this.input.locked && this.input.pressed('KeyQ')) this.shoot()
  }

  private onMoon(): boolean {
    const w = this.walker
    return this.mode === 'walk' && !!w && Math.hypot(w.pos.x, w.pos.y, w.pos.z) < this.world.generator.params.radius + 60
  }

  // --- Sentry turrets (V17) ---

  // The players' turrets, as game.surges keeps them
  setTurrets(t: Record<string, { x: number; y: number; z: number }> | null | undefined): void {
    const next = t ?? {}
    for (const id of Object.keys(this.turrets)) if (!next[id]) {
      this.turretGroup.remove(this.turrets[id].mesh)
      delete this.turrets[id]
    }
    for (const [id, p] of Object.entries(next)) this.showTurret(id, new THREE.Vector3(p.x, p.y, p.z))
  }

  private showTurret(id: string, at: THREE.Vector3): void {
    const old = this.turrets[id]
    if (old) {
      if (old.at.distanceTo(at) < 0.01) return
      this.turretGroup.remove(old.mesh)
    }
    const up = at.clone().normalize()
    const g = new THREE.Group()
    g.name = `turret:${id}`
    const metal = new THREE.MeshStandardNodeMaterial({ color: id === this.myUserId ? 0x5f7f9a : 0x7a7f86, roughness: 0.5, metalness: 0.6 })
    const base = new THREE.Mesh(new THREE.CylinderGeometry(0.55, 0.7, 0.6, 10), metal)
    base.position.y = 0.3
    const head = new THREE.Group()
    head.position.y = 0.95
    const dome = new THREE.Mesh(new THREE.SphereGeometry(0.4, 12, 8), metal)
    const barrel = new THREE.Mesh(new THREE.CylinderGeometry(0.08, 0.08, 1.1, 8), new THREE.MeshStandardNodeMaterial({ color: 0x2a2f36, roughness: 0.4 }))
    barrel.rotation.x = Math.PI / 2
    barrel.position.z = 0.55
    const eye = new THREE.Mesh(new THREE.SphereGeometry(0.09, 8, 6), new THREE.MeshStandardNodeMaterial({ color: 0x103040, emissive: 0x4fd6ff }))
    eye.position.set(0, 0.18, 0.32)
    head.add(dome, barrel, eye)
    g.add(base, head)
    g.position.copy(at)
    g.quaternion.setFromUnitVectors(new THREE.Vector3(0, 1, 0), up)
    this.turretGroup.add(g)
    this.turrets[id] = { at, mesh: g, head, reload: old?.reload ?? 0 }
  }

  // Y on the moon: your turret stands where you are (moved if it stood elsewhere)
  placeTurret(): void {
    const w = this.walker
    if (!w) return
    const at = new THREE.Vector3(w.pos.x, w.pos.y, w.pos.z)
    const up = at.clone().normalize()
    const look = w.look()
    const fwd = new THREE.Vector3(look.x, look.y, look.z).projectOnPlane(up)
    if (fwd.lengthSq() > 1e-6) at.addScaledVector(fwd.normalize(), 3)
    this.showTurret(this.myUserId || 'me', at)
    this.opts.onShipIntent?.({ kind: 'placeTurret', x: round2(at.x), y: round2(at.y), z: round2(at.z) })
    this.flashHint(`Your sentry turret unfolds and starts to scan: it shoots drones within ${TURRET_RANGE} m`)
  }

  // While a surge is on, each turret turns to the nearest drone in reach
  // and fires every TURRET_RELOAD seconds; a drone it downs is yours
  private stepTurrets(dt: number): boolean {
    let changed = false
    for (const t of Object.values(this.turrets)) {
      t.reload = Math.max(0, t.reload - dt)
      const pick = turretPick(t.at, this.swarm.map((x) => ({ id: x.d.id, at: x.at, gone: x.gone })))
      const x = pick === null ? null : this.swarm.find((s) => s.d.id === pick)
      if (!x?.at) continue
      t.head.lookAt(x.at)
      if (t.reload > 0) continue
      t.reload = TURRET_RELOAD
      const from = t.head.getWorldPosition(new THREE.Vector3())
      const bolt = new THREE.Line(new THREE.BufferGeometry().setFromPoints([from, x.at.clone()]), new THREE.LineBasicNodeMaterial({ color: 0xffd36b }))
      this.surgeGroup.add(bolt)
      setTimeout(() => this.surgeGroup.remove(bolt), 140)
      this.downed(x, 'Your turret barks: a drone tumbles out of the sky, and its scrap is yours')
      changed = true
    }
    return changed
  }

  private beginSurge(index: number): void {
    this.endSurge()
    this.surgeIndex = index
    const geo = new THREE.OctahedronGeometry(0.55)
    const mat = new THREE.MeshStandardNodeMaterial({ color: 0x8a1c1c, emissive: 0xff3b2f, roughness: 0.4 })
    this.swarm = dronesOf(index).map((d) => {
      const mesh = new THREE.Mesh(geo, mat)
      mesh.visible = false
      this.surgeGroup.add(mesh)
      return { d, mesh, at: null, gone: false }
    })
  }

  private endSurge(): void {
    for (const x of this.swarm) this.surgeGroup.remove(x.mesh)
    this.swarm = []
    this.updateHint()
  }

  // Q: a bolt along the look; the nearest drone close to its line falls
  shoot(): boolean {
    const w = this.walker
    if (!w) return false
    const eye = this.fly.camera.position
    const look = w.look()
    let best: { x: { d: Drone; mesh: THREE.Mesh; at: THREE.Vector3 | null; gone: boolean }; along: number } | null = null
    for (const x of this.swarm) {
      if (!x.at || x.gone) continue
      const along = shotHits(eye, look, x.at)
      if (along !== null && (!best || along < best.along)) best = { x, along }
    }
    const end = new THREE.Vector3(eye.x, eye.y, eye.z).addScaledVector(new THREE.Vector3(look.x, look.y, look.z), best?.along ?? 30)
    const bolt = new THREE.Line(new THREE.BufferGeometry().setFromPoints([new THREE.Vector3(eye.x, eye.y - 0.3, eye.z), end]), new THREE.LineBasicNodeMaterial({ color: 0x9fe8ff }))
    this.surgeGroup.add(bolt)
    setTimeout(() => this.surgeGroup.remove(bolt), 120)
    if (!best) return false
    this.downed(best.x, `Down it goes in a spray of sparks: ${DRONE_SCRAP} m³ of scrap`)
    return true
  }

  private downed(x: { d: Drone; mesh: THREE.Mesh; gone: boolean }, line: string): void {
    x.gone = true
    x.mesh.visible = false
    const c = this.carried
    if (c && c.dust + c.rock + (c.ore ?? 0) + DRONE_SCRAP <= c.capacity) this.carried = { ...c, ore: Math.round(((c.ore ?? 0) + DRONE_SCRAP) * 1000) / 1000 }
    this.drawPack()
    this.opts.onShipIntent?.({ kind: 'downDrone', surge: this.surgeIndex, drone: x.d.id })
    this.flashHint(line)
  }

  private drawSurgeHud(onMoon: boolean, active: boolean, startsIn: number): void {
    if (!this.surgeHud) {
      this.surgeHud = document.createElement('div')
      this.surgeHud.dataset.surge = ''
      this.surgeHud.style.cssText =
        'position:absolute;top:10px;left:50%;transform:translateX(-50%);padding:3px 12px;border-radius:12px;z-index:2;font:12px system-ui,sans-serif;pointer-events:none'
      this.container.appendChild(this.surgeHud)
    }
    const el = this.surgeHud
    el.style.display = onMoon ? 'block' : 'none'
    if (!onMoon) return
    const left = this.swarm.filter((x) => !x.gone).length
    el.textContent = active ? `SWARM SURGE · ${left} drone${left === 1 ? '' : 's'} left` : `Next swarm surge in ${countdown(startsIn)}`
    el.style.background = active ? 'rgba(160,24,20,0.85)' : 'rgba(0,0,0,0.55)'
    el.style.color = active ? '#fff' : startsIn < 60_000 ? '#ffb74d' : '#ddd'
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
    // V13: F by a cabin's bed sleeps in it
    const bed = this.walker && !seat ? this.shipLayer.bedNear(this.walker.pos) : null
    if (bed !== this.nearBed) {
      this.nearBed = bed
      this.updateHint()
    }
    if (bed && input.locked && input.pressed('KeyF')) this.sleepIn(bed)
    // V14: U at a cabin's field bench crafts
    // V20: U at a cabin's forge smelts ore, then takes the iron; nearer than the bench, it wins U
    const forge = this.walker && !seat ? this.shipLayer.forgeNear(this.walker.pos, 1.7) : null
    const line = forge ? this.forgeHint() : ''
    if (forge !== this.nearForge || line !== this.forgeShown) {
      this.nearForge = forge
      this.forgeShown = line
      this.updateHint()
    }
    if (forge && input.locked && input.pressed('KeyU')) this.forgeAt(forge)
    const bench = this.walker && !seat && !forge ? this.shipLayer.benchNear(this.walker.pos) : null
    if (bench !== this.nearBench) {
      this.nearBench = bench
      this.updateHint()
    }
    if (bench && input.locked && input.pressed('KeyU')) this.craftAt(bench)
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
    if (this.progress.needs) this.needs = { ...this.progress.needs }
    this.savedNeeds = this.progress.needs ? { ...this.progress.needs } : null
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

  // --- The body's needs (V10) ---

  // Air fills in a planet's air, a ship's cockpit or crew seat, and the outpost's dome
  private canBreathe(): boolean {
    if (this.shipLayer.pilotOf || this.riding || this.airDepth > 0.3) return true
    if (!this.outpostPlaced || !this.walker) return false
    const g = this.outpost.giver()
    const p = this.walker.pos
    return Math.hypot(p.x - g.x, p.y - g.y, p.z - g.z) < 14
  }

  private stepNeeds(dt: number): void {
    const playing = !!this.walker && this.placed && !this.pod.falling && (this.mode === 'walk' || !!this.shipLayer.pilotOf || !!this.riding)
    if (!playing) {
      this.needsHud.show(null, { breathable: true })
      return
    }
    const before = this.breathable
    this.breathable = this.canBreathe()
    const rates = ratesFor(this.progress?.tools)
    this.needs = stepNeeds(this.needs, { breathable: this.breathable }, dt, rates)
    if (before !== this.breathable) this.updateHint()
    this.needsHud.show(this.needs, { breathable: this.breathable }, rates)
    if (this.needs.health <= 0) this.blackOut()
  }

  // V14: how digging goes on foot (bare hands, or the drill once crafted);
  // flying the camera digs as before
  private dig(): DigLimits {
    return digLimits(this.progress?.tools)
  }

  private maxBrush(): number {
    return this.mode === 'walk' ? Math.min(MAX_BRUSH, this.dig().maxBrush) : MAX_BRUSH
  }

  private benchHint(): string {
    if (!this.nearBench) return ''
    const r = nextRecipe(this.progress?.tools, this.progress?.manuals)
    return !r ? (this.progress?.manuals?.powerDrill ? 'The bench: everything it knows is made · ' : 'The bench: everything it knows from iron is made; a wreck may hold a manual for more · ') : `U: craft ${named(r)} (${Object.entries(r.needs).map(([k, n]) => `${n} m³ ${k}`).join(', ')}) · `
  }

  private forgeOf(id: string): ForgeState | null {
    return this.shipLayer.ships().find((s) => s.id === id)?.forge ?? null
  }

  private forgeHint(): string {
    if (!this.nearForge) return ''
    const a = forgeAction(this.forgeOf(this.nearForge), this.carried, Date.now())
    if (a.kind === 'smelt') return `U: put ${a.ore.toFixed(2)} m³ of ore in the forge · `
    if (a.kind === 'wait') return `The forge is smelting: ${a.seconds} s left · `
    if (a.kind === 'take') return `U: take ${a.iron.toFixed(2)} m³ of iron from the forge · `
    return `The forge: ${a.why} · `
  }

  // V20: U at the forge puts ore in, or takes the iron once it is smelted
  forgeAt(id: string): void {
    const a = forgeAction(this.forgeOf(id), this.carried, Date.now())
    if (a.kind === 'smelt') {
      if (this.carried) this.carried = { ...this.carried, ore: (this.carried.ore ?? 0) - a.ore }
      this.opts.onShipIntent?.({ kind: 'smelt', id })
      this.flashHint(`You shovel ${a.ore.toFixed(2)} m³ of ore into the forge and work the bellows. It will take ${Math.round((a.ore * SMELT_MS) / 1000)} s`)
    } else if (a.kind === 'take') {
      if (this.carried) this.carried = { ...this.carried, iron: (this.carried.iron ?? 0) + a.iron }
      this.opts.onShipIntent?.({ kind: 'takeIngots', id })
      this.flashHint(`You rake ${a.iron.toFixed(2)} m³ of iron ingots out of the forge, still warm`)
    } else if (a.kind === 'wait') this.flashHint(`The ore is still glowing: ${a.seconds} s left`)
    else this.flashHint(a.why[0].toUpperCase() + a.why.slice(1))
    this.drawPack()
    this.updateHint()
  }

  // V14: U at the bench makes the drill when the pack holds what it takes
  craftAt(id: string): void {
    const r = nextRecipe(this.progress?.tools, this.progress?.manuals)
    if (!r) {
      this.flashHint('The bench has nothing more to teach you yet')
      return
    }
    const why = missing(r, this.carried, this.progress?.tools)
    if (why) {
      this.flashHint(why[0].toUpperCase() + why.slice(1))
      return
    }
    if (this.carried) this.carried = { ...this.carried, ...spend(r, this.carried) }
    this.drawPack()
    if (this.progress) this.progress = { ...this.progress, tools: { ...this.progress.tools, [r.id]: true } }
    this.opts.onShipIntent?.({ kind: 'craft', id, item: r.id })
    if (r.id === 'powerDrill') {
      this.brushRadius = Math.max(this.brushRadius, 3)
      this.flashHint(`Following the manual's diagrams, you fit a motor to your drill: ${named(r)}. It ${r.line}`)
    } else if (r.id === 'drill') {
      this.brushRadius = Math.max(this.brushRadius, 2)
      this.flashHint(`You file and fit the parts at the bench: ${named(r)}. It ${r.line}; the wheel opens its bite to ${this.dig().maxBrush} m`)
    } else this.flashHint(`You bolt the parts together at the bench: ${named(r)}. It ${r.line}`)
    this.updateHint()
  }

  // V13: a night in a cabin's bed: full health, and the cabin is home
  sleepIn(id: string): void {
    this.needs = { ...this.needs, health: 100 }
    this.opts.onShipIntent?.({ kind: 'sleep', id })
    this.saveStand(true)
    this.flashHint('You sleep a while in the cabin. You wake rested; this cabin is home now')
  }

  // E: one ration from the pack, when there is one and it would help
  private eatRation(): void {
    if ((this.carried?.food ?? 0) < 1) {
      this.flashHint('No food in your pack: harvest barley on the planet')
      return
    }
    if (!hungry(this.needs)) {
      this.flashHint('You are not hungry')
      return
    }
    this.needs = eat(this.needs)
    if (this.carried) this.carried = { ...this.carried, food: (this.carried.food ?? 0) - 1 }
    this.drawPack()
    this.opts.onShipIntent?.({ kind: 'eat' })
    this.flashHint('You eat a ration of barley bread')
  }

  private flashHint(text: string): void {
    this.hint.textContent = text
    setTimeout(() => this.updateHint(), 2500)
  }

  // Health gone: wake at the outpost, its medics having brought you round
  private blackOut(): void {
    this.blackouts++
    if (this.shipLayer.pilotOf) this.leaveSeat()
    if (this.riding) this.opts.onShipIntent?.({ kind: 'crew', id: this.riding, aboard: false })
    const w = this.walker
    this.needs = { air: 100, fed: Math.max(this.needs.fed, 40), health: 60 }
    // V13: home is the cabin whose bed they last slept in, while it stands
    const home = this.progress?.home ? this.shipLayer.bedPoint(this.progress.home) : null
    if (w && home) {
      w.pos = home
      w.vel = { x: 0, y: 0, z: 0 }
    } else if (w && this.outpostPlaced) {
      const g = this.outpost.giver()
      w.pos = { x: g.x + 3, y: g.y + 0.5, z: g.z + 3 }
      w.vel = { x: 0, y: 0, z: 0 }
    }
    this.saveStand(true)
    const fade = document.createElement('div')
    fade.dataset.blackout = ''
    fade.style.cssText = 'position:absolute;inset:0;background:#000;opacity:1;transition:opacity 2.5s;z-index:3;display:flex;align-items:center;justify-content:center;font:16px Georgia,serif;color:#ddd'
    fade.textContent = home ? 'You black out, and come round in your own bed.' : 'You black out. The outpost\'s medic brings you round.'
    this.container.appendChild(fade)
    setTimeout(() => (fade.style.opacity = '0'), 1200)
    setTimeout(() => fade.remove(), 4000)
  }

  // Saves where the player stands now and then, when they have moved
  private saveStand(force = false): void {
    const w = this.walker
    if (!w || this.pod.falling) return
    const s = this.savedAt
    const n = rounded(this.needs)
    const sn = this.savedNeeds
    const needsMoved = !sn || Math.abs(sn.air - n.air) + Math.abs(sn.fed - n.fed) + Math.abs(sn.health - n.health) >= 5
    if (!force && !needsMoved && s && Math.hypot(w.pos.x - s.x, w.pos.y - s.y, w.pos.z - s.z) < 3) return
    if (!force && !w.grounded) return
    const r = (v: number) => Math.round(v * 100) / 100
    this.savedAt = { ...w.pos }
    this.savedNeeds = n
    this.opts.onShipIntent?.({ kind: 'stand', x: r(w.pos.x), y: r(w.pos.y), z: r(w.pos.z), needs: n })
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
    // a player who has only stood or slept somewhere has no contracts, xp or levels yet
    if (p) p = { ...p, done: p.done ?? {}, xp: p.xp ?? {}, levels: p.levels ?? {} }
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
    this.brushRadius = Math.min(this.brushRadius, this.maxBrush())
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
    // click carves a bowl rather than a buried bubble (up being away from
    // the pull of the body it is on)
    const { centre, radius } = this.editCentre(kind, at)
    this.sendEditAt(kind, centre, radius, this.world.contentsOf(centre.x, centre.y, centre.z, radius))
  }

  private sendEditAt(kind: EditKind, c: THREE.Vector3, radius: number, has: Contents & { empty: number }): void {
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
      this.growCrops()
    }
    this.stepSky()
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
      crops: this.crops.size,
      air: Math.round(this.airDepth * 100) / 100,
      needs: this.placed && this.walker ? rounded(this.needs) : null,
      breathable: this.breathable,
      blackouts: this.blackouts,
      grazers: this.herd.filter((h) => h.model.visible).length,
      fishing: this.fishing.state,
    }
  }

  dispose(): void {
    if (this.mode === 'walk' && !this.shipLayer.pilotOf && !this.riding) this.saveStand()
    this.loop.stop()
    this.surgeHud?.remove()
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
    this.workRing.remove()
    this.workLabel.remove()
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

// A barley plant: a few stalks with heads, green until ripe, then gold
function cropModel(): THREE.Group {
  const g = new THREE.Group()
  const mat = new THREE.MeshStandardNodeMaterial({ color: 0x6f9a3c, roughness: 0.9 })
  for (let i = 0; i < 5; i++) {
    const a = (i / 5) * Math.PI * 2
    const stalk = new THREE.Mesh(new THREE.CylinderGeometry(0.02, 0.03, 0.9, 5), mat)
    stalk.position.set(Math.cos(a) * 0.12, 0.45, Math.sin(a) * 0.12)
    stalk.rotation.set(Math.sin(a) * 0.12, 0, Math.cos(a) * 0.12)
    const head = new THREE.Mesh(new THREE.CylinderGeometry(0.04, 0.03, 0.22, 5), mat)
    head.position.set(Math.cos(a) * 0.17, 0.98, Math.sin(a) * 0.17)
    g.add(stalk, head)
  }
  return g
}

function round2(v: number): number {
  return Math.round(v * 100) / 100
}
