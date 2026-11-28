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
import { HearthNight, hearthOf, WATCHER_HP } from '../defence/HearthNight.ts'
import { NightHud } from '../defence/NightHud.ts'
import { whistle } from '../defence/Whistle.ts'
import { DialoguePanel } from './DialoguePanel.ts'
import { FirstHourPanel } from '../objectives/FirstHour.ts'
import { JournalScene, journalEntries, link, nextNoMoon } from '../story/Journal.ts'
import { edgePoint, SensesLayer, senseCues, spread, type Source } from '../story/Senses.ts'
import { dragMarks, drawCard, herbSpots, howl, nextStep, showCard, showHerbal, SIGNATURE_TEXT, storySteps } from '../story/FirstHourStory.ts'
import { dayCard, NO_EFFECT, spendWith, cookWith, type CardEffect } from '../story/DayCard.ts'
import { aching, dreams, FORD_DREAM, remembered, showDream } from '../story/Remember.ts'
import { companionStep, VETERAN } from '../story/Companion.ts'
import { CACHE_LINE, CACHE_NOTES, CACHE_REACH, cacheReady, cacheSpot, POULTICE } from '../story/Cache.ts'
import { COUNCIL_LINE, COUNCIL_LINES, councilReady, isHawksRest, REEVE_LINE, REEVE_LINES, reeveReady } from '../story/Council.ts'
import { afterJourney, ARRIVE_LINE, isAldermere, journeyReady, LEGS } from '../story/Journey.ts'
import { atGate, gateSpot, PALACE_LINE, PALACE_LINES, palaceReady } from '../story/Palace.ts'
import { NORTH_LINE, NORTH_SENSE, northRoadReady, waymarkAt, waymarkSpots } from '../story/NorthRoad.ts'
import { TOLD_LINE, ASHBY_LINE, ASHBY_SENSE, ashbyReady, isAshby, remnantAt, remnantSpots, TRACK_LINE, TRACK_REACH, TRACK_SENSE, trackPrints, trackReady } from '../story/Ashby.ts'
import { WELL_LINE, WELL_REACH, WELL_SENSE, WELL_WARD_LINE, wellReady, wellSpot } from '../story/Well.ts'
import { bankSageReady, bankSageSpot, FARM_WARD_LINE, FARM_WARDED_NIGHT, HEARTH_REACH, HEARTH_WARD_LINE, hearthStonesReady, hearthWarded, hearthWardMissing, hearthWardReady, nextHearthWard, SAGE_LINE, SAGE_REACH, wardSpots } from '../story/HearthWard.ts'
import { TRIED_DOOR_LINE, TRIED_LINE, TRIED_LOST, TRIED_NIGHT, triedReady } from '../story/Tried.ts'
import { riverStonesReady, STONES_LINE, STONES_REACH, thresholdSpot, WARD_LINE, WARD_REACH, wardedNight, wardMissing, wardReady } from '../story/Ward.ts'
import { TINCTURE, TINCTURE_ALSO, tinctureReady, TRUE_DREAM, TRUE_DREAM_LINE } from '../story/Tincture.ts'
import { BUNDLE_HERBS, BUNDLE_REACH, bundleSpots, bundlesReady, HEDGE_DISHES, SILL_REACH, sillLine, sillSpot } from '../story/Bundles.ts'
import { HEART, nextWaking, ringSpot, STARLORE_LINE, starlorePool, stonePoints, STONES_MEMORY, stonesWake } from '../story/Stones.ts'
import { clarity, ditchSpot, ReadingScene, wenlanAnswer, type Told } from '../story/Reading.ts'
import { canSleep, CLOCK_START, clockText, FIRST_NIGHT, isDay, nightOf, showAlmanac, sleep, stepClock, type Clock } from '../story/Clock.ts'
import { BODY_START, BodyHud, breathe, BreathPrompt, DISHES, humPool, rounded as roundBody, showCooking, stepBody, type Body } from '../story/Hum.ts'
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
  // S14: the story's foretold night has come: the host starts the night of hearth defence at the farmhouse
  onStoryNight?: () => void
  // A person was selected in the editor (null: none)
  onNpcSelect?: (id: string | null) => void
  // S4: the weapon in hand in play (WEAPONS: sword, pitchfork) and the character's skills
  weapon?: string
  skills?: Skills
  // S6: a step of the first hour reached (step empty: only the place, saved
  // now and then); the host sends it as game.reachStory to the story's area.
  // S7: item and spot, a herb gathered
  onStory?: (s: { step: string; area: string; x: number; z: number; item?: string; spot?: string; use?: string; told?: Told; body?: Body; clock?: Clock }) => void
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
// S15: the village remembers the foretold night
const KNOT_CHILL = 0.75 // the Chill a Hum leaves, carrying the knot of the last sheaf
const KNOT_LINE = 'While you carry it, spending the Hum chills you a quarter less.'
const VILLAGE_LINE = "Hawk's Rest knows you now. Someone has left a loaf and a cup of milk on the cottage step, and the woman at the well nods as you pass: \"You're the one who held Harrow's house.\""

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
  // S10: the reading for Wenlan while it is shown
  reading: ReadingScene | null = null
  private wasNearHearth = false
  private bodySaved: Body | null = null
  private readonly herbs = new THREE.Group()
  private herbal: HTMLDivElement | null = null
  private journal: JournalScene | null = null // S13: the notes
  private senses: SensesLayer | null = null // S12: the smells and sounds at the screen's edge
  private focus = false // S12: F held
  private readonly storyPanel: FirstHourPanel
  private readonly marks = new THREE.Group()
  private readonly stones = new THREE.Group() // S18: the Nine Sisters
  private stonesAwake: boolean | null = null
  private ringHeart: { x: number; z: number } | null = null
  private ringVisited = false
  private cacheOpen = false // S19: the cache's notes are being read
  private atSill = false // S20: standing by the moon calendar
  private tincture = false // S21: the dream tincture drunk, for the next sleep
  private atThreshold = false // S22: standing at Hawk's Rest's door
  private atHearthStone = false // S24: standing on Hawk's Rest's hearthstone
  private signature: Point | null = null
  private barnDoor: Point | null = null
  private resumeAt: { area: string; at: Point } | null = null
  private via = ''
  private talkingWith = ''
  private welcomed = false // S15: Hawk's Rest's welcome said this session
  // S17: the first companion, walking with the character once joined
  private companion: { model: CharacterModel; at: { x: number; z: number }; cooldown: number } | null = null
  private ateHot = false // S16: something hot eaten since the last sleep (a warm night heals a dream wound)
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
    this.scene.add(this.ash, this.marks, this.herbs, this.stones)
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
    canvas.addEventListener('keyup', this.onKeyUp)
    canvas.addEventListener('blur', this.onKeyUp)
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
    // S15: after the foretold night Wenlan has his thanks to give
    // S30: back from Ashby, he hears what is left of it
    const ashby = n?.dialogue === 'farmer' && this.story?.done.ashby && !this.story.done.told ? this.dialogues['farmer-ashby'] : undefined
    const after = ashby ?? (n?.dialogue === 'farmer' && this.story?.done.defended ? this.dialogues['farmer-thanks'] : undefined)
    const d = after ?? (n?.dialogue ? this.dialogues[n.dialogue] : undefined)
    if (!n || !d) return false
    if (d.id === 'farmer' && this.story?.done.sleep && !this.story.done.reading) return this.openReading('Wenlan Harrow')
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
    if (this.companion) this.companion.at = { x: at.x - 1, z: at.z - 1 }
    this.orbit.target.set(at.x, 0, at.z)
    this.orbit.distance = this.set.interior ? 16 : 32
    this.grid.visible = false
    this.startCombat()
    this.drawMarks()
    const bySign = this.via === 'signpost'
    if (bySign) this.reach('road')
    this.via = ''
    // S15: word of the night has gone up the road to Hawk's Rest ahead of you
    const st = this.story
    if (st?.done.defended && bySign && !this.welcomed) {
      this.welcomed = true
      this.say(VILLAGE_LINE)
    }
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
      this.stones.clear()
      this.stonesAwake = null
      this.herbal?.remove()
      this.herbal = null
      this.senses?.dispose()
      this.senses = null
      this.journal?.el.remove()
      this.journal = null
      this.focus = false
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
    this.renderer.canvas.removeEventListener('keyup', this.onKeyUp)
    this.renderer.canvas.removeEventListener('blur', this.onKeyUp)
    this.senses?.dispose()
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
        skills: remembered(this.skills, this.story?.done ?? {}),
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
    const k = remembered(this.skills, this.story?.done ?? {})
    const skill = c.weapon.kind === 'polearm' ? `polearm ${k.polearm}` : `blade ${k.blade}`
    this.hud.update(c.hp, HERO_HP, c.stance, c.weapon.title, `${skill}, reading ${k.reading}`)
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
          this.story.body = spendWith(this.story.body, this.dayEffect())
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

  // S12: F held is focus, which sharpens the senses
  private onKeyUp = (e: Event) => {
    if (!this.focus || (e instanceof KeyboardEvent && e.key !== 'f' && e.key !== 'F')) return
    this.focus = false
    this.updateHint()
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
      if (this.story && (e.key === 'n' || e.key === 'N')) this.openJournal()
      if (this.story && (e.key === 'f' || e.key === 'F') && !this.focus) {
        this.focus = true
        this.updateHint()
      }
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
    const speed = 4 * this.dayEffect().speed
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
    // S25: a warded hearth lets nothing through
    const warded = !!this.story && hearthWarded(this.story.done, house)
    this.hearth = new HearthNight(house, { seed: opts.seed ?? hash(house.id + house.version), length: opts.length, raiders: opts.raiders, cell: opts.cell ?? 2, warded })
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
        case 'settled':
          this.nightHud.say(FARM_WARDED_NIGHT)
          this.hud.say(FARM_WARDED_NIGHT)
          break
        case 'taken':
          this.nightHud.say(`The whistle stops. ${h.watcher ? `${h.watcher.name} is gone, and` : 'Nobody kept the watch, and'} the family with them, dragged under the stone.`)
          if (this.story && triedReady(this.story.done) && h.house !== this.story.area && this.story.done.defended) this.say(TRIED_LOST)
          else if (this.story?.done.watch && !this.story.done.defended) this.say('The house was lost. Wenlan will not forgive it, but the dark comes round again: by a hearth, press R to wait for the next night with no moon.')
          else this.flash('The house was lost. Press Stop to try the night again.')
          break
        case 'dawn':
          this.nightHud.say('Dawn greys the shutters. The house held.')
          this.flash('The house held until dawn')
          // S23: the night the ward was tried, held
          if (this.story && triedReady(this.story.done) && h.house !== this.story.area && this.story.done.defended) {
            this.reach('tried')
            this.say(TRIED_LINE)
          }
          // S14: the foretold night, survived
          if (this.story?.done.watch && !this.story.done.defended) {
            this.reach('defended')
            this.say('Dawn. The hearthstone lies still, and the Harrows are alive, every one. Wenlan grips your hand: "My father said the eyeless ones were a tale. You came back for us. Come and find me in the kitchen when you\'ve slept."')
          }
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
    return humPool(this.night) + (this.story ? this.dayEffect().hum + starlorePool(this.night, !!this.story.items.starlore) : 0)
  }

  // S11: the day's card and its light buff or warning (none before the first morning)
  private dayEffect(): CardEffect {
    const st = this.story
    const e = (st && dayCard(hash(st.area), st.clock.day)?.effect) || NO_EFFECT
    // S15: the knot of the last sheaf keeps the cold out of the hands
    return st?.items.knot ? { ...e, chill: e.chill * KNOT_CHILL } : e
  }

  // Each frame of play: the body changes, a breath is drawn, the HUD follows
  private stepBodyFrame(dt: number): void {
    const st = this.story
    if (!st) return
    st.body = aching(stepBody(st.body, dt), !!st.items.wound) // S16: a dream wound keeps a little Chill
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
    const today = dayCard(hash(st.area), st.clock.day)
    this.bodyHud.show(st.done.herbal ? st.body : null, this.humPool(), clockText(st.clock) + (today && st.done.morning ? ` · ${today.card.title}: ${today.effect.line}` : ''))
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
      st.body = cookWith(st.body, d, this.dayEffect())
      // S19: the yarrow poultice closes the dream's wound at once
      const closes = d.id === POULTICE.id && !!st.items.wound
      if (closes) st.items.wound = 0
      // S21: the tincture takes lavender too, and waits for the night's sleep
      const tincture = d.id === TINCTURE.id && (st.items[TINCTURE_ALSO] ?? 0) >= 1
      if (tincture) {
        st.items[TINCTURE_ALSO]--
        this.tincture = true
      }
      const p = this.player()
      st.done.cook = true
      this.ateHot = true
      this.opts.onStory?.({ step: 'cook', area: this.area.id, x: round1(p?.x ?? 0), z: round1(p?.z ?? 0), use: d.herb, ...this.stateToSave() })
      if (closes) this.opts.onStory?.({ step: '', area: this.area.id, x: round1(p?.x ?? 0), z: round1(p?.z ?? 0), use: 'wound', ...this.stateToSave() })
      if (tincture) this.opts.onStory?.({ step: '', area: this.area.id, x: round1(p?.x ?? 0), z: round1(p?.z ?? 0), use: TINCTURE_ALSO, ...this.stateToSave() })
      this.showStory()
      this.say(`${d.title}. ${d.line}${closes ? ' The ache in your side is gone.' : ''}`)
    }, st.items.hedgecraft ? [...DISHES, POULTICE, ...HEDGE_DISHES, ...(tinctureReady(nightOf(st.clock), st.items, st.done) ? [TINCTURE] : [])] : DISHES)
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
    // S17: the veteran walks with you from now on
    if (this.story && was === VETERAN.id && !this.story.done.companion) {
      this.reach('companion')
      this.say(`${VETERAN.name}, ${VETERAN.title}, picks up his pike and falls in a step behind you. In a fight he takes the flank. On the road he nods at the rise by the signpost: "Nine stones up there. The old folk say they wake when the little moon is full."`)
      return
    }
    // S30: word of Ashby goes up the road to Hawk's Rest
    if (this.story && was === 'farmer-ashby' && !this.story.done.told) {
      this.reach('told')
      this.say(TOLD_LINE)
      return
    }
    // S15: the knot of the last sheaf, Wenlan's thanks
    if (this.story && was === 'farmer-thanks' && !this.story.done.thanked) {
      this.story.items.knot = 1
      this.reach('thanked')
      this.say(`You turn the straw knot over in your fingers. It is warm, as if it had been by the fire all night. ${KNOT_LINE}`)
      return
    }
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
    this.drawStones()
    const st = this.story
    // S26: frost on the well's stones once the hearths are shut
    const well = st && this.area.id === st.area && st.done.farmward && !this.set.interior ? wellSpot(Object.values(this.area.pieces), this.set.cell) : null
    if (well && !st?.done.wellward) {
      const frost = new THREE.Mesh(new THREE.RingGeometry(1.25, 1.9, 24).rotateX(-Math.PI / 2), new THREE.MeshBasicNodeMaterial({ color: 0xdfeaf2, transparent: true, opacity: 0.7 }))
      frost.position.set(well.x, 0.06, well.z)
      this.marks.add(frost)
    } else if (well) {
      // S27: the well ward: sage round the rim, a stone at each quarter and one on the beam
      const sage = new THREE.Mesh(new THREE.TorusGeometry(1.25, 0.06, 6, 24).rotateX(Math.PI / 2), new THREE.MeshStandardNodeMaterial({ color: 0x8fa38a, roughness: 1 }))
      sage.position.set(well.x, 1.02, well.z)
      this.marks.add(sage)
      const pale = new THREE.MeshStandardNodeMaterial({ color: 0xe6e1d3, roughness: 0.6 })
      for (const [dx, dy, dz] of [[1.25, 1.06, 0], [-1.25, 1.06, 0], [0, 1.06, 1.25], [0, 1.06, -1.25], [0, 2.7, 0]]) {
        const m = new THREE.Mesh(new THREE.SphereGeometry(0.1, 8, 6), pale)
        m.position.set(well.x + dx, dy, well.z + dz)
        this.marks.add(m)
      }
    }
    this.drawRoad()
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
    this.drawBundles()
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
      if (this.dayEffect().reveal) this.herbs.add(this.glowAt(h.x, h.z))
    }
    // S22: the river stones in the cleared culvert, once the true dream has taught the ward
    // S24: and again for the hearth ward, once the tried night taught it
    const rs = riverStonesReady(st.done, st.items) || hearthStonesReady(st.done, st.items) ? ditchSpot(this.area.pieces, this.set.cell, this.fieldSize()) : null
    if (rs) {
      const pale = new THREE.MeshStandardNodeMaterial({ color: 0xe6e1d3, roughness: 0.6 })
      for (let i = 0; i < 5; i++) {
        const m = new THREE.Mesh(new THREE.SphereGeometry(0.13, 8, 6), pale)
        m.scale.y = 0.55
        m.position.set(rs.x - 0.5 + i * 0.25, 0.06, rs.z + ((i * 7) % 3) * 0.12 - 0.12)
        this.herbs.add(m)
      }
      if (this.dayEffect().reveal) this.herbs.add(this.glowAt(rs.x, rs.z))
    }
    // S24: the sage seeded on the ditch bank
    const bank = bankSageReady(st.done, st.items, st.picked) ? ditchSpot(this.area.pieces, this.set.cell, this.fieldSize()) : null
    if (bank) {
      const at = bankSageSpot(bank)
      const clump = new THREE.Mesh(new THREE.ConeGeometry(0.22, 0.45, 7), new THREE.MeshStandardNodeMaterial({ color: 0x8fa38a, roughness: 1 }))
      clump.position.set(at.x, 0.22, at.z)
      this.herbs.add(clump)
      if (this.dayEffect().reveal) this.herbs.add(this.glowAt(at.x, at.z))
    }
    // S10: the willow bough in the culvert and the water backed up into the wheat, until cleared
    const ditch = st.done.reading && !st.done.ditch ? ditchSpot(this.area.pieces, this.set.cell, this.fieldSize()) : null
    if (ditch) {
      const flood = new THREE.Mesh(new THREE.PlaneGeometry(ditch.field.w - 1, ditch.field.d - 1.6).rotateX(-Math.PI / 2), new THREE.MeshStandardNodeMaterial({ color: 0x4f86ad, transparent: true, opacity: 0.75, roughness: 0.1 }))
      flood.position.set(ditch.field.x, 0.62, ditch.field.z)
      this.herbs.add(flood)
      const bough = new THREE.Group()
      const bark = new THREE.MeshStandardNodeMaterial({ color: 0x5a4632, roughness: 1 })
      const leaves = new THREE.MeshStandardNodeMaterial({ color: 0x8aa35a, roughness: 0.9 })
      const limb = new THREE.Mesh(new THREE.CylinderGeometry(0.12, 0.18, 3, 6).rotateZ(Math.PI / 2), bark)
      limb.position.y = 0.2
      bough.add(limb)
      for (let i = 0; i < 7; i++) {
        const leaf = new THREE.Mesh(new THREE.SphereGeometry(0.28, 6, 4), leaves)
        leaf.position.set(-1.2 + i * 0.4, 0.35 + (i % 2) * 0.15, (i % 3) * 0.15 - 0.15)
        bough.add(leaf)
      }
      bough.position.set(ditch.x, 0, ditch.z)
      bough.rotation.y = 0.5
      bough.userData.ditch = true
      this.herbs.add(bough)
      if (this.dayEffect().reveal) this.herbs.add(this.glowAt(ditch.x, ditch.z))
    }
  }

  // S10: Wenlan's reading, once the first day is over: three cards, then what to tell him
  private openReading(who: string): boolean {
    const st = this.story
    if (!st || this.reading?.el.isConnected) return true
    this.caption.style.display = 'none'
    this.reading = new ReadingScene(this.container, who, clarity(nightOf(st.clock), !!st.done.stillness), (told) => {
      this.reading = null
      st.done.reading = true
      const p = this.player()
      this.opts.onStory?.({ step: 'reading', area: this.area.id, x: round1(p?.x ?? 0), z: round1(p?.z ?? 0), told, ...this.stateToSave() })
      this.showStory()
      this.updateHint()
      this.say(wenlanAnswer(told))
    })
    return true
  }

  // S11: the Lantern's glow over something hidden: a soft column of light
  private glowAt(x: number, z: number): THREE.Mesh {
    const m = new THREE.Mesh(new THREE.CylinderGeometry(0.35, 0.6, 6, 12, 1, true), new THREE.MeshBasicNodeMaterial({ color: 0xffd98a, transparent: true, opacity: 0.28, depthWrite: false, side: THREE.DoubleSide }))
    m.position.set(x, 3, z)
    m.userData.glow = true
    return m
  }

  // The field piece's size in cells, east and south (the bough's corner)
  private fieldSize(): [number, number] {
    const s = this.set.pieces.find((p) => p.id === 'field')?.size
    return s ? [s[0], s[1]] : [1, 2]
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
    this.herbal = showHerbal(this.container, st.items, st.done.cache ? BUNDLE_HERBS : [])
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
    // S22: the river stones, taken by walking up to the culvert
    const again = hearthStonesReady(st.done, st.items)
    if (this.area.id === st.area && (riverStonesReady(st.done, st.items) || again)) {
      const d = ditchSpot(this.area.pieces, this.set.cell, this.fieldSize())
      if (d && Math.hypot(p.x - d.x, p.z - d.z) < STONES_REACH) {
        st.items.riverstones = 1
        this.opts.onStory?.({ step: '', area: this.area.id, x: round1(p.x), z: round1(p.z), item: 'riverstones', spot: again ? wardSpots(nextHearthWard(st.done)!).stones : 'culvert-stones' })
        this.drawHerbs()
        this.say(STONES_LINE)
      }
    }
    // S24: the sage on the ditch bank, taken by walking up to it
    if (this.area.id === st.area && bankSageReady(st.done, st.items, st.picked)) {
      const d = ditchSpot(this.area.pieces, this.set.cell, this.fieldSize())
      const at = d ? bankSageSpot(d) : null
      if (at && Math.hypot(p.x - at.x, p.z - at.z) < SAGE_REACH) {
        const spot = wardSpots(nextHearthWard(st.done)!).sage
        st.items.sage = (st.items.sage ?? 0) + 1
        st.picked[spot] = true
        this.opts.onStory?.({ step: '', area: this.area.id, x: round1(p.x), z: round1(p.z), item: 'sage', spot })
        this.drawHerbs()
        this.say(SAGE_LINE)
      }
    }
    // S10: the willow bough the cards showed, hauled out by walking up to it
    if (this.area.id === st.area && st.done.reading && !st.done.ditch) {
      const d = ditchSpot(this.area.pieces, this.set.cell, this.fieldSize())
      if (d && Math.hypot(p.x - d.x, p.z - d.z) < 2.2) {
        st.items.silver = (st.items.silver ?? 0) + 1
        this.reach('ditch')
        this.drawHerbs()
        this.say('You haul the willow bough out of the culvert. The water goes with a gulp and a gurgle, and the wheat stands up out of it. Wenlan presses a silver into your hand: "The cards were right."')
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
    this.stepStones(p)
    this.stepCache(p)
    this.stepBundles(p)
    this.stepFarmWard(p)
    this.stepWell(p)
    this.stepRoad(p)
    this.stepJourney()
    this.stepPalace(p)
    this.stepSenses(p)
    this.savePlace()
  }

  // S19: the hedge-witch's cache by the box bed at Hawk's Rest, once the stones have remembered
  private stepCache(p: { x: number; z: number }): void {
    const st = this.story!
    if (!this.blessed() || !cacheReady(st.done) || this.cacheOpen) return
    const c = cacheSpot(Object.values(this.area.pieces), this.set.cell)
    if (!c || Math.hypot(p.x - c.x, p.z - c.z) > CACHE_REACH) return
    this.cacheOpen = true
    this.path = []
    showDream(this.container, CACHE_NOTES, () => {
      this.cacheOpen = false
      st.items.hedgecraft = 1
      this.reach('cache')
      this.drawHerbs()
      this.say(CACHE_LINE)
    })
  }

  // S20: the hedge-witch's bundles over the shelves, once her notes are
  // found, and her moon calendar on the sill
  private drawBundles(): void {
    const st = this.story
    // S25: Wenlan's hearth ward, drawn in his kitchen
    if (st?.done.farmward && this.inKitchen()) this.drawHearthWard()
    if (!st || !this.blessed()) return
    const pieces = Object.values(this.area.pieces)
    const sill = sillSpot(pieces, this.set.cell)
    if (sill) {
      const pebble = (c: number) => new THREE.MeshStandardNodeMaterial({ color: c, roughness: 0.8 })
      for (let i = 0; i < 7; i++) {
        const m = new THREE.Mesh(new THREE.SphereGeometry(0.06, 6, 4), pebble(i % 3 ? 0x9a9a92 : 0xeeeae0))
        m.scale.y = 0.6
        m.position.set(sill.x - 0.36 + i * 0.12, 1.05, sill.z - 0.3)
        this.herbs.add(m)
      }
    }
    // S22: the ward laid on the threshold: a line of sage and five stones
    const th = st.done.ward ? thresholdSpot(pieces, this.set.cell) : null
    if (th) {
      const sage = new THREE.Mesh(new THREE.BoxGeometry(1.2, 0.04, 0.08), new THREE.MeshStandardNodeMaterial({ color: 0x8fa38a, roughness: 1 }))
      sage.position.set(th.x, 0.03, th.z + 0.6)
      this.herbs.add(sage)
      const pale = new THREE.MeshStandardNodeMaterial({ color: 0xe6e1d3, roughness: 0.6 })
      for (const dx of [-0.7, -0.3, 0, 0.3, 0.7]) {
        const m = new THREE.Mesh(new THREE.SphereGeometry(0.09, 8, 6), pale)
        m.scale.y = 0.6
        m.position.set(th.x + dx, 0.05, th.z + 0.75)
        this.herbs.add(m)
      }
    }
    // S24: the hearth ward: sage round the hearthstone and a stone at each corner and the heart
    if (st.done.hearthward) this.drawHearthWard()
    if (!st.done.cache) return
    const string = new THREE.MeshBasicNodeMaterial({ color: 0x6b5236 })
    for (const b of bundleSpots(pieces, this.set.cell)) {
      if (st.picked[b.spot]) continue
      const g = new THREE.Group()
      const cord = new THREE.Mesh(new THREE.CylinderGeometry(0.01, 0.01, 0.4, 4), string)
      cord.position.y = 2.2
      const bunch = new THREE.Mesh(new THREE.ConeGeometry(0.16, 0.5, 7), new THREE.MeshStandardNodeMaterial({ color: b.herb.color, roughness: 1 }))
      bunch.position.y = 1.75
      g.add(cord, bunch)
      g.position.set(b.x, 0, b.z)
      g.userData.spot = b.spot
      this.herbs.add(g)
    }
  }

  // S24: the hearth ward on this house's hearthstone: sage round it, a stone at each corner and the heart
  private drawHearthWard(): void {
    const hs = hearthOf(this.area, this.set.cell)?.stone
    if (hs) {
      const sageMat = new THREE.MeshStandardNodeMaterial({ color: 0x8fa38a, roughness: 1 })
      const pale = new THREE.MeshStandardNodeMaterial({ color: 0xe6e1d3, roughness: 0.6 })
      for (const [dx, dz, w, d] of [[0, -0.85, 1.8, 0.07], [0, 0.85, 1.8, 0.07], [-0.9, 0, 0.07, 1.7], [0.9, 0, 0.07, 1.7]]) {
        const m = new THREE.Mesh(new THREE.BoxGeometry(w, 0.04, d), sageMat)
        m.position.set(hs.x + dx, 0.03, hs.z + dz)
        this.herbs.add(m)
      }
      for (const [dx, dz] of [[-0.75, -0.7], [0.75, -0.7], [-0.75, 0.7], [0.75, 0.7], [0, 0]]) {
        const m = new THREE.Mesh(new THREE.SphereGeometry(0.09, 8, 6), pale)
        m.scale.y = 0.6
        m.position.set(hs.x + dx, 0.05, hs.z + dz)
        this.herbs.add(m)
      }
    }
  }

  // S25: Wenlan's kitchen, the house with the farmer in it
  private inKitchen(): boolean {
    return !!this.set.interior && Object.values(this.area.npcs ?? {}).some((n) => n.dialogue === 'farmer')
  }

  // S25: standing on the farmhouse hearthstone with stones and a sage lays Wenlan's hearth ward
  private atFarmStone = false
  private stepFarmWard(p: { x: number; z: number }): void {
    const st = this.story!
    if (nextHearthWard(st.done) !== 'farmward' || !this.inKitchen()) return
    const hs = hearthOf(this.area, this.set.cell)?.stone
    const at = !!hs && Math.hypot(p.x - hs.x, p.z - hs.z) < HEARTH_REACH
    if (at && !this.atFarmStone) {
      if (hearthWardReady(st.done, st.items, 'farmward')) {
        st.items.sage--
        st.items.riverstones = 0
        st.items.farmward = 1
        this.reach('farmward')
        this.drawHerbs()
        this.say(FARM_WARD_LINE)
      } else this.say(hearthWardMissing(st.items))
    }
    this.atFarmStone = at
  }

  // S26: the frost-cold well in the yard, once both hearths are warded;
  // S27: standing at its rim with stones and sage wards it
  private atWell = false
  private stepWell(p: { x: number; z: number }): void {
    const st = this.story!
    if (this.set.interior || this.area.id !== st.area) return
    const ward = nextHearthWard(st.done) === 'wellward'
    if (!wellReady(st.done) && !ward) return
    const w = wellSpot(Object.values(this.area.pieces), this.set.cell)
    const at = !!w && Math.hypot(p.x - w.x, p.z - w.z) <= WELL_REACH
    if (at && !this.atWell) {
      if (wellReady(st.done)) {
        st.items.welllore = 1
        this.reach('well')
        this.drawMarks()
        this.say(WELL_LINE)
      } else if (hearthWardReady(st.done, st.items, 'wellward')) {
        st.items.sage--
        st.items.riverstones = 0
        st.items.wellward = 1
        this.reach('wellward')
        this.drawMarks()
        this.drawHerbs()
        this.say(WELL_WARD_LINE)
      } else this.say(hearthWardMissing(st.items, 'The well'))
    }
    this.atWell = at
  }

  // S28: the signpost's door, where the road leaves the farmstead
  private signDoor(): { x: number; z: number } | null {
    const sign = Object.values(this.area.pieces).find((p) => p.piece === 'signpost')
    if (!sign) return null
    return this.doors().find((d) => d.id === sign.id)?.at ?? { x: (sign.x + 0.5) * this.set.cell, z: (sign.z + 0.5) * this.set.cell }
  }

  // S28: frost prints from the warded well to the signpost; S29: what the
  // raid left in Ashby
  private ashbySeen = new Set<string>()
  private drawRoad(): void {
    const st = this.story
    if (!st || this.set.interior) return
    if (this.area.id === st.area && trackReady(st.done)) {
      const well = wellSpot(Object.values(this.area.pieces), this.set.cell)
      const sign = this.signDoor()
      if (!well || !sign) return
      const frost = new THREE.MeshBasicNodeMaterial({ color: 0xe4eef5, transparent: true, opacity: 0.75 })
      for (const [i, p] of trackPrints(well, sign).entries()) {
        const m = new THREE.Mesh(new THREE.CircleGeometry(0.22, 10).rotateX(-Math.PI / 2), frost)
        m.scale.set(0.7, 1, 1.4)
        m.rotation.y = i % 2 ? 0.2 : -0.2
        m.position.set(p.x, 0.05, p.z)
        this.marks.add(m)
      }
    }
    if (this.area.id !== st.area && isAshby(this.area.name) && st.done.track) {
      const wood = new THREE.MeshStandardNodeMaterial({ color: 0x5b4330, roughness: 1 })
      const dark = new THREE.MeshBasicNodeMaterial({ color: 0x15171c })
      const iron = new THREE.MeshStandardNodeMaterial({ color: 0x3a3a3e, roughness: 0.7 })
      for (const r of remnantSpots(this.area.width, this.area.depth, this.set.cell)) {
        const g = new THREE.Group()
        if (r.id === 'door') {
          const door = new THREE.Mesh(new THREE.BoxGeometry(1, 2, 0.08), wood)
          door.position.set(0.4, 1, 0.3)
          door.rotation.y = 1.1
          g.add(door)
          for (let k = 0; k < 3; k++) {
            const hand = new THREE.Mesh(new THREE.CircleGeometry(0.09, 6), dark)
            hand.position.set(0.05, 0.6 + k * 0.45, 0.35 + (k % 2) * 0.1)
            hand.rotation.y = 1.1 - Math.PI / 2
            g.add(hand)
          }
        } else if (r.id === 'table') {
          const top = new THREE.Mesh(new THREE.BoxGeometry(2, 0.08, 0.9), wood)
          top.position.y = 0.75
          g.add(top)
          const pot = new THREE.Mesh(new THREE.CylinderGeometry(0.25, 0.2, 0.3, 10), iron)
          pot.position.set(1.6, 0.15, 0.4)
          g.add(pot, ashPile().translateX(1.6).translateZ(0.4))
        } else if (r.id === 'shoe') {
          const shoe = new THREE.Mesh(new THREE.BoxGeometry(0.12, 0.08, 0.22), wood)
          shoe.position.y = 0.04
          g.add(shoe)
        } else {
          for (let k = -3; k <= 3; k++) {
            const f = new THREE.Mesh(new THREE.PlaneGeometry(0.12, 6).rotateX(-Math.PI / 2), dark)
            f.position.set(k * 0.35, 0.04, -3)
            g.add(f)
          }
        }
        g.position.set(r.x, 0, r.z)
        this.marks.add(g)
      }
      if (st.done.council) this.drawNorthRoad()
    }
    // S35: the palace gate stands in Aldermere
    if (st && !this.set.interior && isAldermere(this.area.name)) this.drawPalaceGate()
  }

  // S32: the north road's waymarks, from the end of Ashby's furrows to the
  // area's north edge
  private northSeen = new Set<string>()
  private waymarks() {
    const f = remnantSpots(this.area.width, this.area.depth, this.set.cell).find((r) => r.id === 'furrows')!
    return waymarkSpots(f.x, f.z - 6, 2)
  }
  private drawNorthRoad(): void {
    const dark = new THREE.MeshBasicNodeMaterial({ color: 0x15171c })
    const wood = new THREE.MeshStandardNodeMaterial({ color: 0x5b4330, roughness: 1 })
    const rope = new THREE.MeshStandardNodeMaterial({ color: 0xb59a6a, roughness: 1 })
    const ice = new THREE.MeshStandardNodeMaterial({ color: 0x9fb8c8, roughness: 0.2 })
    const ws = this.waymarks()
    // the furrows run on up the road between the waymarks
    const top = ws.at(-1)!.z
    const from = ws[0].z + (ws[0].z - top) * 0.25
    for (let k = -3; k <= 3; k++) {
      const f = new THREE.Mesh(new THREE.PlaneGeometry(0.12, from - top).rotateX(-Math.PI / 2), dark)
      f.position.set(ws[0].x - ws[0].side + k * 0.35, 0.04, (from + top) / 2)
      this.marks.add(f)
    }
    for (const w of ws) {
      const g = new THREE.Group()
      if (w.id === 'camp') {
        for (let k = 0; k < 3; k++) g.add(ashPile().translateX(-1.5 + k * 1.5).translateZ(-1.2))
        for (let k = 0; k < 8; k++) {
          const h = new THREE.Mesh(new THREE.CircleGeometry(0.35, 10).rotateX(-Math.PI / 2), dark)
          h.scale.set(1, 1, 2)
          h.position.set(-2 + (k % 4) * 1.3, 0.03, 0.8 + Math.floor(k / 4) * 1.2)
          g.add(h)
        }
      } else if (w.id === 'post') {
        const post = new THREE.Mesh(new THREE.BoxGeometry(0.18, 1.3, 0.18), wood)
        post.position.y = 0.65
        const coil = new THREE.Mesh(new THREE.TorusGeometry(0.14, 0.035, 6, 14).rotateX(Math.PI / 2), rope)
        coil.position.y = 0.8
        g.add(post, coil)
      } else if (w.id === 'joined') {
        for (let k = 0; k < 5; k++) {
          const f = new THREE.Mesh(new THREE.PlaneGeometry(0.12, 6).rotateX(-Math.PI / 2), dark)
          f.rotation.y = -0.9
          f.position.set(2 + k * 0.3, 0.04, -k * 0.2)
          g.add(f)
        }
      } else {
        const river = new THREE.Mesh(new THREE.PlaneGeometry(9, 2.2).rotateX(-Math.PI / 2), ice)
        river.position.y = 0.03
        g.add(river)
      }
      g.position.set(w.x, 0, w.z)
      this.marks.add(g)
    }
  }

  // S34: arriving in Aldermere with the reeve's letter plays the journey
  private journeyOn = false
  private stepJourney(): void {
    const st = this.story
    if (!st || this.journeyOn || !journeyReady(st.done) || !isAldermere(this.area.name)) return
    this.journeyOn = true
    // the story object may be replaced by the server's while the days pass:
    // read it afresh each time
    const here = () => (this.story && isAldermere(this.area.name) ? this.story : null)
    LEGS.forEach((leg, i) => setTimeout(() => {
      if (here()) this.say(leg.line)
    }, i * 7000))
    setTimeout(() => {
      const s = here()
      this.journeyOn = false
      if (!s || s.done.aldermere) return
      s.clock = afterJourney(s.clock)
      this.reach('aldermere')
      this.say(ARRIVE_LINE)
    }, LEGS.length * 7000)
  }

  // S35: the palace gate in Aldermere; walking up to it with the letter
  // plays the Steward's reading, a caption at a time
  private palaceOn = false
  private stepPalace(p: { x: number; z: number }): void {
    const st = this.story
    if (!st || this.palaceOn || this.set.interior || !palaceReady(st.done) || !isAldermere(this.area.name)) return
    if (!atGate(gateSpot(this.area.width, this.set.cell), p)) return
    this.palaceOn = true
    const here = () => (this.story && isAldermere(this.area.name) ? this.story : null)
    PALACE_LINES.forEach((line, i) => setTimeout(() => {
      if (here()) this.say(line)
    }, i * 7000))
    setTimeout(() => {
      const s = here()
      this.palaceOn = false
      if (!s || s.done.steward) return
      this.reach('steward')
      this.say(PALACE_LINE)
    }, PALACE_LINES.length * 7000)
  }
  private drawPalaceGate(): void {
    const stone = new THREE.MeshStandardNodeMaterial({ color: 0xb8b0a0, roughness: 0.9 })
    const banner = new THREE.MeshStandardNodeMaterial({ color: 0x7a1f2b, roughness: 1 })
    const g = gateSpot(this.area.width, this.set.cell)
    const gate = new THREE.Group()
    for (const side of [-1, 1]) {
      const tower = new THREE.Mesh(new THREE.BoxGeometry(1.6, 5, 1.6), stone)
      tower.position.set(side * 2.4, 2.5, -1)
      const flag = new THREE.Mesh(new THREE.PlaneGeometry(0.7, 1.6), banner)
      flag.position.set(side * 2.4, 3.4, -0.18)
      gate.add(tower, flag)
    }
    const arch = new THREE.Mesh(new THREE.BoxGeometry(6.4, 1, 1.6), stone)
    arch.position.set(0, 4.2, -1)
    gate.add(arch)
    gate.position.set(g.x, 0, g.z)
    this.marks.add(gate)
  }

  private atSign = false
  private stepRoad(p: { x: number; z: number }): void {
    const st = this.story!
    if (this.set.interior) return
    if (this.area.id === st.area && trackReady(st.done)) {
      const sign = this.signDoor()
      const at = !!sign && Math.hypot(p.x - sign.x, p.z - sign.z) <= TRACK_REACH
      if (at && !this.atSign) {
        this.reach('track')
        this.drawMarks()
        this.say(TRACK_LINE)
      }
      this.atSign = at
      return
    }
    if (this.area.id !== st.area && isAshby(this.area.name) && ashbyReady(st.done)) {
      const r = remnantAt(remnantSpots(this.area.width, this.area.depth, this.set.cell), p)
      if (!r || this.ashbySeen.has(r.id)) return
      this.ashbySeen.add(r.id)
      if (this.ashbySeen.size >= 4) {
        st.items.ashbylore = 1
        this.reach('ashby')
        this.say(`${r.text} ${ASHBY_LINE}`)
      } else this.say(r.text)
      return
    }
    if (this.area.id !== st.area && isAshby(this.area.name) && northRoadReady(st.done)) {
      const w = waymarkAt(this.waymarks(), p)
      if (!w || this.northSeen.has(w.id)) return
      this.northSeen.add(w.id)
      if (w.id === 'ford' && this.northSeen.size >= 4) {
        this.reach('northroad')
        this.say(`${w.text} ${NORTH_LINE}`)
      } else if (w.id === 'ford') {
        this.northSeen.delete('ford')
        this.say(`${w.text} You have come up the road too fast: go back and look at what you passed.`)
      } else this.say(w.text)
    }
  }

  // S20: walking up to a bundle takes it into the herbal; the sill tells the moons
  private stepBundles(p: { x: number; z: number }): void {
    const st = this.story!
    if (!this.blessed()) return
    const pieces = Object.values(this.area.pieces)
    const sill = sillSpot(pieces, this.set.cell)
    const near = !!sill && Math.hypot(p.x - sill.x, p.z - sill.z) < SILL_REACH
    if (near && !this.atSill) this.say(sillLine(nightOf(st.clock)))
    this.atSill = near
    // S22: the threshold: lay the ward when the stones and a sage are in hand
    const th = st.done.truedream && !st.done.ward ? thresholdSpot(pieces, this.set.cell) : null
    const atTh = !!th && Math.hypot(p.x - th.x, p.z - th.z) < WARD_REACH
    if (atTh && !this.atThreshold) {
      if (wardReady(st.done, st.items)) {
        st.items.sage--
        st.items.riverstones = 0
        st.items.ward = 1
        this.reach('ward')
        this.drawHerbs()
        this.say(WARD_LINE)
      } else this.say(wardMissing(st.items))
    }
    this.atThreshold = atTh
    // S24: the hearthstone: lay the hearth ward when the stones and a sage are in hand
    const hs = nextHearthWard(st.done) === 'hearthward' ? hearthOf(this.area, this.set.cell)?.stone : null
    const atHs = !!hs && Math.hypot(p.x - hs.x, p.z - hs.z) < HEARTH_REACH
    if (atHs && !this.atHearthStone) {
      if (hearthWardReady(st.done, st.items)) {
        st.items.sage--
        st.items.riverstones = 0
        st.items.hearthward = 1
        this.reach('hearthward')
        this.drawHerbs()
        this.say(HEARTH_WARD_LINE)
      } else this.say(hearthWardMissing(st.items))
    }
    this.atHearthStone = atHs
    if (!bundlesReady(st.done)) return
    const spots = bundleSpots(pieces, this.set.cell)
    for (const b of spots) {
      if (st.picked[b.spot] || Math.hypot(p.x - b.x, p.z - b.z) > BUNDLE_REACH) continue
      st.picked[b.spot] = true
      st.items[b.herb.id] = (st.items[b.herb.id] ?? 0) + 1
      this.opts.onStory?.({ step: '', area: this.area.id, x: round1(p.x), z: round1(p.z), item: b.herb.id, spot: b.spot })
      this.say(`${b.herb.title}, dried and tied with red thread. ${b.herb.note}`)
      this.drawHerbs()
      if (spots.every((x) => st.picked[x.spot])) {
        this.reach('bundles')
        this.say('Four of her herbs in your herbal now (K). Her notes teach valerian and lavender tea and sage broth: press C at the hearth.')
      }
    }
  }

  // S18: the Nine Sisters, on the open ground nearest the signpost, once
  // the veteran has told of them
  private drawStones(): void {
    this.stones.clear()
    this.stonesAwake = null
    this.ringHeart = null
    const st = this.story
    if (!st || this.area.id !== st.area || !st.done.companion) return
    const sign = Object.values(this.area.pieces).find((p) => p.piece === 'signpost')
    const pieces = Object.values(this.area.pieces)
    const sizeOf = (p: { piece: string; rot: number }) => {
      const piece = pieceOf(this.set, p.piece)
      return piece ? turned(piece, p.rot) : { w: 1, d: 1 }
    }
    const heart = ringSpot(pieces, sizeOf, this.area.width, this.area.depth, this.set.cell, sign ?? { x: 0, z: 0 })
    if (!heart) return
    this.ringHeart = heart
    const stone = new THREE.MeshStandardNodeMaterial({ color: 0x7d8278, roughness: 1 })
    const lichen = new THREE.MeshStandardNodeMaterial({ color: 0x8f9a5c, roughness: 1 })
    const pine = new THREE.MeshStandardNodeMaterial({ color: 0x2f4a32, roughness: 0.9 })
    for (const [i, s] of stonePoints(heart).entries()) {
      const g = new THREE.Group()
      const h = 2.2 + ((i * 5) % 3) * 0.35
      const body = new THREE.Mesh(new THREE.BoxGeometry(0.9, h, 0.55), stone)
      body.position.y = h / 2
      const patch = new THREE.Mesh(new THREE.BoxGeometry(0.92, 0.5, 0.57), lichen)
      patch.position.y = h * 0.35
      const crown = new THREE.Mesh(new THREE.ConeGeometry(0.45, 1.1, 6), pine)
      crown.position.y = h + 0.5
      g.add(body, patch, crown)
      g.position.set(s.x, 0, s.z)
      g.rotation.set(s.tilt, Math.atan2(s.x - heart.x, s.z - heart.z), 0)
      g.userData.stone = true
      this.stones.add(g)
    }
    const glow = new THREE.Mesh(new THREE.RingGeometry(3.6, 4.8, 36).rotateX(-Math.PI / 2), new THREE.MeshBasicNodeMaterial({ color: 0x9fc4ff, transparent: true, opacity: 0.35, depthWrite: false }))
    glow.position.set(heart.x, 0.06, heart.z)
    glow.visible = false
    glow.userData.wake = true
    this.stones.add(glow)
  }

  // S18: the stones wake at the lesser full moon; at the ring's heart then, the ground remembers
  private stepStones(p: { x: number; z: number }): void {
    const st = this.story!
    const heart = this.ringHeart
    if (!heart) return
    const awake = stonesWake(this.night, isDay(st.clock))
    if (awake !== this.stonesAwake) {
      this.stonesAwake = awake
      for (const c of this.stones.children) if (c.userData.wake) c.visible = awake && !st.done.stones
      if (awake && !st.done.stones) this.say('The lesser moon is full. Up by the signpost, something hums, low, like a held note.')
    }
    const inside = Math.hypot(p.x - heart.x, p.z - heart.z) < HEART
    if (!inside) {
      this.ringVisited = false
      return
    }
    if (st.done.stones || this.ringVisited || this.combat) return
    this.ringVisited = true
    if (!awake) {
      const n = nextWaking(this.night)
      this.say(`Nine lichened stones, pines on their crowns. The ground under them feels like it is listening. ${n === this.night ? 'They wake after dark tonight.' : `The lesser moon is full in ${n - this.night} night${n - this.night === 1 ? '' : 's'}.`}`)
      return
    }
    this.path = []
    showDream(this.container, STONES_MEMORY, () => {
      st.items.starlore = 1
      this.reach('stones')
      for (const c of this.stones.children) if (c.userData.wake) c.visible = false
      this.say(STARLORE_LINE)
    })
  }

  // S12: what there is to smell and hear in the story's area now
  senseSources(): Source[] {
    const st = this.story
    if (st && this.blessed()) {
      const pieces = Object.values(this.area.pieces)
      const c = cacheReady(st.done) ? cacheSpot(pieces, this.set.cell) : null
      const out: Source[] = c ? [{ id: 'cache', ...c, sense: 'sound', text: 'A draught, and a board that knocks hollow', from: 'by the box bed', range: 14 }] : []
      // S20: the bundles drying over the shelves
      if (bundlesReady(st.done)) for (const b of bundleSpots(pieces, this.set.cell)) if (!st.picked[b.spot]) out.push({ id: b.spot, x: b.x, z: b.z, sense: 'smell', text: `${b.herb.title}, dry and sweet`, from: 'over the shelves', range: 12 })
      return out
    }
    if (st && this.area.id !== st.area && isAshby(this.area.name) && ashbyReady(st.done) && !this.set.interior) {
      // S29: what the raid left, sensed from across the village
      return remnantSpots(this.area.width, this.area.depth, this.set.cell)
        .filter((r) => !this.ashbySeen.has(r.id))
        .map((r): Source => (r.id === 'door' ? { id: `ashby-${r.id}`, x: r.x, z: r.z, sense: 'sound', text: 'A door knocking on one hinge', from: 'in the lane', range: 26 } : { id: `ashby-${r.id}`, x: r.x, z: r.z, sense: 'smell', text: r.id === 'table' ? 'Cold ash and old stew' : ASHBY_SENSE, from: 'in the lane', range: 26 }))
    }
    if (st && this.area.id !== st.area && isAshby(this.area.name) && northRoadReady(st.done) && !this.set.interior) {
      // S32: the north road, sensed from the village
      return this.waymarks()
        .filter((w) => !this.northSeen.has(w.id))
        .map((w): Source => ({ id: `north-${w.id}`, x: w.x, z: w.z, sense: w.id === 'ford' ? 'sound' : 'smell', text: w.id === 'ford' ? 'Water running under ice' : NORTH_SENSE, from: 'up the north road', range: 30 }))
    }
    if (!st || this.area.id !== st.area) return []
    const out: Source[] = []
    // S26: the well, frost-cold once both hearths are shut
    const well = wellReady(st.done) ? wellSpot(Object.values(this.area.pieces), this.set.cell) : null
    if (well) out.push({ id: 'well', ...well, sense: 'smell', text: WELL_SENSE, from: 'from the well', range: 30 })
    // S28: the frost prints, seen from the yard
    const sign = trackReady(st.done) ? this.signDoor() : null
    if (sign) out.push({ id: 'track', ...sign, sense: 'smell', text: TRACK_SENSE, from: 'toward the signpost', range: 40 })
    if (st.done.howl && !st.done.signature && this.signature) {
      out.push({ id: 'signature', ...this.signature, sense: 'smell', text: 'Old pennies, and scorched snow', from: 'out by the well', range: 30 })
    }
    if (st.done.signature && !st.done.fight && this.barnDoor) {
      out.push({ id: 'hollowed', x: this.barnDoor.x, z: this.barnDoor.z, sense: 'smell', text: 'Burnt sugar and old iron', from: 'from the barn', range: 30 })
      if (this.storyDay === false) out.push({ id: 'owls', x: this.barnDoor.x, z: this.barnDoor.z + 6, sense: 'sound', text: 'The owls have gone silent', from: 'toward the barn', range: 20 })
    }
    if (st.done.morning && !st.done.forage) {
      for (const h of herbSpots(this.area.pieces, this.set.cell)) {
        if (!st.picked[h.spot]) out.push({ id: `herb-${h.spot}`, x: h.x, z: h.z, sense: 'smell', text: `${h.herb.title}, green on the air`, from: 'by the trees', range: 18 })
      }
    }
    if (this.ringHeart && this.stonesAwake && !st.done.stones) {
      out.push({ id: 'stones', ...this.ringHeart, sense: 'sound', text: 'A low hum, like a held note', from: 'from the stones by the signpost', range: 45 })
    }
    if (riverStonesReady(st.done, st.items) || hearthStonesReady(st.done, st.items)) {
      const d = ditchSpot(this.area.pieces, this.set.cell, this.fieldSize())
      if (d) out.push({ id: 'riverstones', x: d.x, z: d.z, sense: 'sound', text: 'Clean water running over stones', from: 'at the culvert by the east field', range: 35 })
    }
    if (bankSageReady(st.done, st.items, st.picked)) {
      const d = ditchSpot(this.area.pieces, this.set.cell, this.fieldSize())
      if (d) out.push({ id: 'banksage', ...bankSageSpot(d), sense: 'smell', text: 'Sage, sharp and green', from: 'on the ditch bank by the culvert', range: 25 })
    }
    if (st.done.reading && !st.done.ditch) {
      const d = ditchSpot(this.area.pieces, this.set.cell, this.fieldSize())
      if (d) out.push({ id: 'ditch', x: d.x, z: d.z, sense: 'sound', text: 'Water, choked and gurgling', from: 'at the low corner of the east field', range: 35 })
    }
    return out
  }

  // S12: the senses' cues, each at the screen's edge toward its source
  private stepSenses(p: { x: number; z: number }): void {
    if (!this.senses) this.senses = new SensesLayer(this.container)
    const cues = senseCues(p.x, p.z, this.senseSources(), this.focus)
    const w = this.container.clientWidth
    const h = this.container.clientHeight
    const cam = this.orbit.camera
    const me = new THREE.Vector3(p.x, 0, p.z).project(cam)
    const at = spread(
      cues.map((c) => {
        const q = new THREE.Vector3(p.x + Math.cos(c.angle) * 10, 0, p.z + Math.sin(c.angle) * 10).project(cam)
        return edgePoint(q.x - me.x, me.y - q.y, w, h)
      }),
      h,
    )
    this.senses.show(cues.map((c, i) => ({ ...c, at: at[i] })))
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
    // S31: after word of Ashby, the hearth-watch meets at Hawk's Rest's hearth
    if (st && councilReady(st.done) && isHawksRest(this.area.name) && this.nearHearth()) return this.council()
    if (st && reeveReady(st.done) && isHawksRest(this.area.name) && this.nearHearth()) return this.tellReeve()
    if (st?.done.foretold && !st.done.defended) return this.waitForDark()
    // S23: once the ward is laid, R at Hawk's Rest waits for the night it is tried
    if (st && triedReady(st.done) && this.blessed()) return this.wardNight()
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

  // S31: the hearth-watch's meeting, a caption at a time, then the step
  private council(): void {
    const st = this.story!
    COUNCIL_LINES.forEach((line, i) => setTimeout(() => {
      if (this.story === st) this.say(line)
    }, i * 7000))
    setTimeout(() => {
      if (this.story !== st || st.done.council) return
      this.reach('council')
      this.say(COUNCIL_LINE)
    }, COUNCIL_LINES.length * 7000)
  }

  // S33: the reeve hears there are two villages, and writes to the capital
  private tellReeve(): void {
    const st = this.story!
    REEVE_LINES.forEach((line, i) => setTimeout(() => {
      if (this.story === st) this.say(line)
    }, i * 7000))
    setTimeout(() => {
      if (this.story !== st || st.done.reeve) return
      this.reach('reeve')
      this.say(REEVE_LINE)
    }, REEVE_LINES.length * 7000)
  }

  // S14: R by a hearth once the notes foretold the night: the days pass
  // until it, and the night of hearth defence at the farmhouse begins
  private waitForDark(): void {
    const st = this.story!
    if (!this.nearHearth()) return this.flash('Stand by a hearth to wait for the dark night')
    if (this.hearth && !this.hearth.outcome) return this.flash('The night has come: keep the watch at the farmhouse')
    const n = nextNoMoon(nightOf(st.clock) - 1)
    st.clock = { day: n - FIRST_NIGHT, hour: 20 }
    this.slept = true
    const fade = document.createElement('div')
    fade.style.cssText = 'position:absolute;inset:0;background:#000;opacity:1;transition:opacity 2s;z-index:3;pointer-events:none'
    this.container.appendChild(fade)
    setTimeout(() => (fade.style.opacity = '0'), 600)
    setTimeout(() => fade.remove(), 2800)
    this.drawMarks()
    if (st.done.watch) this.savePlace(true)
    else this.reach('watch')
    this.say(`The days pass. You help Wenlan bar the byre and set a hooded lamp by the hearthstone. Day ${st.clock.day + 1}: no moon tonight. Keep the watch at the farmhouse.`)
    this.opts.onStoryNight?.()
  }

  // S23: R by Hawk's Rest's hearth once the ward is laid: the days pass to
  // the next night with no moon; the ward holds the door, and one comes up
  // through the hearthstone instead
  private wardNight(): void {
    const st = this.story!
    if (!this.nearHearth()) return this.flash('Stand by the hearth to wait for the dark night')
    if (this.hearth && !this.hearth.outcome) return this.flash('The night has come: hold the cottage until dawn')
    const n = nextNoMoon(nightOf(st.clock))
    st.clock = { day: n - FIRST_NIGHT, hour: 23 }
    this.slept = true
    const fade = document.createElement('div')
    fade.style.cssText = 'position:absolute;inset:0;background:#000;opacity:1;transition:opacity 2s;z-index:3;pointer-events:none'
    this.container.appendChild(fade)
    setTimeout(() => (fade.style.opacity = '0'), 600)
    setTimeout(() => fade.remove(), 2800)
    this.drawMarks()
    this.savePlace(true)
    this.startHearthNight(this.area, { ...TRIED_NIGHT, seed: hash(this.area.id) + n, cell: this.set.cell })
    this.nightHud.say(`Day ${st.clock.day + 1}, no moon. Nobody keeps the watch but you: stay in the cottage until dawn.`)
    this.say(TRIED_DOOR_LINE)
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
    const e = dayCard(hash(st.area), st.clock.day)?.effect
    const morning = `Day ${st.clock.day + 1}; the day's card: ${card.title}${e?.line ? `: ${e.line}` : ''}.`
    const warm = this.ateHot
    this.ateHot = false
    // S21: the dream tincture brings a true dream, wherever the night is slept
    if (this.tincture && !st.done.truedream) {
      this.tincture = false
      showDream(this.container, TRUE_DREAM, () => {
        st.items.wardlore = 1
        this.reach('truedream')
        showCard(this.container, card)
        this.drawMarks()
        this.say(`${TRUE_DREAM_LINE} ${morning}`)
      })
      return
    }
    // S16: a blessed place's dream, or a warm night that heals its wound
    if (dreams(st.done, this.blessed())) {
      showDream(this.container, FORD_DREAM, () => {
        st.items.wound = 1
        this.reach('dream')
        this.restartCombat()
        showCard(this.container, card)
        this.drawMarks()
        this.say(`You remember the long spear: your polearm skill rises. Your side aches where the dream's claw went in; until a warm night, some of the Chill stays. ${morning}`)
        this.knock()
      })
      return
    }
    if (st.done.dream && !st.done.companion && this.blessed()) this.knock()
    showCard(this.container, card)
    this.drawMarks() // S11: the new day's card may light what is hidden
    // S22: a warded place's night is dreamless and healing
    if (st.done.ward && this.blessed()) {
      st.body = wardedNight(st.body)
      const healed = !!st.items.wound
      st.items.wound = 0
      const p = this.player()
      if (healed) this.opts.onStory?.({ step: '', area: this.area.id, x: round1(p?.x ?? 0), z: round1(p?.z ?? 0), use: 'wound', ...this.stateToSave() })
      else this.savePlace(true)
      this.say(`A dreamless, healing night behind the ward: you wake warm through${healed ? ', and the ache in your side is gone' : ''}. ${morning}`)
      return
    }
    if (st.items.wound && warm) {
      st.items.wound = 0
      const p = this.player()
      this.opts.onStory?.({ step: '', area: this.area.id, x: round1(p?.x ?? 0), z: round1(p?.z ?? 0), use: 'wound', ...this.stateToSave() })
      this.say(`A warm, dreamless night. The ache in your side is gone. ${morning}`)
      return
    }
    this.say(`You sleep by the hearth${st.items.wound ? '. Your side still aches: eat something hot before you sleep' : ''}. ${morning}`)
  }

  // S16: a blessed place: the empty cottage up the road (an interior of
  // another area than the story's, with nobody in it), which approved of its guest
  private blessed(): boolean {
    const st = this.story
    return !!st && !!this.set.interior && this.area.id !== st.area && !Object.keys(this.area.npcs ?? {}).length
  }

  // S17: a knock at the cottage door in the morning: the first companion
  private knock(): void {
    const d = this.dialogues[VETERAN.id]
    if (!d) return
    setTimeout(() => {
      if (!this.playing || !this.story || this.story.done.companion) return
      this.talkingWith = d.id
      this.caption.style.display = 'none'
      this.talk.start(d, VETERAN.name)
      this.updateHint()
    }, 3500)
  }

  // S17: the companion follows a step behind, and in a fight takes the
  // foe's flank and strikes with the pike on their own time
  private stepCompanion(dt: number): void {
    const ch = this.character
    const want = this.playing && !!ch && !!this.story?.done.companion
    if (!want) {
      if (this.companion) this.scene.remove(this.companion.model.group)
      this.companion = null
      return
    }
    if (!this.companion) {
      const model = new CharacterModel(VETERAN.color)
      const p = ch!.group.position
      this.companion = { model, at: { x: p.x - 1, z: p.z - 1 }, cooldown: 1 }
      this.scene.add(model.group)
    }
    const cp = this.companion
    const pos = ch!.group.position
    const c = this.combat
    const foes = c ? [...c.foes.values()].map((f) => ({ id: f.id, x: f.x, z: f.z, alive: f.alive })) : []
    const r = companionStep(VETERAN, cp, { x: pos.x, z: pos.z, facing: ch!.group.rotation.y }, foes, c?.target ?? null, dt)
    const moved = Math.hypot(r.at.x - cp.at.x, r.at.z - cp.at.z)
    cp.at = r.at
    cp.cooldown = r.cooldown
    cp.model.group.position.set(r.at.x, 0, r.at.z)
    cp.model.group.rotation.y = r.facing
    cp.model.walk(this.loopTime * 12, moved > 0.002 ? 1 : 0)
    if (r.strike && c && c.over !== 'fallen') {
      const e = c.allyStrike(r.strike, VETERAN.polearm, VETERAN.aim)
      if (!e) return
      const name = this.area.npcs?.[r.strike]?.name ?? 'it'
      if (e.kind === 'kill') {
        this.showEvent(e)
        this.hud.say(`${VETERAN.name}'s pike takes ${name} through the ${e.vital ?? 'heart'} from the flank.`)
      } else if (e.kind === 'futile') {
        this.floatText(r.strike, 'pike', '#b9ad94')
        this.hud.say(`${VETERAN.name} drives the pike into ${name}'s side. It shrugs it off.`)
      } else if (e.kind === 'miss') this.floatText(r.strike, 'parried', '#8a8a8a')
    }
  }

  // The fight again with the character's skills as they are now
  private restartCombat(): void {
    if (this.combat) this.startCombat()
  }

  // S13: N, the notes, once Wenlan's ditch is clear: link two that belong together
  openJournal(): void {
    const st = this.story
    if (!st) return
    if (this.journal?.el.isConnected) {
      this.journal.el.remove()
      this.journal = null
      return
    }
    if (!st.done.ditch) {
      this.say('Nothing to write down yet.')
      return
    }
    const night = nightOf(st.clock)
    const concluded = st.done.foretold ? [link('ash', 'almanac', night)!] : []
    this.journal = new JournalScene(this.container, journalEntries(st.done), concluded, night, (c) => {
      this.say(c.text)
      if (c.step) this.reach(c.step)
    })
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
    this.stepCompanion(dt)
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
      ? `The story · click to walk, talk or go through a door · J hides the steps${this.nearHearth() && this.story.done.road && !this.story.done.rest ? ' · R to rest by the hearth' : ''}${this.story.done.morning ? ' · K: the herbal' : ''}${this.story.done.herbal && this.nearHearth() ? ' · B: stillness · C: cook' : ''}${this.story.done.rest ? ' · M: the almanac' : ''}${this.story.done.rest && this.nearHearth() && canSleep(this.story.clock) ? ' · R: sleep' : ''}${this.story.done.ditch ? ' · N: notes' : ''}${this.focus ? ' · focusing: your senses sharpen' : this.story.done.howl ? ' · hold F to focus your senses' : ''}`
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
