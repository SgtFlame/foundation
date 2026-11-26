import { moons } from '../sky/Moons.ts'
import { FIRST_NIGHT } from './Clock.ts'

// The clue journal (forge 375 S13; the play design's investigation: "a
// clue journal collects people, places and evidence. Some entries can be
// linked to draw a conclusion, which opens the next step"), shown as the
// player's own notes in the herbal's style. N opens it once Wenlan's ditch
// is clear. The first conclusion: the Ash card's warning ("on a night with
// no moon") linked with the moon almanac names the night the eyeless ones
// come back to the farm.

export interface Entry {
  id: string
  kind: 'person' | 'place' | 'evidence'
  title: string
  text: string
}

// What the notes hold, from the steps reached
export function journalEntries(done: Readonly<Record<string, boolean>>): Entry[] {
  const all: [string, Entry][] = [
    ['farmer', { id: 'wenlan', kind: 'person', title: 'Wenlan Harrow', text: 'A farmer of the Sheaflands. His father cut the drains along the east field, forty winters ago.' }],
    ['road', { id: 'hawks-rest', kind: 'place', title: "Hawk's Rest", text: 'An empty cottage west along the north road. Its cold hearth took me in.' }],
    ['signature', { id: 'signature', kind: 'evidence', title: 'The raid by the well', text: "Burnt sugar and old iron, frost-black handprints, the Brannocks' lantern where it fell." }],
    ['fight', { id: 'eyes', kind: 'evidence', title: 'Ember eyes in the barn', text: 'Two Hollowed hunched in the snow. Only the neck or the heart ended them.' }],
    ['almanac', { id: 'almanac', kind: 'evidence', title: 'The moon almanac', text: 'The coming nights: which moons are up, and which nights are dark.' }],
    ['reading', { id: 'ash', kind: 'evidence', title: 'The Ash card', text: 'Faces with no eyes in them. They come back to this farm before the thaw, on a night with no moon.' }],
  ]
  return all.filter(([step]) => done[step]).map(([, e]) => e)
}

// The next night with no moon after a night: the greater moon dark and the
// lesser all but dark
export function nextNoMoon(after: number): number {
  for (let n = after + 1; n < after + 400; n++) {
    const m = moons(n)
    if (m.greater.lit < 0.1 && m.lesser.lit < 0.3) return n
  }
  for (let n = after + 1; ; n++) if (moons(n).greater.lit < 0.1) return n
}

export interface Conclusion {
  id: string
  step?: string // the story step it reaches
  text: string
}

// What two entries linked conclude, or null when they do not fit together
export function link(a: string, b: string, night: number): Conclusion | null {
  const pair = [a, b].sort().join('+')
  if (pair === 'almanac+ash') {
    const n = nextNoMoon(night)
    return { id: 'foretold', step: 'foretold', text: `The eyeless ones come back to the farm on Day ${n - FIRST_NIGHT + 1}, the next night with no moon. Wenlan must bar the byre, and someone must keep watch.` }
  }
  if (pair === 'eyes+signature') return { id: 'raiders', text: 'The raid by the well and the things in the barn were one: the Hollowed took the Brannocks and dragged them west.' }
  return null
}

// The notes' page: entries to click two at a time, and what they concluded
export class JournalScene {
  readonly el: HTMLDivElement
  private picked: string | null = null
  private readonly said: HTMLDivElement
  private readonly rows = new Map<string, HTMLDivElement>()
  private readonly night: number
  private readonly onConclude: (c: Conclusion) => void

  constructor(container: HTMLElement, entries: readonly Entry[], concluded: readonly Conclusion[], night: number, onConclude: (c: Conclusion) => void) {
    this.night = night
    this.onConclude = onConclude
    this.el = document.createElement('div')
    this.el.dataset.journal = ''
    this.el.style.cssText =
      'position:absolute;left:50%;top:50%;transform:translate(-50%,-50%);width:min(480px,calc(100% - 32px));max-height:calc(100% - 40px);overflow-y:auto;padding:16px 18px;border-radius:6px;' +
      'background:#efe3c8;color:#3a2a18;font:13px Georgia,serif;box-shadow:0 8px 30px rgba(0,0,0,.6);z-index:4;box-sizing:border-box'
    const h = document.createElement('div')
    h.style.cssText = 'font:600 17px Georgia,serif;margin-bottom:2px'
    h.textContent = 'My notes'
    const sub = document.createElement('div')
    sub.style.cssText = 'font-style:italic;opacity:0.75;margin-bottom:10px'
    sub.textContent = 'Click two notes that belong together. N closes.'
    this.el.append(h, sub)
    for (const kind of ['person', 'place', 'evidence'] as const) {
      const list = entries.filter((e) => e.kind === kind)
      if (!list.length) continue
      const k = document.createElement('div')
      k.style.cssText = 'font:11px system-ui,sans-serif;letter-spacing:0.08em;text-transform:uppercase;opacity:0.6;margin:8px 0 4px'
      k.textContent = { person: 'People', place: 'Places', evidence: 'Evidence' }[kind]
      this.el.append(k)
      for (const e of list) {
        const row = document.createElement('div')
        row.dataset.note = e.id
        row.style.cssText = 'padding:5px 8px;margin:3px 0;border-radius:4px;cursor:pointer;border:1px solid rgba(58,42,24,0.25)'
        const t = document.createElement('b')
        t.textContent = e.title
        row.append(t, document.createTextNode(` · ${e.text}`))
        row.addEventListener('click', () => this.pick(e.id))
        this.rows.set(e.id, row)
        this.el.append(row)
      }
    }
    this.said = document.createElement('div')
    this.said.dataset.concluded = ''
    this.said.style.cssText = 'margin-top:12px;line-height:1.45'
    this.el.append(this.said)
    for (const c of concluded) this.write(c)
    container.appendChild(this.el)
  }

  // Picks a note; the second pick links the two
  pick(id: string): Conclusion | null {
    if (!this.picked) {
      this.picked = id
      this.rows.get(id)!.style.background = 'rgba(176,138,74,0.35)'
      return null
    }
    const first = this.picked
    this.picked = null
    for (const r of this.rows.values()) r.style.background = ''
    if (first === id) return null
    const c = link(first, id, this.night)
    if (!c) {
      this.flash('These do not fit together.')
      return null
    }
    if (![...this.said.children].some((x) => (x as HTMLElement).dataset.conclusion === c.id)) {
      this.write(c)
      this.onConclude(c)
    }
    return c
  }

  private write(c: Conclusion): void {
    const p = document.createElement('div')
    p.dataset.conclusion = c.id
    p.style.cssText = 'padding:6px 8px;margin:4px 0;border-left:3px solid #8a5a2a;font-style:italic'
    p.textContent = `So: ${c.text}`
    this.said.append(p)
  }

  private flash(text: string): void {
    const p = document.createElement('div')
    p.style.cssText = 'opacity:0.7;font-style:italic'
    p.textContent = text
    this.said.append(p)
    setTimeout(() => p.remove(), 2500)
  }
}
