import * as THREE from 'three/webgpu'
import { OrbitCamera } from '../camera/OrbitCamera.ts'
import { Loop } from '../core/Loop.ts'
import { StatsOverlay } from '../debug/StatsOverlay.ts'
import { Input } from '../input/Input.ts'
import { Renderer } from '../render/Renderer.ts'
import { WalkGrid, type Point } from '../nav/WalkGrid.ts'
import { moonLight, moons, type MoonPhase } from '../sky/Moons.ts'
import { Combat, HERO_HP, isHostile, WEAPONS, type CombatEvent, type Skills, type Stance } from '../combat/Combat.ts'
import { CombatHud } from '../combat/CombatHud.ts'
import { HearthNight, WATCHER_HP } from '../defence/HearthNight.ts'
import { NightHud } from '../defence/NightHud.ts'
import { whistle } from '../defence/Whistle.ts'
import { DialoguePanel } from './DialoguePanel.ts'
import { FirstHourPanel } from '../objectives/FirstHour.ts'
import { dragMarks, drawCard, herbSpots, howl, nextStep, showCard, showHerbal, SIGNATURE_TEXT, storySteps } from '../story/FirstHourStory.ts'
import { canSleep, CLOCK_START, clockText, isDay, nightOf, showAlmanac, sleep, stepClock, type Clock } from '../story/Clock.ts'
import { BODY_START, BodyHud, breathe, BreathPrompt, cook, humPool, rounded as roundBody, showCooking, spendHum, stepBody, type Body } from '../story/Hum.ts'
import { ashPile, buildModel, CharacterModel, interiorShell, material } from './Models.ts'
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
  // S4: the weapon in hand in play (WEAPONS: sword, pitchfork) and the character's skills
  weapon?: string
  skills?: Skills
  // S6: a step of the first hour reached (step empty: only the place, saved
  // now and then); the host sends it as game.reachStory to the story's area.
  // S7: item and spot, a herb gathered
  onStory?: (s: { step: string; area: string; x: number; z: number; item?: string; spot?: string; use?: string; body?: Body; clock?: Clock }) => void
}

// S6: a player's place in a story as the server keeps it (game.story)
export interface StoryPlace {
  done: Record<string, boolean>
  last?: string
  area?: string
  at?: [number, number]
  // S7: herbs gathered, and the spots they came from
  items?: Record<string, number>
  picked?: Record<string, boolean>
  // S8: the Hum, Chill and Hunger when last saved
  body?: Body
  // S9: the story's day and hour when last saved
  clock?: Clock
}

// The thimble's skills at the first fight (375 › the first hour): a little
// blade from muscle memory, a farm hand's polearm, the first of reading
export const STARTING_SKILLS: Skills = { blade: 2, polearm: 1, reading: 1 }

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
  // S4: the fight in play, if the area holds foes
  combat: { hp: number; stance: Stance; weapon: string; foes: number; alive: number; over: 'won' | 'fallen' | null; target: string | null } | null
  // A night of hearth defence (S5)
  hearth: { house: string; time: number; left: number; watcherHp: number; barrel: boolean; inside: number; outcome: 'held' | 'taken' | null; signals: number[] } | null
  // The first hour (S6): the story's area, the steps done and the next, and the card drawn
  story: { area: string; done: string[]; next: string | null; card: string | null; items: Record<string, number> } | null
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
  // S4: the fight, its panel, the foes' ash and where the character was last sent toward its target
  private combat: Combat | null = null
  private hearth: HearthNight | null = null
  private hearthSignals: number[] = []
  private readonly nightHud: NightHud
  private readonly hud: CombatHud
  private readonly ash = new THREE.Group()
  private weapon: string
  private skills: Skills
  private approachTick = 0
  private loopTime = 0
  // S6: the first hour being played: its area, the steps done, the marks the
  // raid left, where to stand after resuming in another area, the door last
  // taken, the last place saved and the card drawn
  private story: { area: string; done: Record<string, boolean>; card: string | null; items: Record<string, number>; picked: Record<string, boolean>; body: Body; clock: Clock } | null = null
  private almanacEl: HTMLDivElement | null = null
  private storyDay: boolean | null = null // the light the clock last set
  private slept = false
  // S8: the Hum, Chill and Hunger, stillness and the hearth's cooking
  private bodyHud!: BodyHud
  private breath!: BreathPrompt
  private cooking: HTMLDivElement | null = null
  private wasNearHearth = false
  private bodySaved: Body | null = null
  private readonly herbs = new THREE.Group()
  private herbal: HTMLDivElement | null = null
  private readonly storyPanel: FirstHourPanel
  private readonly marks = new THREE.Group()
  private signature: Point | null = null
  private barnDoor: Point | null = null
  private resumeAt: { area: string; at: Point } | null = null
  private via = ''
  private talkingWith = ''
  private saved = { x: 0, z: 0, t: 0 }
  private readonly caption: HTMLDivElement

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
    this.talk = new DialoguePanel(container, () => this.talkEnded())
    this.hud = new CombatHud(container, (s) => this.setStance(s))
    this.nightHud = new NightHud(container)
    this.weapon = opts.weapon ?? 'sword'
    this.skills = opts.skills ?? STARTING_SKILLS
    this.scene.add(this.ash, this.marks, this.herbs)
    this.storyPanel = new FirstHourPanel(container, 96)
    this.storyPanel.title = 'Your story'
    this.bodyHud = new BodyHud(container, 116)
    this.breath = new BreathPrompt(container)
    this.caption = document.createElement('div')
    this.caption.dataset.storyCaption = ''
    this.caption.style.cssText =
      'position:absolute;left:50%;bottom:44px;transform:translateX(-50%);width:min(560px,calc(100% - 32px));z-index:3;display:none;pointer-events:none;' +
      'padding:8px 12px;border-radius:6px;background:rgba(16,14,12,0.85);border:1px solid #6b5a43;font:italic 14px Georgia,serif;color:#eadfc8;line-height:1.4;text-align:center'
    container.appendChild(this.caption)

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

  // The weapon in hand (WEAPONS) and the character's skills; a fight under way restarts with them
  setWeapon(id: string): void {
    if (!WEAPONS[id] || id === this.weapon) return
    this.weapon = id
    if (this.combat) this.startCombat()
  }

  setSkills(s: Skills): void {
    this.skills = s
    if (this.combat) this.startCombat()
  }

  // Strike, Pivot or Guard, as keys 1 to 3 or the panel's buttons
  setStance(s: Stance): void {
    this.combat?.setStance(s)
  }

  // Orders the character to fight a foe (as clicking it in play does)
  fight(id: string): boolean {
    if (!this.combat?.foes.get(id)?.alive) return false
    this.combat.attack(id)
    this.talk.end()
    this.arrival = null
    return true
  }

  // Talks to a person now, as walking up to them does in play
  talkTo(id: string): boolean {
    const n = this.area.npcs?.[id]
    const d = n?.dialogue ? this.dialogues[n.dialogue] : undefined
    if (!n || !d) return false
    this.talkingWith = d.id
    this.caption.style.display = 'none'
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
    this.area = this.withNight(area)
    this.set = set
    for (const p of this.pieces.values()) this.piecesGroup.remove(p.group)
    this.pieces.clear()
    this.layout(true)
    this.syncPieces()
    this.syncNpcs()
    this.buildWalk()
    const resume = this.resumeAt?.area === area.id ? this.resumeAt.at : null
    this.resumeAt = null
    const at = resume ?? this.doorTo(from) ?? this.spawnPoint()
    this.path = []
    this.arrival = null
    this.character?.group.position.set(at.x, 0, at.z)
    this.orbit.target.set(at.x, 0, at.z)
    this.orbit.distance = this.set.interior ? 16 : 32
    this.grid.visible = false
    this.startCombat()
    this.drawMarks()
    if (this.via === 'signpost') this.reach('road')
    this.via = ''
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
      this.startCombat()
      this.grid.visible = false
      if (this.story) {
        this.saved = { x: at.x, z: at.z, t: this.loopTime }
        this.reach('arrive')
        this.drawMarks()
      }
    } else {
      if (this.story) this.savePlace(true)
      if (this.character) this.scene.remove(this.character.group)
      this.character = null
      this.walk = null
      this.path = []
      this.arrival = null
      this.marker.visible = false
      this.talk.end()
      this.endCombat()
      this.hearth = null
      this.nightHud.show(false)
      this.story = null
      this.resumeAt = null
      this.marks.clear()
      this.herbs.clear()
      this.herbal?.remove()
      this.herbal = null
      this.storyPanel.show([], false)
      this.caption.style.display = 'none'
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
      }
      this.syncNpcs(true)
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
      combat: this.combat
        ? {
            hp: this.combat.hp,
            stance: this.combat.stance,
            weapon: this.combat.weapon.id,
            foes: this.combat.foes.size,
            alive: [...this.combat.foes.values()].filter((f) => f.alive).length,
            over: this.combat.over,
            target: this.combat.target,
          }
        : null,
      hearth: this.hearth
        ? {
            house: this.hearth.house,
            time: Math.round(this.hearth.time * 10) / 10,
            left: Math.round(this.hearth.left),
            watcherHp: this.hearth.watcherHp,
            barrel: this.hearth.barrel,
            inside: this.hearth.inside.size,
            outcome: this.hearth.outcome,
            signals: [...this.hearthSignals],
          }
        : null,
      story: this.story
        ? { area: this.story.area, done: Object.keys(this.story.done).filter((k) => this.story!.done[k]), next: nextStep(this.story.done), card: this.story.card, items: { ...this.story.items } }
        : null,
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
    this.hud.dispose()
    this.nightHud.dispose()
    this.storyPanel.dispose()
    this.caption.remove()
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
  private syncNpcs(force = false): void {
    const hide = this.foesHidden()
    const want = Object.fromEntries(Object.entries(this.area.npcs ?? {}).filter(([, n]) => !(hide && isHostile(n.kind))))
    if (force) {
      for (const have of this.npcs.values()) {
        this.scene.remove(have.model.group)
        have.label.remove()
      }
      this.npcs.clear()
    }
    for (const [id, n] of Object.entries(want)) {
      const key = JSON.stringify(n)
      let have = this.npcs.get(id)
      if (have?.key === key) continue
      if (this.combat?.foes.has(id)) continue // a foe in a fight moves by the fight, not the definition
      if (!have) {
        const model = new CharacterModel(npcColor(n), isHostile(n.kind))
        this.scene.add(model.group)
        const label = document.createElement('div')
        label.dataset.npc = id
        label.style.cssText =
          'position:absolute;z-index:2;padding:1px 6px;border-radius:3px;font:12px Georgia,serif;color:#f3e7cf;pointer-events:none;' +
          `background:${isHostile(n.kind) ? 'rgba(90,20,14,0.8)' : 'rgba(20,16,12,0.75)'};transform:translate(-50%,-160%);white-space:nowrap`
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

  // Starts the fight with the area's foes, if it has any (play, travel, a new weapon)
  private startCombat(): void {
    this.endCombat()
    if (this.foesHidden()) return
    const foes = Object.values(this.area.npcs ?? {}).filter((n) => isHostile(n.kind))
    if (!foes.length || !this.playing) return
    const walk = this.walk
    this.combat = new Combat(
      foes.map((n) => ({ id: n.id, kind: n.kind!, x: n.x, z: n.z })),
      {
        skills: this.skills,
        weapon: WEAPONS[this.weapon] ?? WEAPONS.sword,
        seed: hash(this.area.id + this.area.version),
        walkable: walk ? (x, z) => walk.walkable(...walk.cellOf({ x, z })) : undefined,
        cadenceAllowed: () => this.cadenceAllowed(),
      },
    )
    this.hud.show(true)
    this.hud.say(`${foes.length === 1 ? 'A Hollowed' : `${foes.length} Hollowed`}. Only the neck or the heart will end them.`)
    this.updateHud()
    this.updateHint()
  }

  private endCombat(): void {
    if (!this.combat) return
    this.combat = null
    this.ash.clear()
    this.hud.show(false)
    this.syncNpcs(true)
    this.updateHint()
  }

  private foeAt(x: number, z: number): string | null {
    let best: string | null = null
    let bestD = 1.6
    for (const f of this.combat?.foes.values() ?? []) {
      const d = Math.hypot(f.x - x, f.z - z)
      if (f.alive && d < bestD) {
        best = f.id
        bestD = d
      }
    }
    return best
  }

  private updateHud(): void {
    const c = this.combat
    if (!c) return
    const skill = c.weapon.kind === 'polearm' ? `polearm ${this.skills.polearm}` : `blade ${this.skills.blade}`
    this.hud.update(c.hp, HERO_HP, c.stance, c.weapon.title, `${skill}, reading ${this.skills.reading}`)
  }

  // One step of the fight: the character closes on its target or circles it
  // when pivoting, the foes move and claw, and what happened is shown
  private stepCombat(dt: number): void {
    const c = this.combat!
    const ch = this.character
    if (!ch || c.over === 'fallen') return
    const pos = ch.group.position
    const t = c.target ? c.foes.get(c.target) : undefined
    const want = c.wantsApproach(pos)
    this.approachTick -= dt
    if (want && this.approachTick <= 0) {
      this.approachTick = 0.3
      this.walkNear(want)
    } else if (t?.alive && !want && this.path.length) {
      this.path = [] // in reach: stand and fight
      this.marker.visible = false
    }
    if (t?.alive && !want) {
      ch.group.rotation.y = Math.atan2(t.x - pos.x, t.z - pos.z)
      if (c.stance === 'pivot') {
        // circle the target toward its flank
        const a = Math.atan2(pos.x - t.x, pos.z - t.z) + dt * 1.6
        const r = Math.hypot(pos.x - t.x, pos.z - t.z)
        const nx = t.x + Math.sin(a) * r
        const nz = t.z + Math.cos(a) * r
        if (this.walk?.walkable(...this.walk.cellOf({ x: nx, z: nz }))) pos.set(nx, 0, nz)
      }
    }
    for (const e of c.step(dt, pos)) this.showEvent(e)
    // the foes follow the fight, lurching
    let i = 0
    for (const f of c.foes.values()) {
      const have = this.npcs.get(f.id)
      i++
      if (!have || !f.alive) continue
      const g = have.model.group
      const moved = Math.hypot(f.x - g.position.x, f.z - g.position.z)
      g.position.set(f.x + Math.sin(this.loopTime * 23 + i) * 0.03, 0, f.z)
      g.rotation.y = Math.atan2(pos.x - f.x, pos.z - f.z) + Math.sin(this.loopTime * 9 + i) * 0.15
      have.model.walk(this.loopTime * 14 + i, moved > 0.001 ? 1 : f.telling > 0 ? 0.6 : 0)
    }
    this.updateHud()
  }

  private showEvent(e: CombatEvent): void {
    const name = (id: string) => this.area.npcs?.[id]?.name ?? 'It'
    switch (e.kind) {
      case 'tell':
        this.floatText(e.foe, '!', '#ffcc66')
        break
      case 'miss':
        this.hud.say('Your swing goes wide.')
        break
      case 'futile':
        this.floatText(e.foe, 'hacked', '#b9ad94')
        this.hud.say(`You cut ${name(e.foe)} half through. It keeps coming.`)
        break
      case 'hurt':
        this.floatText(e.foe, `-${e.damage}`, '#ff8a65')
        break
      case 'kill': {
        this.hearth?.slay(e.foe)
        this.hud.say(e.vital === 'heart' ? `Through the heart. ${name(e.foe)} falls to ash.` : `Beheaded. ${name(e.foe)} falls to ash.`)
        const have = this.npcs.get(e.foe)
        if (have) {
          const pile = ashPile()
          pile.position.copy(have.model.group.position)
          this.ash.add(pile)
          this.scene.remove(have.model.group)
          have.label.remove()
          this.npcs.delete(e.foe)
        }
        if (this.combat?.over === 'won') {
          this.hud.say('Quiet again. Nothing left but ash and rags.')
          if (this.story && this.area.id === this.story.area) {
            this.reach('fight')
            this.say("Whatever took the Brannocks is gone west. The road north leads on to Hawk's Rest.")
          }
        }
        break
      }
      case 'claw':
        if (e.blocked) this.hud.say(e.read ? 'You read the lunge and turn it on your knife arm. An opening.' : `Your guard takes most of it (-${e.damage}).`)
        else if (e.dodged) this.hud.say('You pivot clear of the claws.')
        else this.hud.say(`Nails rake you, burning (-${e.damage}).`)
        break
      case 'cadence':
        if (this.story?.done.stillness) {
          this.story.body = spendHum(this.story.body)
          this.hud.say('Strike, Pivot, Guard: the Hum answers the old cadence. Your next strike is aimed; your hands go cold.')
          break
        }
        this.hud.say('Strike, Pivot, Guard: the old cadence. Your next strike is aimed.')
        break
      case 'fallen':
        this.hud.say('You go down under them. Press Stop to try again.')
        this.path = []
        this.flash('You were overwhelmed')
        break
    }
  }

  // A word over a foe's head that rises and fades
  private floatText(id: string, text: string, color: string): void {
    const have = this.npcs.get(id)
    if (!have) return
    const el = document.createElement('div')
    el.textContent = text
    el.style.cssText = `position:absolute;z-index:3;pointer-events:none;font:700 14px system-ui,sans-serif;color:${color};text-shadow:0 1px 2px #000;transition:transform 0.8s,opacity 0.8s`
    const v = have.model.group.position.clone()
    v.y = 2.6
    v.project(this.orbit.camera)
    el.style.left = `${((v.x + 1) / 2) * this.renderer.canvas.clientWidth}px`
    el.style.top = `${((1 - v.y) / 2) * this.renderer.canvas.clientHeight}px`
    this.container.appendChild(el)
    requestAnimationFrame(() => {
      el.style.transform = 'translateY(-24px)'
      el.style.opacity = '0'
    })
    setTimeout(() => el.remove(), 900)
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

  // Every door with where a character stands to go through it, linked or not
  doorPoints(): { id: string; link: string; at: Point }[] {
    return this.doors()
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
      // a foe: fight it
      const foe = this.foeAt(hit.x, hit.z)
      if (foe) {
        this.fight(foe)
        return
      }
      this.combat?.attack(null)
      // a person: walk up to them and talk
      const n = this.npcAt(hit.x, hit.z)
      if (n && !isHostile(n.kind)) {
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
        else {
          this.arrival = { kind: 'travel', area: door.link }
          this.via = this.area.pieces[door.id]?.piece ?? ''
        }
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
      if (this.story && (e.key === 'j' || e.key === 'J')) {
        this.storyPanel.shown = !this.storyPanel.shown
        this.showStory()
      }
      if (this.story && (e.key === 'r' || e.key === 'R')) this.rest()
      if (this.story && (e.key === 'k' || e.key === 'K')) this.openHerbal()
      if (this.story && (e.key === 'b' || e.key === 'B')) this.startStillness()
      if (this.story && (e.key === 'c' || e.key === 'C')) this.openCooking()
      if (this.story && (e.key === 'm' || e.key === 'M')) this.openAlmanac()
      if (this.combat && !this.talk.open) {
        const s = ({ '1': 'strike', '2': 'pivot', '3': 'guard' } as Record<string, Stance>)[e.key]
        if (s) this.setStance(s)
      }
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
    for (const n of Object.values(this.area.npcs ?? {})) if (!isHostile(n.kind)) grid.block(n.x - 0.4, n.z - 0.4, 0.8, 0.8)
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
  // --- Hearth defence (S5) ---

  // Starts a night of hearth defence at a house (an interior area holding a
  // hearth and, by day, a hearth-watch), while playing; length in seconds
  startHearthNight(house: AreaDef, opts: { length?: number; seed?: number; raiders?: number; cell?: number } = {}): boolean {
    if (!this.playing) return false
    this.hearth = new HearthNight(house, { seed: opts.seed ?? hash(house.id + house.version), length: opts.length, raiders: opts.raiders, cell: opts.cell ?? 2 })
    this.hearthSignals = []
    this.nightHud.show(true)
    this.nightHud.say(
      this.hearth.watcher
        ? `${this.hearth.watcher.name} keeps the watch in ${house.name}. Listen for the whistle.`
        : `Nobody keeps the watch in ${house.name} tonight.`,
    )
    this.updateNightHud()
    return true
  }

  // The house as the night has it: whatever came through stands in it
  private withNight(area: AreaDef): AreaDef {
    const h = this.hearth
    if (!h || area.id !== h.house) return area
    // those slain are gone; those still standing keep where they stand
    const npcs = Object.fromEntries(Object.entries(area.npcs ?? {}).filter(([id]) => !id.startsWith('raider-') || h.inside.has(id)))
    for (const f of h.inside.values()) if (!npcs[f.id]) npcs[f.id] = { id: f.id, name: 'Hollowed', kind: f.kind, x: f.x, z: f.z, facing: Math.PI }
    return { ...area, npcs }
  }

  private updateNightHud(): void {
    const h = this.hearth
    if (!h) return
    this.nightHud.update(h.left, h.watcher ? { hp: h.watcherHp, max: WATCHER_HP } : null, h.barrel, h.inside.size)
  }

  private nightTick = 0
  private stepHearth(dt: number): void {
    const h = this.hearth!
    const present = this.area.id === h.house
    const where = present ? 'by the hearth' : 'from the house'
    for (const e of h.step(dt, present)) {
      switch (e.kind) {
        case 'whistle': {
          this.hearthSignals.push(e.blasts)
          whistle(e.blasts)
          const meaning = { 1: 'One blast: the stone has moved.', 2: 'Two blasts: something has come through.', 3: 'Three blasts: the watch is about to die.' }[e.blasts]
          this.nightHud.say(`♪ ${meaning} (${where})`)
          this.hud.say(meaning)
          break
        }
        case 'through':
          if (present) {
            this.area = this.withNight(this.area)
            this.syncNpcs()
            if (this.combat) this.combat.addFoe(e.foe)
            else this.startCombat()
            this.hud.say('The hearthstone heaves. Something comes up out of the dark.')
          }
          break
        case 'taken':
          this.nightHud.say(`The whistle stops. ${h.watcher ? `${h.watcher.name} is gone, and` : 'Nobody kept the watch, and'} the family with them, dragged under the stone.`)
          this.flash('The house was lost. Press Stop to try the night again.')
          break
        case 'dawn':
          this.nightHud.say('Dawn greys the shutters. The house held.')
          this.flash('The house held until dawn')
          break
      }
    }
    this.nightTick -= dt
    if (this.nightTick <= 0) {
      this.nightTick = 0.25
      this.updateNightHud()
    }
  }

  // --- The first hour (S6) ---

  // Plays the first hour from this area (the farmstead) at night, resuming
  // from the place the server keeps: the steps done, and the area and point
  // the player last stood at (the host travels there through onTravel)
  playStory(place: StoryPlace | null): void {
    if (this.playing) this.play(false)
    this.story = { area: this.area.id, done: { ...(place?.done ?? {}) }, card: null, items: { ...(place?.items ?? {}) }, picked: { ...(place?.picked ?? {}) }, body: { ...(place?.body ?? BODY_START) }, clock: { ...(place?.clock ?? (place?.done.rest ? { day: 1, hour: 7 } : CLOCK_START)) } }
    // S9: the light and the moons follow the story's clock
    this.storyDay = isDay(this.story.clock)
    this.setLighting(this.storyDay ? 'day' : 'night')
    this.setNight(nightOf(this.story.clock))
    const at = place?.at ? { x: place.at[0], z: place.at[1] } : null
    this.play(true)
    this.syncNpcs(true) // the Hollowed hide until the trail is followed
    if (at && place?.area && place.area !== this.area.id) {
      this.resumeAt = { area: place.area, at }
      this.opts.onTravel?.(place.area)
    } else if (at && this.walk?.walkable(...this.walk.cellOf(at))) {
      this.character?.group.position.set(at.x, 0, at.z)
      this.orbit.target.set(at.x, 0, at.z)
      this.saved = { ...at, t: this.loopTime }
    }
    const next = nextStep(this.story.done)
    this.say(next === 'arrive' || next === 'farmer' ? 'The night of the first snow. The light from the capital is gone; the farmhouse is warm.' : next ? (storySteps(this.story.done).find((s) => s.id === next)?.how ?? '') : 'The first hour is over.')
    this.showStory()
    this.updateHint()
  }

  // Marks a step reached, once, and tells the host where
  private reach(step: string): void {
    const st = this.story
    if (!st || st.done[step]) return
    st.done[step] = true
    const p = this.player()
    this.opts.onStory?.({ step, area: this.area.id, x: round1(p?.x ?? 0), z: round1(p?.z ?? 0), ...this.stateToSave() })
    if (p) this.saved = { x: p.x, z: p.z, t: this.loopTime }
    this.showStory()
    this.updateHint()
  }

  // Saves where the player stands every 15 s after moving 3 m, and on stopping
  private savePlace(now = false): void {
    const p = this.player()
    if (!this.story || !p) return
    const moved = Math.hypot(p.x - this.saved.x, p.z - this.saved.z)
    const b = roundBody(this.story.body)
    const sb = this.bodySaved
    const bodyMoved = !sb || sb.hum !== b.hum || Math.abs(sb.chill - b.chill) + Math.abs(sb.hunger - b.hunger) >= 5
    if (!now && ((moved < 3 && !bodyMoved) || this.loopTime - this.saved.t < 15)) return
    if (now && moved < 0.5 && !bodyMoved) return
    this.saved = { x: p.x, z: p.z, t: this.loopTime }
    this.opts.onStory?.({ step: '', area: this.area.id, x: round1(p.x), z: round1(p.z), ...this.stateToSave() })
  }

  // --- The Hum, Chill and Hunger (S8) ---

  // The body and the clock, sent with every save
  private stateToSave(): { body?: Body; clock?: Clock } {
    const st = this.story
    if (!st) return {}
    return { body: this.bodyToSave(), clock: { day: st.clock.day, hour: Math.round(st.clock.hour * 10) / 10 } }
  }

  private bodyToSave(): Body | undefined {
    if (!this.story) return undefined
    this.bodySaved = roundBody(this.story.body)
    return this.bodySaved
  }

  private humPool(): number {
    return humPool(this.night)
  }

  // Each frame of play: the body changes, a breath is drawn, the HUD follows
  private stepBodyFrame(dt: number): void {
    const st = this.story
    if (!st) return
    st.body = stepBody(st.body, dt)
    // S9: the clock runs; dawn and dusk change the light, a new night the moons
    st.clock = stepClock(st.clock, dt)
    const day = isDay(st.clock)
    if (day !== this.storyDay) {
      this.setLighting(day ? 'day' : 'night')
      if (this.storyDay !== null && !this.slept) this.say(day ? 'Dawn.' : 'Dusk. The light goes out of the fields; night is when the Hollowed walk.')
      this.storyDay = day
      this.slept = false
    }
    if (nightOf(st.clock) !== this.night) this.setNight(nightOf(st.clock))
    if (this.breath.active) {
      const moving = this.path.length > 0 || !!this.combat || !this.nearHearth()
      if (moving) {
        this.breath.stop()
        this.say('The stillness breaks.')
      } else if (this.breath.step(dt)) {
        st.body = breathe(st.body, this.humPool())
        if (!st.done.stillness) {
          this.reach('stillness')
          this.say('The bottom of a still pool. Something hums under your breastbone and stays: the Hum. Each breath adds to it, as far as the sky allows.')
        }
        if (st.body.hum >= this.humPool()) {
          this.breath.stop()
          this.say(`The Hum is full: ${st.body.hum} tonight, by the moons. Spend it in a fight with the old cadence: 1 Strike, 2 Pivot, 3 Guard, then 1.`)
        }
      }
    }
    const near = this.nearHearth()
    if (near !== this.wasNearHearth) {
      this.wasNearHearth = near
      this.updateHint()
    }
    this.bodyHud.show(st.done.herbal ? st.body : null, this.humPool(), clockText(st.clock))
  }

  // B by a hearth: stillness, breath by breath
  private startStillness(): void {
    const st = this.story
    if (!st || !st.done.herbal) return
    if (this.breath.active) {
      this.breath.stop()
      return
    }
    if (this.combat) return this.flash('Not in a fight')
    if (!this.nearHearth()) return this.flash('Stillness needs a quiet place: stand by a hearth')
    if (st.body.hum >= this.humPool()) return this.flash('The Hum is already full')
    this.path = []
    if (!st.done.stillness) this.say('From the heavy book on the shelf: breathe in for four, out for six, and sink to the bottom of a still pool.')
    this.breath.start()
  }

  // C by a hearth: cook a hot dish from the herbal's herbs
  private openCooking(): void {
    const st = this.story
    if (!st || !st.done.herbal) return
    if (this.cooking?.isConnected) {
      this.cooking.remove()
      this.cooking = null
      return
    }
    if (!this.nearHearth()) return this.flash('Stand by a hearth to cook')
    this.cooking = showCooking(this.container, st.items, (d) => {
      this.cooking = null
      if ((st.items[d.herb] ?? 0) < 1) return
      st.items[d.herb]--
      st.body = cook(st.body, d)
      const p = this.player()
      st.done.cook = true
      this.opts.onStory?.({ step: 'cook', area: this.area.id, x: round1(p?.x ?? 0), z: round1(p?.z ?? 0), use: d.herb, ...this.stateToSave() })
      this.showStory()
      this.say(`${d.title}. ${d.line}`)
    })
  }

  // A fight's cadence spends a Hum, once stillness has been learned
  private cadenceAllowed(): boolean {
    const st = this.story
    if (!st || !st.done.stillness) return !st // outside the story, as before
    return st.body.hum >= 1
  }

  private showStory(): void {
    this.storyPanel.show(this.story ? storySteps(this.story.done) : [], !!this.story)
  }

  // A line of the story at the top of the view for a while
  private say(text: string): void {
    if (!text) return
    this.caption.textContent = text
    this.caption.style.display = 'block'
    const shown = text
    setTimeout(() => {
      if (this.caption.textContent === shown) this.caption.style.display = 'none'
    }, 9000)
  }

  // The Hollowed wait unseen in the story's area until the drag marks are
  // followed, and are gone once the fight is won
  private foesHidden(): boolean {
    const st = this.story
    if (!st || this.area.id !== st.area) return false
    return !st.done.trail || !!st.done.fight
  }

  // The talk ended: with the farmer, the first howl follows
  private talkEnded(): void {
    const was = this.talkingWith
    this.talkingWith = ''
    this.updateHint()
    if (!this.story || was !== 'farmer' || this.story.done.farmer) return
    this.reach('farmer')
    setTimeout(() => {
      if (!this.story || this.story.done.howl) return
      howl()
      this.reach('howl')
      this.say('A howl, long and low, out by the well. Then a dog barking, cut off. Next door, the Brannocks\' hearth has gone dark.')
      this.drawMarks()
    }, 2500)
  }

  // The raid signature by the well and the drag marks to the barn's door,
  // in the story's area once the howl is heard
  private drawMarks(): void {
    this.marks.clear()
    this.drawHerbs()
    const st = this.story
    if (!st || this.area.id !== st.area || !st.done.howl) return
    const c = this.set.cell
    const yard = Object.values(this.area.pieces).find((p) => p.piece === 'yard')
    const barn = Object.values(this.area.pieces).find((p) => p.piece === 'barn')
    this.signature = yard ? { x: (yard.x + 1.5) * c - 5, z: (yard.z + 1.5) * c } : null
    this.barnDoor = barn ? (this.doors().find((d) => d.id === barn.id)?.at ?? null) : null
    if (!this.signature) return
    const scorch = new THREE.Mesh(new THREE.RingGeometry(0.9, 2.2, 28).rotateX(-Math.PI / 2), new THREE.MeshBasicNodeMaterial({ color: 0x1b1612, transparent: true, opacity: 0.85 }))
    scorch.position.set(this.signature.x, 0.05, this.signature.z)
    this.marks.add(scorch)
    const lantern = new THREE.Mesh(new THREE.BoxGeometry(0.3, 0.4, 0.3), new THREE.MeshStandardNodeMaterial({ color: 0xffb347, emissive: 0x6a3a10 }))
    lantern.position.set(this.signature.x + 1.2, 0.2, this.signature.z + 0.6)
    lantern.rotation.z = 1.2
    this.marks.add(lantern)
    if (!this.barnDoor) return
    const furrow = new THREE.MeshBasicNodeMaterial({ color: 0x241c15, transparent: true, opacity: 0.8 })
    for (const m of dragMarks(this.signature, this.barnDoor, hash(st.area))) {
      for (const off of [-0.45, 0, 0.45]) {
        const g = new THREE.Mesh(new THREE.PlaneGeometry(0.14, 1.1).rotateX(-Math.PI / 2), furrow)
        g.position.set(m.x + Math.cos(m.heading) * off, 0.04, m.z - Math.sin(m.heading) * off)
        g.rotation.y = m.heading
        this.marks.add(g)
      }
    }
  }

  // S7: the herbs by the trees, in the story's area, from the first morning until gathered
  private drawHerbs(): void {
    this.herbs.clear()
    const st = this.story
    if (!st || this.area.id !== st.area || !st.done.morning) return
    for (const h of herbSpots(this.area.pieces, this.set.cell)) {
      if (st.picked[h.spot]) continue
      const g = new THREE.Group()
      const mat = new THREE.MeshStandardNodeMaterial({ color: h.herb.color, roughness: 0.9 })
      for (let i = 0; i < 6; i++) {
        const a = (i / 6) * Math.PI * 2
        const leaf = new THREE.Mesh(new THREE.SphereGeometry(0.18, 6, 4), mat)
        leaf.position.set(Math.cos(a) * 0.25, 0.15 + (i % 2) * 0.12, Math.sin(a) * 0.25)
        g.add(leaf)
      }
      g.position.set(h.x, 0, h.z)
      g.userData.spot = h.spot
      this.herbs.add(g)
    }
  }

  // K: the herbal, once something is gathered; reading it after foraging is a step
  private openHerbal(): void {
    const st = this.story
    if (!st) return
    if (this.herbal?.isConnected) {
      this.herbal.remove()
      this.herbal = null
      return
    }
    this.herbal = showHerbal(this.container, st.items)
    if (st.done.forage) this.reach('herbal')
  }

  // Each frame of the first hour: the signature found, the trail followed,
  // and the place saved now and then
  private stepStory(): void {
    const st = this.story!
    const p = this.player()
    if (!p) return
    // S7: the first morning, once play has reached where the player rested
    if (st.done.rest && !st.done.morning && !this.resumeAt) {
      const card = drawCard(hash(st.area) + 1)
      st.card = card.id
      this.reach('morning')
      showCard(this.container, card)
      this.say(`Morning. Frost on the sill, and the day's card: ${card.title}. Out by the farmstead's trees, the herbs are up.`)
      this.drawMarks()
    }
    if (this.area.id === st.area && st.done.morning && !st.done.forage) {
      const spots = herbSpots(this.area.pieces, this.set.cell)
      for (const h of spots) {
        if (st.picked[h.spot] || Math.hypot(p.x - h.x, p.z - h.z) > 2) continue
        st.picked[h.spot] = true
        st.items[h.herb.id] = (st.items[h.herb.id] ?? 0) + 1
        this.opts.onStory?.({ step: '', area: this.area.id, x: round1(p.x), z: round1(p.z), item: h.herb.id, spot: h.spot })
        this.say(`${h.herb.title}. You gather a handful.`)
        this.drawHerbs()
        if (spots.every((x) => st.picked[x.spot])) {
          this.reach('forage')
          this.say('All three. Press K to write them in the herbal.')
        }
      }
    }
    if (this.area.id === st.area && st.done.howl) {
      if (!st.done.signature && this.signature && Math.hypot(p.x - this.signature.x, p.z - this.signature.z) < 4) {
        this.reach('signature')
        this.say(SIGNATURE_TEXT)
      } else if (st.done.signature && !st.done.trail && this.barnDoor && Math.hypot(p.x - this.barnDoor.x, p.z - this.barnDoor.z) < 10) {
        this.reach('trail')
        this.syncNpcs(true)
        this.startCombat()
        this.say('The furrows end at the barn. Two of them, hunched in the snow, turn toward you. Ember eyes.')
      }
    }
    this.savePlace()
  }

  // The hearth of an interior the character stands by
  private nearHearth(): boolean {
    const p = this.player()
    if (!p || !this.set.interior) return false
    const c = this.set.cell
    return Object.values(this.area.pieces).some((pl) => pl.piece === 'hearth' && Math.hypot((pl.x + 1) * c - p.x, (pl.z + 0.5) * c - p.z) < 3.5)
  }

  // R by Hawk's Rest's hearth: rest, and draw the first card
  private rest(): void {
    const st = this.story
    if (st?.done.rest) return this.sleepHere()
    if (!st || !st.done.road || st.done.rest) return
    if (!this.nearHearth()) {
      this.flash('Stand by the hearth to rest')
      return
    }
    const card = drawCard(hash(st.area))
    st.card = card.id
    this.reach('rest')
    showCard(this.container, card)
    this.say('Stillness. The cottage settles around you, and your hand finds the old deck on the shelf. The first hour is over; sleep until morning.')
    setTimeout(() => {
      if (this.story === st) st.clock = sleep(st.clock)
    }, 4000)
  }

  // S9: R by a hearth once night is falling: sleep until seven
  private sleepHere(): void {
    const st = this.story!
    if (!this.nearHearth()) return this.flash('Stand by a hearth to sleep')
    if (!canSleep(st.clock)) return this.flash('Too early to sleep: night falls at 19:00')
    st.clock = sleep(st.clock)
    this.slept = true
    const card = drawCard(hash(st.area) + st.clock.day)
    st.card = card.id
    if (st.done.almanac) this.reach('sleep')
    else this.savePlace(true)
    const fade = document.createElement('div')
    fade.style.cssText = 'position:absolute;inset:0;background:#000;opacity:1;transition:opacity 2s;z-index:3;pointer-events:none'
    this.container.appendChild(fade)
    setTimeout(() => (fade.style.opacity = '0'), 600)
    setTimeout(() => fade.remove(), 2800)
    showCard(this.container, card)
    this.say(`You sleep by the hearth. Day ${st.clock.day + 1}; the day's card: ${card.title}.`)
  }

  // S9: M, the moon almanac
  private openAlmanac(): void {
    const st = this.story
    if (!st || !st.done.rest) return
    if (this.almanacEl?.isConnected) {
      this.almanacEl.remove()
      this.almanacEl = null
      return
    }
    this.almanacEl = showAlmanac(this.container, st.clock)
    if (st.done.cook) this.reach('almanac')
  }

  private flash(text: string): void {
    this.hint.textContent = text
    setTimeout(() => this.updateHint(), 1500)
  }

  private step(dt: number): void {
    if (this.playing) this.stepCharacter(dt)
    this.loopTime += dt
    if (this.playing && this.combat) this.stepCombat(Math.min(dt, 0.1))
    if (this.playing && this.hearth) this.stepHearth(dt)
    if (this.playing && this.story) this.stepStory()
    if (this.playing && this.story) this.stepBodyFrame(dt)
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
      : this.playing && this.story && !this.combat
      ? `The story · click to walk, talk or go through a door · J hides the steps${this.nearHearth() && this.story.done.road && !this.story.done.rest ? ' · R to rest by the hearth' : ''}${this.story.done.morning ? ' · K: the herbal' : ''}${this.story.done.herbal && this.nearHearth() ? ' · B: stillness · C: cook' : ''}${this.story.done.rest ? ' · M: the almanac' : ''}${this.story.done.rest && this.nearHearth() && canSleep(this.story.clock) ? ' · R: sleep' : ''}`
      : this.playing && this.combat
      ? 'Fighting · click a Hollowed to attack it · 1 Strike · 2 Pivot · 3 Guard · click the ground to back away'
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
  return { farmer: 0x7a6a3a, wife: 0x6b3b4a, child: 0x4a6b5a, hollowed: 0x6f6a64 }[n.kind ?? ''] ?? 0x8a5a3a
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
