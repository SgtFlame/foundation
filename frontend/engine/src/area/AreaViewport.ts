import * as THREE from 'three/webgpu'
import { OrbitCamera } from '../camera/OrbitCamera.ts'
import { Loop } from '../core/Loop.ts'
import { StatsOverlay } from '../debug/StatsOverlay.ts'
import { Input } from '../input/Input.ts'
import { Renderer } from '../render/Renderer.ts'
import { WalkGrid, type Point } from '../nav/WalkGrid.ts'
import { moonLight, moons, type MoonPhase } from '../sky/Moons.ts'
import { DialoguePanel } from './DialoguePanel.ts'
import { buildModel, CharacterModel, interiorShell, material } from './Models.ts'
import { fit, pieceOf, turned, type AreaDef, type Dialogue, type Npc, type PieceSet, type Placement } from './PieceSet.ts'

// What the editor asks for; the host sends it to the server, which decides
export type AreaIntent =
  | { kind: 'place'; piece: string; x: number; z: number; rot: number }
  | { kind: 'move'; id: string; x?: number; z?: number; rot?: number }
  | { kind: 'remove'; id: string }
  // people (S3), in metres
  | { kind: 'placeNpc'; npcKind: string; x: number; z: number }
  | { kind: 'moveNpc'; id: string; x: number; z: number }
  | { kind: 'removeNpc'; id: string }

// Where this viewport's person is: the cell under the pointer, the piece
// in hand and its turn, the selected placement and the camera
export interface AreaCursor {
  x: number | null
  z: number | null
  piece?: string
  rot?: number
  selected?: string
  camera: { x: number; z: number; yaw: number; pitch: number; distance: number }
}

export interface RemoteCursor extends AreaCursor {
  id: string
  name: string
  color: string
}

export type Lighting = 'day' | 'night'

export interface AreaViewportOptions {
  set: PieceSet
  area: AreaDef
  onIntent?: (i: AreaIntent) => void
  // A placement refused before it was sent, with why
  onRefused?: (message: string) => void
  // At most 10 times a second while the cursor or camera moves
  onCursor?: (c: AreaCursor) => void
  // The piece in hand or the selection changed (Escape, a click)
  onToolChange?: (t: { piece: string | null; rot: number; selected: string | null }) => void
  forceWebGL?: boolean
  stats?: boolean
  lighting?: Lighting
  // The calendar night, which sets the moons' phases (default 11)
  night?: number
  // Play (S3): the character walked through a linked door; the host
  // fetches the area it leads to and gives it back with travel()
  onTravel?: (area: string) => void
  // A person was selected in the editor (null: none)
  onNpcSelect?: (id: string | null) => void
}

export interface AreaStats {
  backend: string
  fps: number
  frameMs: number
  worstMs: number
  drawCalls: number
  triangles: number
  pieces: number
  lights: number
  heapMB: number | null
  // Playing: the character on the area (S2)
  playing: boolean
  // How long the walk grid took to build from the pieces, last time
  navMs: number
  // S3: the area shown (another while play has gone through a door), the people in it and the talk
  areaShown: string
  npcs: number
  talking: { dialogue: string; node: string } | null
}

const GOOD = 0x4caf50
const BAD = 0xe53935
const SELECT = 0xffd54f
const EDGE_COLORS: Record<string, number> = { road: 0xff9800, grass: 0x8bc34a }

// A viewport onto a story game area (forge 375 S1): the renderer, the
// loop, a top-down orbit camera, the grid and the placed pieces, a piece
// in hand with its footprint preview (green when it fits, red when it does
// not, with its edges marked), selection, others' cursors, and day or
// night lighting. It knows nothing of React or the live connection: the
// host sends its intents as commands and gives it the area back.
export class AreaViewport {
  readonly orbit = new OrbitCamera()
  readonly scene = new THREE.Scene()
  private readonly loop: Loop
  private readonly input: Input
  private readonly overlay: StatsOverlay | null
  private readonly container: HTMLElement
  private readonly renderer: Renderer
  private readonly opts: AreaViewportOptions
  private set: PieceSet
  private area: AreaDef
  private readonly pieces = new Map<string, { key: string; group: THREE.Group }>()
  private readonly piecesGroup = new THREE.Group()
  private readonly grid = new THREE.Group()
  private readonly ground: THREE.Mesh
  private readonly preview = new THREE.Group()
  private readonly selection: THREE.LineSegments
  private readonly remotes = new Map<string, { group: THREE.Group; label: HTMLDivElement; key: string }>()
  private readonly hint: HTMLDivElement
  private readonly raycaster = new THREE.Raycaster()
  private readonly plane = new THREE.Plane(new THREE.Vector3(0, 1, 0), 0)
  private tool: string | null = null
  private rot = 0
  private selected: string | null = null
  private hover: { x: number; z: number } | null = null
  private previewKey = ''
  private cursorTick = 0
  private lastCursor = ''
  private lighting: Lighting
  private readonly lightsGroup = new THREE.Group()
  private readonly sun = new THREE.DirectionalLight(0xfff1dc, 2.4)
  private readonly hemi = new THREE.HemisphereLight(0xbcd2ff, 0x3a3020, 0.7)
  private readonly greaterMoon = new THREE.DirectionalLight(0xe8e2d4, 0.0)
  private readonly lesserMoon = new THREE.DirectionalLight(0xb8c8ff, 0.0)
  private pointLights = 0
  // the last frame's draw calls and triangles (the renderer resets them each frame)
  private lastInfo = { drawCalls: 0, triangles: 0 }
  private night: number
  private moonHud: HTMLDivElement | null = null
  // Play: the character, where it may walk and the path it follows
  private playing = false
  private walk: WalkGrid | null = null
  private navMs = 0
  private character: CharacterModel | null = null
  private path: Point[] = []
  private stride = 0
  private moving = 0
  private readonly marker: THREE.Mesh
  private editCamera: { target: THREE.Vector3; yaw: number; pitch: number; distance: number } | null = null
  // S3: an interior's floor and walls, the people, the talk, and the area
  // being edited while play has gone through a door into another
  private readonly shell = new THREE.Group()
  private readonly npcs = new Map<string, { key: string; model: CharacterModel; label: HTMLDivElement }>()
  private npcTool: string | null = null
  private selectedNpc: string | null = null
  private dialogues: Record<string, Dialogue> = {}
  private readonly talk: DialoguePanel
  private arrival: { kind: 'talk'; id: string } | { kind: 'travel'; area: string } | null = null
  private home: { area: AreaDef; set: PieceSet } | null = null

  private constructor(container: HTMLElement, renderer: Renderer, opts: AreaViewportOptions) {
    this.container = container
    this.renderer = renderer
    this.opts = opts
    this.set = opts.set
    this.area = opts.area
    this.lighting = opts.lighting ?? 'day'
    this.night = opts.night ?? 11
    const canvas = renderer.canvas
    canvas.tabIndex = 0
    renderer.three.shadowMap.enabled = true
    renderer.three.shadowMap.type = THREE.PCFShadowMap
    this.input = new Input(canvas)
    renderer.track(this.orbit.camera)

    this.ground = new THREE.Mesh(new THREE.PlaneGeometry(1, 1), material(0x2c3424))
    this.ground.rotation.x = -Math.PI / 2
    this.ground.position.y = -0.32
    this.ground.receiveShadow = true
    this.scene.add(this.ground, this.grid, this.piecesGroup, this.preview, this.lightsGroup, this.shell)
    for (const l of [this.sun, this.greaterMoon, this.lesserMoon]) {
      l.castShadow = true
      l.shadow.mapSize.set(2048, 2048)
      l.shadow.bias = -0.0005
      this.scene.add(l, l.target)
    }
    this.scene.add(this.hemi)

    this.selection = new THREE.LineSegments(new THREE.BufferGeometry(), new THREE.LineBasicNodeMaterial({ color: SELECT }))
    this.selection.visible = false
    this.scene.add(this.selection)
    this.marker = new THREE.Mesh(new THREE.RingGeometry(0.35, 0.55, 24).rotateX(-Math.PI / 2), new THREE.MeshBasicNodeMaterial({ color: 0xffd54f, transparent: true, opacity: 0.9 }))
    this.marker.visible = false
    this.scene.add(this.marker)

    this.overlay = opts.stats === false ? null : new StatsOverlay(container)
    this.hint = document.createElement('div')
    this.hint.style.cssText =
      'position:absolute;bottom:10px;left:50%;transform:translateX(-50%);padding:4px 10px;border-radius:4px;z-index:2;' +
      'font:13px system-ui,sans-serif;color:#eee;background:rgba(0,0,0,0.55);pointer-events:none;white-space:nowrap'
    container.appendChild(this.hint)
    this.talk = new DialoguePanel(container, () => this.updateHint())

    canvas.addEventListener('pointerdown', this.onPointerDown)
    canvas.addEventListener('keydown', this.onKey)
    this.layout(true)
    this.syncPieces()
    this.syncNpcs()
    this.applyLighting()
    this.updateHint()

    this.loop = new Loop({ step: (dt) => this.step(dt), render: () => this.render() })
    this.loop.start()
  }

  static async create(container: HTMLElement, opts: AreaViewportOptions): Promise<AreaViewport> {
    if (getComputedStyle(container).position === 'static') container.style.position = 'relative'
    const canvas = document.createElement('canvas')
    canvas.style.cssText = 'width:100%;height:100%;display:block;outline:none'
    container.appendChild(canvas)
    const renderer = await Renderer.create(canvas, { forceWebGL: opts.forceWebGL })
    return new AreaViewport(container, renderer, opts)
  }

  // The area as the server has it now
  setArea(area: AreaDef): void {
    if (this.home) {
      // play has gone through a door: the edited area waits until play stops
      if (area.id === this.home.area.id) this.home.area = area
      return
    }
    const resized = area.width !== this.area.width || area.depth !== this.area.depth
    this.area = area
    if (resized) this.layout(false)
    this.syncPieces()
    this.syncNpcs()
    this.previewKey = ''
    if (this.selected && !area.pieces[this.selected]) this.select(null)
    if (this.selectedNpc && !area.npcs?.[this.selectedNpc]) this.selectNpc(null)
  }

  setPieceSet(set: PieceSet): void {
    if (this.home) {
      this.home.set = set
      return
    }
    const relayout = set.cell !== this.set.cell || !!set.interior !== !!this.set.interior
    this.set = set
    if (relayout) this.layout(true)
    for (const p of this.pieces.values()) this.piecesGroup.remove(p.group)
    this.pieces.clear()
    this.syncPieces()
  }

  // The piece in hand (null for none: clicking selects)
  setTool(piece: string | null, rot = this.rot): void {
    if (this.playing && piece) return // no building while playing
    this.tool = piece
    this.rot = ((rot % 4) + 4) % 4
    if (piece) {
      this.select(null)
      this.npcTool = null
    }
    this.previewKey = ''
    this.updateHint()
    this.toolChanged()
  }

  // A person in hand (their kind, e.g. farmer): clicking stands one there (S3)
  setNpcTool(kind: string | null): void {
    if (this.playing && kind) return
    this.npcTool = kind
    if (kind) {
      this.tool = null
      this.select(null)
    }
    this.previewKey = ''
    this.updateHint()
    this.toolChanged()
  }

  // What people say: the game.dialogues query
  setDialogues(d: Record<string, Dialogue>): void {
    this.dialogues = d
  }

  selectNpc(id: string | null): void {
    this.selectedNpc = id
    if (id) this.select(null)
    for (const [nid, n] of this.npcs) n.label.style.outline = nid === id ? '2px solid #ffd54f' : ''
    this.updateHint()
    this.opts.onNpcSelect?.(id)
  }

  // Talks to a person now, as walking up to them does in play
  talkTo(id: string): boolean {
    const n = this.area.npcs?.[id]
    const d = n?.dialogue ? this.dialogues[n.dialogue] : undefined
    if (!n || !d) return false
    this.talk.start(d, n.name)
    this.updateHint()
    return true
  }

  // Answers the talk shown (0 the first answer)
  answer(i: number): void {
    this.talk.choose(i)
  }

  // Play went through a door (onTravel): the area it leads to and its
  // piece set. The character comes in by the door that leads back to the
  // area it left, else at the area's spawn point.
  travel(area: AreaDef, set: PieceSet): void {
    if (!this.playing) return
    const from = this.area.id
    this.home ??= { area: this.area, set: this.set }
    this.talk.end()
    this.area = area
    this.set = set
    for (const p of this.pieces.values()) this.piecesGroup.remove(p.group)
    this.pieces.clear()
    this.layout(true)
    this.syncPieces()
    this.syncNpcs()
    this.buildWalk()
    const at = this.doorTo(from) ?? this.spawnPoint()
    this.path = []
    this.arrival = null
    this.character?.group.position.set(at.x, 0, at.z)
    this.orbit.target.set(at.x, 0, at.z)
    this.orbit.distance = this.set.interior ? 16 : 32
    this.grid.visible = false
  }

  setLighting(l: Lighting): void {
    this.lighting = l
    this.applyLighting()
  }

  // The calendar night: the moons' phases follow from it
  setNight(night: number): void {
    this.night = night
    this.applyLighting()
  }

  get isPlaying(): boolean {
    return this.playing
  }

  // Play in editor (S2.4): a character stands on the area and walks where
  // it is clicked, the camera following; off returns to editing where the
  // camera was
  play(on: boolean): void {
    if (on === this.playing) return
    this.playing = on
    const canvas = this.renderer.canvas
    if (on) {
      this.setTool(null)
      this.select(null)
      this.editCamera = { target: this.orbit.target.clone(), yaw: this.orbit.yaw, pitch: this.orbit.pitch, distance: this.orbit.distance }
      this.buildWalk()
      this.character = new CharacterModel(0x5b6f8f)
      const at = this.spawnPoint()
      this.character.group.position.set(at.x, 0, at.z)
      this.scene.add(this.character.group)
      this.orbit.keyPan = false
      this.orbit.target.set(at.x, 0, at.z)
      this.orbit.distance = this.set.interior ? 16 : 32
      this.setNpcTool(null)
      this.selectNpc(null)
      this.grid.visible = false
    } else {
      if (this.character) this.scene.remove(this.character.group)
      this.character = null
      this.walk = null
      this.path = []
      this.arrival = null
      this.marker.visible = false
      this.talk.end()
      if (this.home) {
        // back to the area being edited
        const { area, set } = this.home
        this.home = null
        this.area = area
        this.set = set
        for (const p of this.pieces.values()) this.piecesGroup.remove(p.group)
        this.pieces.clear()
        this.layout(false)
        this.syncPieces()
        this.syncNpcs()
      }
      this.orbit.keyPan = true
      if (this.editCamera) {
        this.orbit.target.copy(this.editCamera.target)
        this.orbit.yaw = this.editCamera.yaw
        this.orbit.pitch = this.editCamera.pitch
        this.orbit.distance = this.editCamera.distance
      }
      this.grid.visible = true
      canvas.style.cursor = ''
    }
    this.previewKey = ''
    this.updateHint()
  }

  // Where the character is, while playing
  player(): { x: number; z: number; heading: number; moving: boolean } | null {
    const c = this.character
    if (!c) return null
    return { x: c.group.position.x, z: c.group.position.z, heading: c.group.rotation.y, moving: this.path.length > 0 }
  }

  // Walks the character to a point on the ground, as a click does; false
  // when there is no way there
  walkTo(x: number, z: number): boolean {
    if (!this.walk || !this.character) return false
    const from = { x: this.character.group.position.x, z: this.character.group.position.z }
    const path = this.walk.findPath(from, { x, z })
    if (!path) return false
    this.path = path.slice(1)
    const end = path[path.length - 1]
    this.marker.position.set(end.x, 0.08, end.z)
    this.marker.visible = this.path.length > 0
    return true
  }

  // Walks to the walkable cell nearest a point (a person, a door)
  private walkNear(p: Point): boolean {
    if (this.walkTo(p.x, p.z)) return true
    const near = this.walk?.nearestWalkable(p, 6)
    if (!near || !this.walk) return false
    const c = this.walk.centre(near[0], near[1])
    return this.walkTo(c.x, c.z)
  }

  select(id: string | null): void {
    this.selected = id
    this.drawSelection()
    this.updateHint()
    this.toolChanged()
  }

  setRemoteCursors(cursors: readonly RemoteCursor[]): void {
    const seen = new Set<string>()
    for (const c of cursors) {
      seen.add(c.id)
      const key = JSON.stringify([c.x, c.z, c.piece, c.rot, c.selected, c.color, c.name])
      let r = this.remotes.get(c.id)
      if (r && r.key === key) continue
      if (r) this.scene.remove(r.group)
      const group = this.remoteGroup(c)
      this.scene.add(group)
      const label = r?.label ?? document.createElement('div')
      label.textContent = c.name
      label.style.cssText =
        'position:absolute;z-index:2;padding:1px 6px;border-radius:3px;font:12px system-ui,sans-serif;color:#fff;pointer-events:none;' +
        `background:${c.color};transform:translate(-50%,-140%);white-space:nowrap`
      if (!r) this.container.appendChild(label)
      r = { group, label, key }
      this.remotes.set(c.id, r)
    }
    for (const [id, r] of this.remotes) {
      if (seen.has(id)) continue
      this.scene.remove(r.group)
      r.label.remove()
      this.remotes.delete(id)
    }
  }

  cursor(): AreaCursor {
    return { x: this.hover?.x ?? null, z: this.hover?.z ?? null, piece: this.tool ?? undefined, rot: this.rot, selected: this.selected ?? undefined, camera: this.orbit.state() }
  }

  // A PNG data URL of the current view, rendered now
  screenshot(): string {
    this.render()
    return this.renderer.screenshot()
  }

  stats(): AreaStats {
    const l = this.loop.stats()
    const info = this.lastInfo
    const mem = (performance as unknown as { memory?: { usedJSHeapSize: number } }).memory
    return {
      backend: this.renderer.backend,
      fps: l.fps,
      frameMs: l.frameMs,
      worstMs: l.worstMs,
      drawCalls: info.drawCalls,
      triangles: info.triangles,
      pieces: this.pieces.size,
      lights: this.pointLights,
      heapMB: mem ? mem.usedJSHeapSize / 1048576 : null,
      playing: this.playing,
      navMs: this.navMs,
      areaShown: this.area.id,
      npcs: this.npcs.size,
      talking: this.talk.state(),
    }
  }

  dispose(): void {
    this.loop.stop()
    this.renderer.canvas.removeEventListener('pointerdown', this.onPointerDown)
    this.renderer.canvas.removeEventListener('keydown', this.onKey)
    this.input.dispose()
    this.overlay?.dispose()
    for (const r of this.remotes.values()) r.label.remove()
    for (const n of this.npcs.values()) n.label.remove()
    this.talk.dispose()
    this.hint.remove()
    this.moonHud?.remove()
    this.renderer.dispose()
    this.renderer.canvas.remove()
  }

  // The world size and the grid, and on first layout the camera
  private layout(first: boolean): void {
    const c = this.set.cell
    const W = this.area.width * c
    const D = this.area.depth * c
    this.ground.scale.set(W + 400, D + 400, 1)
    this.ground.position.set(W / 2, -0.32, D / 2)
    this.ground.material = material(this.set.interior ? 0x121010 : 0x2c3424)
    this.shell.clear()
    if (this.set.interior) this.shell.add(interiorShell(W, D))
    this.scene.remove(this.grid)
    this.grid.clear()
    const pts: number[] = []
    for (let i = 0; i <= this.area.width; i++) pts.push(i * c, 0.05, 0, i * c, 0.05, D)
    for (let j = 0; j <= this.area.depth; j++) pts.push(0, 0.05, j * c, W, 0.05, j * c)
    const g = new THREE.BufferGeometry()
    g.setAttribute('position', new THREE.Float32BufferAttribute(pts, 3))
    this.grid.add(new THREE.LineSegments(g, new THREE.LineBasicNodeMaterial({ color: 0x000000, transparent: true, opacity: 0.25 })))
    this.scene.add(this.grid)
    // Shadows cover the area
    const r = Math.max(W, D) * 0.75
    for (const l of [this.sun, this.greaterMoon, this.lesserMoon]) {
      const cam = l.shadow.camera as THREE.OrthographicCamera
      cam.left = -r
      cam.right = r
      cam.top = r
      cam.bottom = -r
      cam.near = 1
      cam.far = r * 6
      cam.updateProjectionMatrix()
      l.target.position.set(W / 2, 0, D / 2)
    }
    this.applyLighting()
    if (first) {
      this.orbit.target.set(W / 2, 0, D / 2)
      this.orbit.distance = Math.max(this.set.interior ? 18 : 40, Math.max(W, D) * 1.1)
      this.orbit.maxDistance = Math.max(400, Math.max(W, D) * 3)
      this.orbit.apply()
    }
  }

  private applyLighting(): void {
    const c = this.set.cell
    const cx = (this.area.width * c) / 2
    const cz = (this.area.depth * c) / 2
    const far = Math.max(this.area.width, this.area.depth) * c * 2
    const night = this.lighting === 'night'
    this.sun.intensity = night ? 0 : 2.4
    this.sun.castShadow = !night
    this.sun.position.set(cx + far * 0.5, far, cz + far * 0.3)
    // The greater moon: wide, soft, slightly warm; the lesser: small, cool, hard (375 › art style)
    const m = moons(this.night)
    this.greaterMoon.intensity = night ? 0.55 * moonLight(m.greater) : 0
    this.greaterMoon.castShadow = night
    this.greaterMoon.position.set(cx - far * 0.6, far * 0.8, cz - far * 0.2)
    this.lesserMoon.intensity = night ? 0.4 * moonLight(m.lesser) : 0
    this.lesserMoon.castShadow = night
    this.lesserMoon.position.set(cx + far * 0.3, far * 0.9, cz - far * 0.7)
    this.hemi.color.set(night ? 0x26324d : 0xbcd2ff)
    this.hemi.groundColor.set(night ? 0x0b0c10 : 0x3a3020)
    this.hemi.intensity = night ? 0.35 : 1.1
    this.scene.background = new THREE.Color(night ? 0x070a14 : 0x9fb6cf)
    this.scene.fog = night ? new THREE.Fog(0x0a0e1a, far * 0.35, far * 1.4) : new THREE.Fog(0x9fb6cf, far * 0.6, far * 2.5)
    this.lightsGroup.visible = night || !!this.set.interior // a room's hearth burns by day too
    this.drawMoonHud(night ? m : null)
  }

  // The two moons' phases in a corner at night: the top-down camera never
  // sees the sky, so the moons are shown here
  private drawMoonHud(m: { greater: MoonPhase; lesser: MoonPhase } | null): void {
    if (!m) {
      this.moonHud?.remove()
      this.moonHud = null
      return
    }
    if (!this.moonHud) {
      this.moonHud = document.createElement('div')
      this.moonHud.style.cssText =
        'position:absolute;top:10px;right:10px;z-index:2;display:flex;gap:10px;padding:6px 10px;border-radius:4px;' +
        'background:rgba(0,0,0,0.55);font:11px system-ui,sans-serif;color:#ddd;pointer-events:none'
      this.moonHud.dataset.moons = ''
      this.container.appendChild(this.moonHud)
    }
    this.moonHud.replaceChildren()
    for (const [name, phase, size, tint] of [
      ['Greater moon', m.greater, 34, '#efe6d2'],
      ['Lesser moon', m.lesser, 22, '#c8d6ff'],
    ] as const) {
      const box = document.createElement('div')
      box.style.cssText = 'display:flex;flex-direction:column;align-items:center;justify-content:flex-end;gap:3px'
      const c = document.createElement('canvas')
      c.width = c.height = size * 2
      c.style.cssText = `width:${size}px;height:${size}px`
      drawPhase(c, phase, tint)
      const label = document.createElement('div')
      label.textContent = `${name} · ${phaseName(phase)}`
      box.append(c, label)
      this.moonHud.appendChild(box)
    }
  }

  // Adds, replaces and removes piece groups to match the area
  private syncPieces(): void {
    const c = this.set.cell
    let changed = false
    for (const [id, pl] of Object.entries(this.area.pieces ?? {})) {
      const key = JSON.stringify(pl)
      const have = this.pieces.get(id)
      if (have?.key === key) continue
      if (have) this.piecesGroup.remove(have.group)
      const group = this.placedModel(pl, c)
      if (!group) continue
      group.userData.placement = id
      this.piecesGroup.add(group)
      this.pieces.set(id, { key, group })
      changed = true
    }
    for (const [id, have] of this.pieces) {
      if (this.area.pieces?.[id]) continue
      this.piecesGroup.remove(have.group)
      this.pieces.delete(id)
      changed = true
    }
    if (changed) {
      this.rebuildLights()
      this.drawSelection()
      if (this.playing) this.buildWalk()
    }
  }

  private placedModel(pl: Placement, c: number, seed?: number): THREE.Group | null {
    const p = pieceOf(this.set, pl.piece)
    if (!p) return null
    const model = buildModel(p.model, p.size[0], p.size[1], c, seed ?? hash(pl.id))
    const t = turned(p, pl.rot)
    model.rotation.y = -pl.rot * (Math.PI / 2)
    const g = new THREE.Group()
    g.add(model)
    g.position.set((pl.x + t.w / 2) * c, 0, (pl.z + t.d / 2) * c)
    g.userData.lights = model.userData.lights
    g.userData.door = model.userData.door
    return g
  }

  // Night lights from the pieces that name them (hearths, windows, a well's lamp)
  private rebuildLights(): void {
    this.lightsGroup.clear()
    this.pointLights = 0
    const v = new THREE.Vector3()
    for (const { group } of this.pieces.values()) {
      const pts = group.userData.lights as [number, number, number][] | undefined
      if (!pts) continue
      group.updateMatrixWorld(true)
      const model = group.children[0]
      for (const [x, y, z] of pts) {
        v.set(x, y, z)
        model.localToWorld(v)
        const l = new THREE.PointLight(0xffa64d, 30, 18, 2)
        l.position.copy(v)
        this.lightsGroup.add(l)
        this.pointLights++
      }
    }
  }

  // Adds, moves and removes the people to match the area (S3)
  private syncNpcs(): void {
    const want = this.area.npcs ?? {}
    for (const [id, n] of Object.entries(want)) {
      const key = JSON.stringify(n)
      let have = this.npcs.get(id)
      if (have?.key === key) continue
      if (!have) {
        const model = new CharacterModel(npcColor(n))
        this.scene.add(model.group)
        const label = document.createElement('div')
        label.dataset.npc = id
        label.style.cssText =
          'position:absolute;z-index:2;padding:1px 6px;border-radius:3px;font:12px Georgia,serif;color:#f3e7cf;pointer-events:none;' +
          'background:rgba(20,16,12,0.75);transform:translate(-50%,-160%);white-space:nowrap'
        this.container.appendChild(label)
        have = { key, model, label }
        this.npcs.set(id, have)
      }
      have.key = key
      have.label.textContent = n.name
      have.model.group.position.set(n.x, 0, n.z)
      // facing 0 is north (-z), turning clockwise; the model faces +z at rotation 0
      have.model.group.rotation.y = Math.PI - (n.facing ?? 0)
    }
    for (const [id, have] of this.npcs) {
      if (want[id]) continue
      this.scene.remove(have.model.group)
      have.label.remove()
      this.npcs.delete(id)
    }
    if (this.playing) this.buildWalk()
  }

  // The person standing nearest a point on the ground, within reach
  private npcAt(x: number, z: number, reach = 1.2): Npc | null {
    let best: Npc | null = null
    let bestD = reach
    for (const n of Object.values(this.area.npcs ?? {})) {
      const d = Math.hypot(n.x - x, n.z - z)
      if (d < bestD) {
        best = n
        bestD = d
      }
    }
    return best
  }

  // The linked doors with where a character stands to go through each
  private doors(): { id: string; link: string; at: Point }[] {
    const out: { id: string; link: string; at: Point }[] = []
    const v = new THREE.Vector3()
    for (const [id, { group }] of this.pieces) {
      const d = group.userData.door as [number, number] | undefined
      const pl = this.area.pieces[id]
      if (!d || !pl) continue
      group.updateMatrixWorld(true)
      v.set(d[0], 0, d[1])
      group.children[0].localToWorld(v)
      out.push({ id, link: pl.link ?? '', at: { x: v.x, z: v.z } })
    }
    return out
  }

  // Where a character comes in from another area: the door leading back there
  private doorTo(area: string): Point | null {
    const d = this.doors().find((x) => x.link === area)
    if (!d) return null
    const near = this.walk?.nearestWalkable(d.at, 6)
    return near && this.walk ? this.walk.centre(near[0], near[1]) : d.at
  }

  private drawSelection(): void {
    const pl = this.selected ? this.area.pieces[this.selected] : undefined
    const p = pl && pieceOf(this.set, pl.piece)
    if (!pl || !p) {
      this.selection.visible = false
      return
    }
    const t = turned(p, pl.rot)
    this.selection.geometry.dispose()
    this.selection.geometry = rectLines(pl.x * this.set.cell, pl.z * this.set.cell, t.w * this.set.cell, t.d * this.set.cell, 0.4)
    this.selection.visible = true
  }

  private onPointerDown = (e: PointerEvent) => {
    this.renderer.canvas.focus()
    if (e.button !== 0) return
    if (this.playing) {
      const hit = this.groundAt(e.offsetX, e.offsetY)
      if (!hit) return
      this.talk.end()
      this.arrival = null
      // a person: walk up to them and talk
      const n = this.npcAt(hit.x, hit.z)
      if (n) {
        if (!this.walkNear({ x: n.x, z: n.z })) this.flash('No way there')
        else this.arrival = { kind: 'talk', id: n.id }
        return
      }
      // a linked door: walk to it and go through
      const door = this.doors().find((d) => Math.hypot(d.at.x - hit.x, d.at.z - hit.z) < 3.5)
      if (door) {
        if (!door.link) {
          this.flash('This door leads nowhere yet')
          return
        }
        if (!this.walkNear(door.at)) this.flash('No way there')
        else this.arrival = { kind: 'travel', area: door.link }
        return
      }
      if (!this.walkTo(hit.x, hit.z)) this.flash('No way there')
      return
    }
    if (this.npcTool) {
      const hit = this.groundAt(e.offsetX, e.offsetY)
      const W = this.area.width * this.set.cell
      const D = this.area.depth * this.set.cell
      if (!hit || hit.x < 0 || hit.z < 0 || hit.x > W || hit.z > D) return
      this.opts.onIntent?.({ kind: 'placeNpc', npcKind: this.npcTool, x: round1(hit.x), z: round1(hit.z) })
      return
    }
    {
      const hit = this.groundAt(e.offsetX, e.offsetY)
      const n = hit && !this.tool ? this.npcAt(hit.x, hit.z) : null
      if (n) {
        this.selectNpc(n.id)
        return
      }
      if (this.selectedNpc) this.selectNpc(null)
    }
    const cell = this.cellAt(e.offsetX, e.offsetY)
    if (!cell) return
    if (this.tool) {
      const pl: Placement = { id: '', piece: this.tool, x: cell.x, z: cell.z, rot: this.rot }
      const why = fit(this.set, this.area.width, this.area.depth, this.area.pieces, pl)
      if (why) this.opts.onRefused?.(why)
      else this.opts.onIntent?.({ kind: 'place', piece: this.tool, x: cell.x, z: cell.z, rot: this.rot })
      return
    }
    this.select(this.placementAt(cell.x, cell.z))
  }

  private onKey = (e: KeyboardEvent) => {
    if (this.playing) {
      if (e.key === 'Escape') this.path = []
      return
    }
    if (this.selectedNpc) {
      const n = this.area.npcs?.[this.selectedNpc]
      if (!n) return
      if (e.key === 'Delete' || e.key === 'Backspace') this.opts.onIntent?.({ kind: 'removeNpc', id: n.id })
      else if (e.key === 'Escape') this.selectNpc(null)
      else if (e.key.startsWith('Arrow') && e.altKey) {
        const d = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }[e.key] ?? [0, 0]
        const W = this.area.width * this.set.cell
        const D = this.area.depth * this.set.cell
        const x = Math.min(W, Math.max(0, n.x + d[0] * 0.5))
        const z = Math.min(D, Math.max(0, n.z + d[1] * 0.5))
        this.opts.onIntent?.({ kind: 'moveNpc', id: n.id, x, z })
        e.preventDefault()
      }
      return
    }
    if (e.key === 'Escape' && this.npcTool) {
      this.setNpcTool(null)
      return
    }
    if (e.key === 'r' || e.key === 'R') {
      if (this.tool) {
        this.setTool(this.tool, this.rot + (e.shiftKey ? 3 : 1))
      } else if (this.selected) {
        const pl = this.area.pieces[this.selected]
        const rot = (pl.rot + (e.shiftKey ? 3 : 1)) % 4
        const why = fit(this.set, this.area.width, this.area.depth, this.area.pieces, { ...pl, rot })
        if (why) this.opts.onRefused?.(why)
        else this.opts.onIntent?.({ kind: 'move', id: pl.id, rot })
      }
    } else if ((e.key === 'Delete' || e.key === 'Backspace') && this.selected) {
      this.opts.onIntent?.({ kind: 'remove', id: this.selected })
    } else if (e.key === 'Escape') {
      if (this.tool) this.setTool(null)
      else this.select(null)
    } else if (this.selected && e.key.startsWith('Arrow') && e.altKey) {
      // Alt + arrows move the selection a cell
      const pl = this.area.pieces[this.selected]
      const d = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }[e.key] ?? [0, 0]
      const next = { ...pl, x: pl.x + d[0], z: pl.z + d[1] }
      const why = fit(this.set, this.area.width, this.area.depth, this.area.pieces, next)
      if (why) this.opts.onRefused?.(why)
      else this.opts.onIntent?.({ kind: 'move', id: pl.id, x: next.x, z: next.z })
      e.preventDefault()
    }
  }

  // The point on the ground under a pixel
  private groundAt(px: number, py: number): THREE.Vector3 | null {
    const canvas = this.renderer.canvas
    const ndc = new THREE.Vector2((px / canvas.clientWidth) * 2 - 1, -(py / canvas.clientHeight) * 2 + 1)
    this.raycaster.setFromCamera(ndc, this.orbit.camera)
    const hit = new THREE.Vector3()
    return this.raycaster.ray.intersectPlane(this.plane, hit)
  }

  private cellAt(px: number, py: number): { x: number; z: number } | null {
    const hit = this.groundAt(px, py)
    if (!hit) return null
    const x = Math.floor(hit.x / this.set.cell)
    const z = Math.floor(hit.z / this.set.cell)
    if (x < 0 || z < 0 || x >= this.area.width || z >= this.area.depth) return null
    return { x, z }
  }

  private placementAt(x: number, z: number): string | null {
    for (const pl of Object.values(this.area.pieces)) {
      const p = pieceOf(this.set, pl.piece)
      if (!p) continue
      const t = turned(p, pl.rot)
      if (x >= pl.x && x < pl.x + t.w && z >= pl.z && z < pl.z + t.d) return pl.id
    }
    return null
  }

  // The walk grid from the placed pieces' blockers, each turned and moved
  // with its piece (S2.2)
  private buildWalk(): void {
    const t0 = performance.now()
    const c = this.set.cell
    // rooms (2 m cells) need a finer grid than the farmstead to pass between furniture
    const grid = new WalkGrid(this.area.width * c, this.area.depth * c, c < 5 ? 0.25 : 1)
    const v = new THREE.Vector3()
    for (const { group } of this.pieces.values()) {
      const model = group.children[0]
      const rects = model?.userData.blockers as [number, number, number, number][] | undefined
      if (!rects) continue
      group.updateMatrixWorld(true)
      for (const [x0, z0, x1, z1] of rects) {
        let minX = Infinity
        let minZ = Infinity
        let maxX = -Infinity
        let maxZ = -Infinity
        for (const [x, z] of [
          [x0, z0],
          [x1, z0],
          [x0, z1],
          [x1, z1],
        ]) {
          v.set(x, 0, z)
          model.localToWorld(v)
          minX = Math.min(minX, v.x)
          minZ = Math.min(minZ, v.z)
          maxX = Math.max(maxX, v.x)
          maxZ = Math.max(maxZ, v.z)
        }
        grid.block(minX, minZ, maxX - minX, maxZ - minZ)
      }
    }
    for (const n of Object.values(this.area.npcs ?? {})) grid.block(n.x - 0.4, n.z - 0.4, 0.8, 0.8)
    this.walk = grid
    this.navMs = performance.now() - t0
    // A piece laid on the character, or on its way, moves it aside or finds another way
    const ch = this.character
    if (ch) {
      const at = { x: ch.group.position.x, z: ch.group.position.z }
      const [col, row] = grid.cellOf(at)
      if (!grid.walkable(col, row)) {
        const near = grid.nearestWalkable(at)
        if (near) {
          const p = grid.centre(near[0], near[1])
          ch.group.position.set(p.x, 0, p.z)
        }
      }
      const end = this.path[this.path.length - 1]
      if (end) this.walkTo(end.x, end.z)
    }
  }

  // Where the character starts: the middle of a yard if there is one, else the area's
  private spawnPoint(): Point {
    const c = this.set.cell
    let at: Point = { x: (this.area.width * c) / 2, z: (this.area.depth * c) / 2 }
    for (const pl of Object.values(this.area.pieces)) {
      const p = pieceOf(this.set, pl.piece)
      if (p?.model !== 'yard') continue
      const t = turned(p, pl.rot)
      at = { x: (pl.x + t.w / 2) * c + 2, z: (pl.z + t.d / 2) * c + 2 }
      break
    }
    const near = this.walk?.nearestWalkable(at, 40)
    return near && this.walk ? this.walk.centre(near[0], near[1]) : at
  }

  // Moves the character along its path at walking pace, facing its way
  private stepCharacter(dt: number): void {
    const ch = this.character
    if (!ch) return
    const speed = 4
    let budget = speed * dt
    const pos = ch.group.position
    while (budget > 0 && this.path.length) {
      const next = this.path[0]
      const dx = next.x - pos.x
      const dz = next.z - pos.z
      const d = Math.hypot(dx, dz)
      if (d > 1e-4) ch.group.rotation.y = Math.atan2(dx, dz)
      if (d <= budget) {
        pos.x = next.x
        pos.z = next.z
        budget -= d
        this.path.shift()
      } else {
        pos.x += (dx / d) * budget
        pos.z += (dz / d) * budget
        budget = 0
      }
    }
    if (!this.path.length) {
      this.marker.visible = false
      const a = this.arrival
      this.arrival = null
      if (a?.kind === 'talk') {
        const n = this.area.npcs?.[a.id]
        if (n) ch.group.rotation.y = Math.atan2(n.x - pos.x, n.z - pos.z)
        if (!this.talkTo(a.id)) this.flash(`${n?.name ?? 'They'} has nothing to say`)
      } else if (a?.kind === 'travel') {
        this.opts.onTravel?.(a.area)
      }
    }
    const target = this.path.length ? 1 : 0
    this.moving += (target - this.moving) * Math.min(1, dt * 10)
    this.stride += dt * speed * 2.4 * (this.path.length ? 1 : 0)
    ch.walk(this.stride, this.moving)
    // The camera follows
    const k = Math.min(1, dt * 4)
    this.orbit.target.x += (pos.x - this.orbit.target.x) * k
    this.orbit.target.z += (pos.z - this.orbit.target.z) * k
  }

  // A short message in place of the hint
  private flash(text: string): void {
    this.hint.textContent = text
    setTimeout(() => this.updateHint(), 1500)
  }

  private step(dt: number): void {
    if (this.playing) this.stepCharacter(dt)
    this.orbit.step(dt, this.input)
    if (this.playing && this.walk) {
      // The context cursor: a hand where the ground may be walked to
      const g = this.groundAt(this.input.pointerX, this.input.pointerY)
      const ok = g && this.walk.walkable(...this.walk.cellOf({ x: g.x, z: g.z }))
      this.renderer.canvas.style.cursor = ok ? 'pointer' : 'not-allowed'
    }
    this.hover = this.cellAt(this.input.pointerX, this.input.pointerY)
    this.updatePreview()
    this.reportCursor()
    this.input.endStep()
  }

  // The piece in hand at the hovered cell: its model in a tint, its
  // footprint green or red, and its edges marked by type
  private updatePreview(): void {
    const key = this.tool && this.hover ? `${this.tool}:${this.rot}:${this.hover.x}:${this.hover.z}` : ''
    if (key === this.previewKey) return
    this.previewKey = key
    this.preview.clear()
    if (!this.tool || !this.hover) return
    const p = pieceOf(this.set, this.tool)
    if (!p) return
    const c = this.set.cell
    const pl: Placement = { id: '', piece: this.tool, x: this.hover.x, z: this.hover.z, rot: this.rot }
    const ok = fit(this.set, this.area.width, this.area.depth, this.area.pieces, pl) === ''
    const t = turned(p, pl.rot)
    const model = this.placedModel(pl, c, 7)
    if (model) {
      const ghost = new THREE.MeshStandardNodeMaterial({ color: ok ? GOOD : BAD, transparent: true, opacity: 0.45, depthWrite: false, flatShading: true })
      model.traverse((o) => {
        if (o instanceof THREE.Mesh) {
          o.material = ghost
          o.castShadow = false
        }
      })
      model.position.y += 0.1
      this.preview.add(model)
    }
    const outline = new THREE.LineSegments(rectLines(pl.x * c, pl.z * c, t.w * c, t.d * c, 0.5), new THREE.LineBasicNodeMaterial({ color: ok ? GOOD : BAD }))
    this.preview.add(outline)
    // Edge marks: a short bar at each segment's middle, coloured by type
    const marks = (side: 'n' | 'e' | 's' | 'w', list: string[]) => {
      list.forEach((type, i) => {
        const color = EDGE_COLORS[type] ?? 0xffffff
        const len = c * 0.5
        const geo = side === 'n' || side === 's' ? new THREE.BoxGeometry(len, 0.3, 0.6) : new THREE.BoxGeometry(0.6, 0.3, len)
        const m = new THREE.Mesh(geo, new THREE.MeshBasicNodeMaterial({ color }))
        const x0 = pl.x * c
        const z0 = pl.z * c
        if (side === 'n') m.position.set(x0 + (i + 0.5) * c, 0.6, z0 + 0.5)
        if (side === 's') m.position.set(x0 + (i + 0.5) * c, 0.6, z0 + t.d * c - 0.5)
        if (side === 'w') m.position.set(x0 + 0.5, 0.6, z0 + (i + 0.5) * c)
        if (side === 'e') m.position.set(x0 + t.w * c - 0.5, 0.6, z0 + (i + 0.5) * c)
        this.preview.add(m)
      })
    }
    marks('n', t.edges.n)
    marks('e', t.edges.e)
    marks('s', t.edges.s)
    marks('w', t.edges.w)
  }

  private reportCursor(): void {
    const now = performance.now()
    if (!this.opts.onCursor || now - this.cursorTick < 100) return
    const c = this.cursor()
    const r = (v: number) => Math.round(v * 10) / 10
    const key = JSON.stringify([c.x, c.z, c.piece, c.rot, c.selected, r(c.camera.x), r(c.camera.z), r(c.camera.yaw), r(c.camera.distance)])
    if (key === this.lastCursor) return
    this.lastCursor = key
    this.cursorTick = now
    this.opts.onCursor(c)
  }

  private toolChanged(): void {
    this.opts.onToolChange?.({ piece: this.tool, rot: this.rot, selected: this.selected })
  }

  // Another person's cursor: their hovered footprint (with the piece in
  // hand, if any) and their selection, in their colour
  private remoteGroup(c: RemoteCursor): THREE.Group {
    const g = new THREE.Group()
    const color = new THREE.Color(c.color)
    const cell = this.set.cell
    const lineMat = new THREE.LineBasicNodeMaterial({ color })
    if (c.x !== null && c.z !== null) {
      let w = 1
      let d = 1
      const p = c.piece ? pieceOf(this.set, c.piece) : undefined
      if (p) {
        const t = turned(p, c.rot ?? 0)
        w = t.w
        d = t.d
      }
      g.add(new THREE.LineSegments(rectLines(c.x * cell, c.z * cell, w * cell, d * cell, 0.7), lineMat))
      g.userData.anchor = new THREE.Vector3((c.x + w / 2) * cell, 2, (c.z + d / 2) * cell)
    }
    const sel = c.selected ? this.area.pieces[c.selected] : undefined
    const sp = sel && pieceOf(this.set, sel.piece)
    if (sel && sp) {
      const t = turned(sp, sel.rot)
      g.add(new THREE.LineSegments(rectLines(sel.x * cell, sel.z * cell, t.w * cell, t.d * cell, 0.3), lineMat))
      g.userData.anchor ??= new THREE.Vector3((sel.x + t.w / 2) * cell, 2, (sel.z + t.d / 2) * cell)
    }
    return g
  }

  private placeLabels(): void {
    const w = this.renderer.canvas.clientWidth
    const h = this.renderer.canvas.clientHeight
    const v = new THREE.Vector3()
    for (const n of this.npcs.values()) {
      v.copy(n.model.group.position)
      v.y = 2.4
      v.project(this.orbit.camera)
      const visible = v.z < 1 && Math.abs(v.x) < 1.1 && Math.abs(v.y) < 1.1
      n.label.style.display = visible ? 'block' : 'none'
      n.label.style.left = `${((v.x + 1) / 2) * w}px`
      n.label.style.top = `${((1 - v.y) / 2) * h}px`
    }
    for (const r of this.remotes.values()) {
      const a = r.group.userData.anchor as THREE.Vector3 | undefined
      if (!a) {
        r.label.style.display = 'none'
        continue
      }
      v.copy(a).project(this.orbit.camera)
      const visible = v.z < 1 && Math.abs(v.x) < 1.1 && Math.abs(v.y) < 1.1
      r.label.style.display = visible ? 'block' : 'none'
      r.label.style.left = `${((v.x + 1) / 2) * w}px`
      r.label.style.top = `${((1 - v.y) / 2) * h}px`
    }
  }

  private updateHint(): void {
    this.hint.textContent = this.talk.open
      ? 'Pick an answer · 1 to 9 · Esc to end the talk'
      : this.playing
      ? 'Playing · click the ground to walk there · click a person to talk · click a door to go through · Esc to stop walking'
      : this.npcTool
      ? 'Click to stand a person there · Esc to stop'
      : this.selectedNpc
      ? 'Alt+arrows to move · Delete to remove · Esc to deselect'
      : this.tool
      ? `Placing ${pieceOf(this.set, this.tool)?.title ?? this.tool} · click to place · R to turn · Esc to stop`
      : this.selected
        ? 'R to turn · Alt+arrows to move · Delete to remove · Esc to deselect'
        : 'Pick a piece to place · click a piece to select · right-drag to turn the view · WASD to pan · wheel to zoom'
  }

  private render(): void {
    this.renderer.render(this.scene, this.orbit.camera)
    this.lastInfo = this.renderer.info()
    this.placeLabels()
    if (this.overlay) {
      const s = this.stats()
      this.overlay.set('backend', s.backend)
      this.overlay.set('frame', `${s.frameMs.toFixed(1)} ms (${s.fps.toFixed(0)} fps)`)
      this.overlay.set('draws', `${s.drawCalls} · ${s.triangles.toLocaleString()} tris`)
      this.overlay.set('pieces', `${s.pieces}${this.lighting === 'night' ? ` · ${s.lights} lights` : ''}`)
    }
  }
}

// A rectangle's outline on the ground, lifted by y
function rectLines(x: number, z: number, w: number, d: number, y: number): THREE.BufferGeometry {
  const g = new THREE.BufferGeometry()
  // prettier-ignore
  g.setAttribute('position', new THREE.Float32BufferAttribute([
    x, y, z, x + w, y, z,  x + w, y, z, x + w, y, z + d,
    x + w, y, z + d, x, y, z + d,  x, y, z + d, x, y, z,
  ], 3))
  return g
}

function round1(v: number): number {
  return Math.round(v * 10) / 10
}

// A person's cloak, by their look
function npcColor(n: Npc): number {
  return { farmer: 0x7a6a3a, wife: 0x6b3b4a, child: 0x4a6b5a }[n.kind ?? ''] ?? 0x8a5a3a
}

function hash(s: string): number {
  let h = 2166136261
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619)
  return h >>> 0
}

// A moon's disc with its lit part, drawn on a canvas: the dark disc, then
// the lit half on its side, then the terminator's ellipse adding to it
// (gibbous) or taking from it (crescent)
function drawPhase(c: HTMLCanvasElement, m: MoonPhase, tint: string): void {
  const g = c.getContext('2d')
  if (!g) return
  const r = c.width / 2 - 1
  const cx = c.width / 2
  g.clearRect(0, 0, c.width, c.height)
  g.fillStyle = 'rgba(255,255,255,0.10)'
  g.beginPath()
  g.arc(cx, cx, r, 0, Math.PI * 2)
  g.fill()
  if (m.lit < 0.01) return
  const right = m.waxing
  const k = Math.abs(1 - 2 * m.lit) * r // the terminator ellipse's half width
  g.fillStyle = tint
  g.beginPath()
  // the lit half: from top to bottom round the lit side
  g.arc(cx, cx, r, -Math.PI / 2, Math.PI / 2, !right)
  // back up along the terminator: bulging into the dark side when more than half is lit
  const gibbous = m.lit > 0.5
  g.ellipse(cx, cx, k, r, 0, Math.PI / 2, -Math.PI / 2, gibbous ? !right : right)
  g.fill()
}

function phaseName(m: MoonPhase): string {
  if (m.lit < 0.04) return 'new'
  if (m.lit > 0.96) return 'full'
  const way = m.waxing ? 'waxing' : 'waning'
  if (Math.abs(m.lit - 0.5) < 0.08) return m.waxing ? 'first quarter' : 'last quarter'
  return `${way} ${m.lit > 0.5 ? 'gibbous' : 'crescent'}`
}
