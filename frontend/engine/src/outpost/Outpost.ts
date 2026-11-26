import * as THREE from 'three/webgpu'
import type { ShipState, Vec3 } from '../ship/Ship.ts'

// The outpost (forge 360 V6): a beacon at the moon's north pole and its
// quartermaster, who offers contracts (game.contracts) handed in with a
// ship parked by the beacon (game.handIn). Skills (game.player) level up
// from what the contracts pay. The beacon's place on the server is the
// north pole at the moon's radius plus its hills; within 40 m counts.

export interface ContractList {
  giver: { name: string; title: string; line: string }
  contracts: {
    id: string
    title: string
    description: string
    skill: string
    xp: number
    // V8: what the ship must bring, the design the job needs, the design it unlocks
    needs?: { complete?: boolean; iron?: number; ore?: number; rock?: number }
    design?: string
    unlocks?: string
    // V11: a job of the job board, done again and again, and the Trade level it needs
    job?: boolean
    level?: number
  }[]
}

export interface Progress {
  done: Record<string, boolean>
  xp: Record<string, number>
  levels: Record<string, number>
  unlocked?: Record<string, boolean> // V8: designs contracts opened
  at?: [number, number, number] // V8: where the player last stood; missing until they land
  needs?: { air: number; fed: number; health: number } // V10: their body's needs when last saved
  jobs?: Record<string, number> // V11: how many times they did each job
  home?: string // V13: the cabin whose bed they last slept in
}

export const OUTPOST_REACH = 40

// What a piloting level gives: thrusters answer a better pilot harder
export function thrustFor(levels: Record<string, number> | undefined): number {
  return 1 + 0.1 * Math.max(0, (levels?.piloting ?? 1) - 1)
}

// The beacon's point as the server reckons it
export function beaconPoint(radius: number, amplitude: number): Vec3 {
  return { x: 0, y: radius + amplitude, z: 0 }
}

export function atOutpost(s: ShipState, radius: number, amplitude: number): boolean {
  const b = beaconPoint(radius, amplitude)
  return Math.hypot(s.x - b.x, s.y - b.y, s.z - b.z) <= OUTPOST_REACH
}

// What the V6 contracts need, for a list from before V8's needs
const NEEDS: Record<string, ContractList['contracts'][number]['needs']> = { 'first-flight': { complete: true }, 'beacon-iron': { iron: 1 } }

// Why a ship cannot hand in a contract, or '' (the blueprint decides)
export function handInProblem(id: string, s: ShipState | null, p: Progress | null, blocks: number, c?: ContractList['contracts'][number]): string {
  if (p?.done[id] && !c?.job) return 'done'
  if (c?.level && (p?.levels.trade ?? 1) < c.level) return `needs Trade level ${c.level}`
  if (!s) return `park a ship within ${OUTPOST_REACH} m of the beacon`
  const needs = c?.needs ?? NEEDS[id] ?? {}
  if (c?.design && s.design !== c.design) return `needs a ${c.design}`
  if (needs.complete && s.placed.length < blocks) return `${blocks - s.placed.length} blocks still to place`
  if ((s.cargo?.rock ?? 0) < (needs.rock ?? 0)) return `needs ${needs.rock} m³ of rock in the hold`
  if ((s.cargo?.iron ?? 0) < (needs.iron ?? 0)) return `needs ${needs.iron} m³ of iron in the hold`
  if ((s.cargo?.ore ?? 0) < (needs.ore ?? 0)) return `needs ${needs.ore} m³ of ore in the hold`
  return ''
}

// The beacon mast and the quartermaster, standing on the ground at the pole
export class OutpostModel {
  readonly group = new THREE.Group()
  private readonly lamp: THREE.Mesh
  readonly giverAt = new THREE.Vector3()

  constructor() {
    const metal = new THREE.MeshStandardNodeMaterial({ color: 0x9aa4ad, roughness: 0.5, metalness: 0.3 })
    const mast = new THREE.Mesh(new THREE.CylinderGeometry(0.15, 0.25, 9, 8), metal)
    mast.position.y = 4.5
    this.group.add(mast)
    const pad = new THREE.Mesh(new THREE.CylinderGeometry(6, 6, 0.3, 24), new THREE.MeshStandardNodeMaterial({ color: 0x3c4148, roughness: 0.8 }))
    pad.position.set(8, 0.1, 0)
    this.group.add(pad)
    this.lamp = new THREE.Mesh(new THREE.SphereGeometry(0.45, 16, 12), new THREE.MeshBasicNodeMaterial({ color: 0xff5252 }))
    this.lamp.position.y = 9.3
    this.group.add(this.lamp)
    // the quartermaster: a suit of a body and a helmet
    const suit = new THREE.MeshStandardNodeMaterial({ color: 0xe0a040, roughness: 0.6 })
    const body = new THREE.Mesh(new THREE.CylinderGeometry(0.3, 0.35, 1.2, 10), suit)
    body.position.set(2, 0.9, 1)
    const head = new THREE.Mesh(new THREE.SphereGeometry(0.28, 12, 10), new THREE.MeshStandardNodeMaterial({ color: 0x80deea, roughness: 0.1 }))
    head.position.set(2, 1.75, 1)
    this.group.add(body, head)
    this.giverAt.set(2, 1.2, 1)
  }

  // Stands it on the ground at a point, its up away from the pull
  place(at: Vec3, up: Vec3): void {
    this.group.position.set(at.x, at.y, at.z)
    this.group.quaternion.setFromUnitVectors(new THREE.Vector3(0, 1, 0), new THREE.Vector3(up.x, up.y, up.z).normalize())
    this.group.updateMatrixWorld(true)
  }

  // Where the quartermaster stands in the world
  giver(): Vec3 {
    const p = this.giverAt.clone().applyMatrix4(this.group.matrixWorld)
    return { x: p.x, y: p.y, z: p.z }
  }

  blink(t: number): void {
    this.lamp.visible = Math.sin(t * 3) > -0.3
  }
}

// The quartermaster's panel: their line, each contract with Hand in, and
// the player's skills. Pure DOM; the viewport owns it.
export class ContractPanel {
  private readonly el: HTMLDivElement
  open = false

  constructor(container: HTMLElement) {
    this.el = document.createElement('div')
    this.el.dataset.contracts = ''
    this.el.style.cssText =
      'position:absolute;right:12px;top:12px;z-index:4;display:none;width:340px;padding:10px 12px;border-radius:6px;max-height:calc(100% - 24px);overflow-y:auto;box-sizing:border-box;' +
      'background:rgba(14,16,22,0.92);border:1px solid #48536a;font:12px system-ui,sans-serif;color:#e4e9f2'
    container.appendChild(this.el)
  }

  show(list: ContractList, rows: { id: string; problem: string }[], progress: Progress | null, onHandIn: (id: string) => void): void {
    this.open = true
    this.el.replaceChildren()
    const h = document.createElement('div')
    h.style.cssText = 'font:600 14px system-ui,sans-serif;margin-bottom:2px'
    h.textContent = `${list.giver.name}, ${list.giver.title}`
    const line = document.createElement('div')
    line.style.cssText = 'opacity:0.85;font-style:italic;margin-bottom:8px'
    line.textContent = `“${list.giver.line}”`
    this.el.append(h, line)
    let board = false
    for (const c of list.contracts) {
      if (c.job && !board) {
        // V11: the job board below the quartermaster's own contracts
        board = true
        const jb = document.createElement('div')
        jb.dataset.jobBoard = ''
        jb.style.cssText = 'border-top:2px solid #5b6b8a;margin-top:6px;padding-top:6px;font:600 13px system-ui,sans-serif'
        jb.textContent = 'Job board: take these again and again; only jobs teach Trade'
        this.el.append(jb)
      }
      const row = rows.find((r) => r.id === c.id)
      const box = document.createElement('div')
      box.style.cssText = 'border-top:1px solid #333b4a;padding:6px 0'
      const t = document.createElement('div')
      t.style.cssText = 'display:flex;justify-content:space-between;gap:8px;align-items:center'
      const name = document.createElement('b')
      name.textContent = c.title
      const btn = document.createElement('button')
      btn.dataset.handIn = c.id
      const done = row?.problem === 'done'
      btn.textContent = done ? 'Done' : 'Hand in'
      btn.disabled = !!row?.problem
      btn.style.cssText = 'padding:2px 10px;border-radius:4px;border:1px solid #5b6b8a;background:transparent;color:#e4e9f2;cursor:pointer;opacity:' + (row?.problem ? '0.5' : '1')
      btn.onclick = (e) => {
        e.stopPropagation()
        onHandIn(c.id)
      }
      t.append(name, btn)
      const d = document.createElement('div')
      d.style.cssText = 'opacity:0.8;margin-top:2px'
      const times = c.job ? (progress?.jobs?.[c.id] ?? 0) : 0
      d.textContent = `${c.description} Pays ${c.xp} ${c.skill} xp.${times ? ` Done ${times}×.` : ''}${row?.problem && !done ? ` (${row.problem})` : ''}`
      box.append(t, d)
      this.el.append(box)
    }
    const skills = document.createElement('div')
    skills.dataset.skills = ''
    skills.style.cssText = 'border-top:1px solid #333b4a;padding-top:6px;opacity:0.9'
    const lv = (k: string) => progress?.levels[k] ?? 1
    skills.textContent = `Your skills: piloting ${lv('piloting')} · prospecting ${lv('prospecting')} · mining ${lv('mining')} · trade ${lv('trade')}${progress?.unlocked?.freighter ? ' (the freighter is yours to build)' : ''} · Esc frees the mouse to click · T closes`
    this.el.append(skills)
    this.el.style.display = 'block'
  }

  hide(): void {
    this.open = false
    this.el.style.display = 'none'
  }

  dispose(): void {
    this.el.remove()
  }
}
