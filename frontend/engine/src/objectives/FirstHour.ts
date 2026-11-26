import type { Progress } from '../outpost/Outpost.ts'
import type { ShipState } from '../ship/Ship.ts'

// The first hour (forge 360 V8): the steps that lead a new player from
// landing to their first two contracts, worked out from what the server
// already keeps (the pack, the world's ships, the player's progress) and
// what this window saw (walking, flying). A step counts once any later
// step is done, so the list only moves forward.

export interface FirstHourInput {
  walked: boolean
  flew: boolean
  pack: { dust: number; rock: number } | null
  ships: readonly ShipState[]
  // the number of blocks in each design, by id
  blocks: Record<string, number>
  progress: Progress | null
}

export interface Step {
  id: string
  title: string
  how: string
  done: boolean
}

export function firstHour(i: FirstHourInput): Step[] {
  const finished = (design: string) => i.ships.some((s) => s.design === design && s.placed.length >= (i.blocks[design] ?? Infinity))
  const done = i.progress?.done ?? {}
  const carried = (i.pack?.dust ?? 0) + (i.pack?.rock ?? 0) > 0 || i.ships.some((s) => (s.cargo?.dust ?? 0) + (s.cargo?.rock ?? 0) + (s.cargo?.iron ?? 0) > 0)
  const steps: Step[] = [
    { id: 'walk', title: 'Set foot on the moon', how: 'Choose Walk, click the view, and walk with WASD.', done: i.walked },
    { id: 'dig', title: 'Dig some regolith', how: 'Left click the ground near you; what you dig goes into your pack.', done: carried },
    { id: 'skiff', title: 'Lay out a skiff', how: 'Press Start a skiff: its outline appears ahead of you.', done: i.ships.some((s) => s.design === 'skiff') },
    { id: 'build', title: 'Build the skiff', how: 'Aim at each outline block and left click to place it.', done: finished('skiff') },
    { id: 'fly', title: 'Fly it', how: 'F at the cockpit; W/S, A/D, Space and C, the mouse to turn.', done: i.flew },
    { id: 'first-flight', title: 'Report to the outpost', how: 'Park within 40 m of the beacon at the north pole, press T by the quartermaster and hand in A ship that flies.', done: !!done['first-flight'] },
    { id: 'prospector', title: 'Build a prospector', how: 'Start a prospector and place all its blocks.', done: finished('prospector') },
    { id: 'iron', title: 'Refine iron', how: 'Dig rock, L to load it into the hold, R to refine it into iron.', done: i.ships.some((s) => (s.cargo?.iron ?? 0) > 0) },
    { id: 'beacon-iron', title: 'Bring iron for the beacon', how: 'Park the prospector at the beacon with 1 m³ of iron and hand in Iron for the beacon.', done: !!done['beacon-iron'] },
  ]
  // a later step done means the earlier ones were
  for (let k = steps.length - 1, later = false; k >= 0; k--) {
    if (later) steps[k].done = true
    later ||= steps[k].done
  }
  return steps
}

// The first-hour panel: the steps with the current one opened. Pure DOM;
// the viewport owns it. J shows or hides it.
export class FirstHourPanel {
  private readonly el: HTMLDivElement
  private last = ''
  shown = true

  constructor(container: HTMLElement) {
    this.el = document.createElement('div')
    this.el.dataset.firstHour = ''
    this.el.style.cssText =
      'position:absolute;right:12px;top:12px;z-index:3;width:270px;padding:8px 10px;border-radius:6px;pointer-events:none;' +
      'background:rgba(14,16,22,0.78);border:1px solid #39424f;font:12px system-ui,sans-serif;color:#e4e9f2;display:none'
    container.appendChild(this.el)
  }

  show(steps: readonly Step[], visible: boolean): void {
    const on = visible && this.shown
    const key = (on ? '1' : '0') + steps.map((s) => (s.done ? 1 : 0)).join('')
    if (key === this.last) return
    this.last = key
    this.el.style.display = on ? 'block' : 'none'
    if (!on) return
    this.el.replaceChildren()
    const h = document.createElement('div')
    const count = steps.filter((s) => s.done).length
    h.style.cssText = 'font:600 13px system-ui,sans-serif;margin-bottom:4px'
    h.textContent = count === steps.length ? 'Your first hour: all done' : `Your first hour: ${count} of ${steps.length}`
    this.el.append(h)
    const current = steps.find((s) => !s.done)
    for (const s of steps) {
      const row = document.createElement('div')
      row.dataset.step = s.id
      row.dataset.done = String(s.done)
      row.style.cssText = `padding:1px 0;opacity:${s.done ? 0.55 : s === current ? 1 : 0.75}`
      row.textContent = `${s.done ? '✓' : s === current ? '▸' : '○'} ${s.title}`
      this.el.append(row)
      if (s === current) {
        const how = document.createElement('div')
        how.style.cssText = 'padding:0 0 3px 14px;opacity:0.85;font-style:italic'
        how.textContent = s.how
        this.el.append(how)
      }
    }
    const foot = document.createElement('div')
    foot.style.cssText = 'margin-top:4px;opacity:0.6'
    foot.textContent = 'J hides this'
    this.el.append(foot)
  }

  dispose(): void {
    this.el.remove()
  }
}
