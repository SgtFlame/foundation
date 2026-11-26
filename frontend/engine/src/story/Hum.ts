import { moons } from '../sky/Moons.ts'

// The Hum, Chill and Hunger (forge 375 S8; the design's rules 2 and 3):
// one pool, the Hum, whose size the sky sets; it fills through stillness,
// a breath in for four and out for six at a quiet hearth, not with time.
// Spending it (the Hum cadence in a fight) leaves the Chill, the hands
// gone white and cold, and Hunger spikes; hot rich food clears them.

export interface Body {
  hum: number
  chill: number // 0 to 100
  hunger: number // 0 to 100
}

export const BODY_START: Body = { hum: 0, chill: 0, hunger: 20 }

export const HUM = {
  breathSeconds: 10, // in for four, out for six
  inSeconds: 4,
  chillPerHum: 40,
  hungerPerHum: 15,
  hungerMinutes: 25, // calm to famished: about one story day
  chillFadeMinutes: 15,
}

// A thimble line's small pool (the design: bloodline tiers): 1 in the
// greater moon's dark, up to 4 when it is full; the lesser's full adds one
export function humPool(night: number): number {
  const m = moons(night)
  if (m.greater.lit < 0.1) return 1
  return Math.min(5, 2 + Math.round(2 * m.greater.lit) + (m.lesser.lit > 0.9 ? 1 : 0))
}

const clamp = (v: number) => Math.max(0, Math.min(100, v))

// One breath of stillness at a quiet place
export function breathe(b: Body, pool: number): Body {
  return { ...b, hum: Math.min(pool, b.hum + 1) }
}

// The Hum cadence spends one
export function spendHum(b: Body): Body {
  if (b.hum < 1) return b
  return { hum: b.hum - 1, chill: clamp(b.chill + HUM.chillPerHum), hunger: clamp(b.hunger + HUM.hungerPerHum) }
}

// Time passing: hunger grows, faster while chilled; the chill fades slowly
export function stepBody(b: Body, dt: number): Body {
  const hunger = clamp(b.hunger + (100 / (HUM.hungerMinutes * 60)) * dt * (1 + b.chill / 50))
  const chill = clamp(b.chill - (100 / (HUM.chillFadeMinutes * 60)) * dt)
  return { hum: b.hum, chill, hunger }
}

// What can be cooked at a hearth from the herbal's herbs
export interface Dish {
  id: string
  herb: string
  title: string
  line: string
  chill: number // taken off
  hunger: number // taken off
}

export const DISHES: readonly Dish[] = [
  { id: 'thymeMilk', herb: 'thyme', title: 'Thyme in hot milk', line: 'Warms the hands back to pink: clears the Chill.', chill: 100, hunger: 10 },
  { id: 'nettleSoup', herb: 'nettle', title: 'Nettle soup', line: 'Hot and green and filling: eases Hunger and a little Chill.', chill: 25, hunger: 60 },
]

export function cook(b: Body, d: Dish): Body {
  return { hum: b.hum, chill: clamp(b.chill - d.chill), hunger: clamp(b.hunger - d.hunger) }
}

// What the body says, worst first
export function bodyLine(b: Body): string | null {
  if (b.chill >= 50) return 'Your hands are white and cold: something hot would help'
  if (b.hunger >= 80) return 'Ravenous'
  if (b.hunger >= 55) return 'Hungry'
  return null
}

export function rounded(b: Body): Body {
  return { hum: Math.round(b.hum), chill: Math.round(b.chill), hunger: Math.round(b.hunger) }
}

// The Hum's pips, the Chill and Hunger, under the moons
export class BodyHud {
  readonly el: HTMLDivElement
  private pips: HTMLDivElement
  private chill: HTMLDivElement
  private hunger: HTMLDivElement
  private line: HTMLDivElement
  private clock: HTMLDivElement
  private rows: HTMLDivElement[] = []

  constructor(container: HTMLElement, bottom = 56) {
    const el = document.createElement('div')
    el.dataset.body = ''
    el.style.cssText = `position:absolute;left:12px;bottom:${bottom}px;width:150px;padding:6px 8px;border-radius:6px;background:rgba(10,12,20,.6);font:11px system-ui,sans-serif;color:#e8eef4;pointer-events:none;display:none;z-index:2`
    this.clock = document.createElement('div')
    this.clock.dataset.clock = ''
    this.clock.style.cssText = 'font-weight:600;margin-bottom:3px'
    el.appendChild(this.clock)
    this.pips = document.createElement('div')
    this.pips.dataset.hum = ''
    this.pips.style.cssText = 'font-size:13px;letter-spacing:2px;color:#f4d27a'
    const bar = (label: string, color: string) => {
      const row = document.createElement('div')
      row.style.cssText = 'display:flex;align-items:center;gap:6px;margin-top:4px'
      const name = document.createElement('span')
      name.textContent = label
      name.style.width = '44px'
      const track = document.createElement('div')
      track.style.cssText = 'flex:1;height:6px;background:rgba(0,0,0,.6);border-radius:3px;overflow:hidden'
      const fill = document.createElement('div')
      fill.style.cssText = `height:100%;width:0;background:${color}`
      track.appendChild(fill)
      row.append(name, track)
      el.appendChild(row)
      this.rows.push(row)
      return fill
    }
    el.appendChild(this.pips)
    this.chill = bar('Chill', '#a8d8ff')
    this.hunger = bar('Hunger', '#e8a860')
    this.line = document.createElement('div')
    this.line.style.cssText = 'margin-top:4px;color:#ffd0b0;font-style:italic'
    el.appendChild(this.line)
    container.appendChild(el)
    this.el = el
  }

  // the body once the Hum is known; the clock (S9) whenever it is given
  show(b: Body | null, pool: number, clock?: string): void {
    this.el.style.display = b || clock ? 'block' : 'none'
    this.clock.textContent = clock ?? ''
    this.clock.style.display = clock ? 'block' : 'none'
    for (const r of [this.pips, this.line, ...this.rows]) r.style.display = b ? '' : 'none'
    if (!b) return
    this.pips.textContent = 'Hum ' + '◆'.repeat(Math.min(b.hum, pool)) + '◇'.repeat(Math.max(0, pool - b.hum))
    this.chill.style.width = `${b.chill}%`
    this.hunger.style.width = `${b.hunger}%`
    this.line.textContent = bodyLine(b) ?? ''
  }
}

// The breathing prompt: a ring that swells for four seconds and falls for six
export class BreathPrompt {
  private el: HTMLDivElement | null = null
  private ring: HTMLDivElement | null = null
  private label: HTMLDivElement | null = null
  private t = 0
  private readonly container: HTMLElement
  constructor(container: HTMLElement) {
    this.container = container
  }

  get active(): boolean {
    return !!this.el
  }

  start(): void {
    if (this.el) return
    this.t = 0
    const el = document.createElement('div')
    el.dataset.breath = ''
    el.style.cssText = 'position:absolute;left:50%;top:42%;transform:translate(-50%,-50%);width:180px;height:180px;display:flex;align-items:center;justify-content:center;pointer-events:none;z-index:3'
    const ring = document.createElement('div')
    ring.style.cssText = 'position:absolute;border-radius:50%;border:2px solid rgba(244,210,122,.85);box-shadow:0 0 18px rgba(244,210,122,.35)'
    const label = document.createElement('div')
    label.style.cssText = 'font:15px Georgia,serif;color:#f4e6c0;text-shadow:0 1px 3px #000'
    el.append(ring, label)
    this.container.appendChild(el)
    this.el = el
    this.ring = ring
    this.label = label
  }

  // Advances the prompt; true when a whole breath has been drawn
  step(dt: number): boolean {
    if (!this.el || !this.ring || !this.label) return false
    this.t += dt
    let done = false
    if (this.t >= HUM.breathSeconds) {
      this.t -= HUM.breathSeconds
      done = true
    }
    const inhale = this.t < HUM.inSeconds
    const k = inhale ? this.t / HUM.inSeconds : 1 - (this.t - HUM.inSeconds) / (HUM.breathSeconds - HUM.inSeconds)
    const size = 50 + 120 * k
    this.ring.style.width = this.ring.style.height = `${size}px`
    this.label.textContent = inhale ? `In… ${Math.ceil(HUM.inSeconds - this.t)}` : `Out… ${Math.ceil(HUM.breathSeconds - this.t)}`
    return done
  }

  stop(): void {
    this.el?.remove()
    this.el = this.ring = this.label = null
  }
}

// The cooking panel by a hearth: each dish with the herb it needs
export function showCooking(container: HTMLElement, items: Readonly<Record<string, number>>, onCook: (d: Dish) => void): HTMLDivElement {
  const el = document.createElement('div')
  el.dataset.cooking = ''
  el.style.cssText = 'position:absolute;left:50%;top:50%;transform:translate(-50%,-50%);width:360px;padding:16px 18px;border-radius:6px;background:#efe3c8;color:#3a2a18;font:13px Georgia,serif;box-shadow:0 8px 30px rgba(0,0,0,.6);z-index:4'
  const h = document.createElement('div')
  h.textContent = 'At the hearth'
  h.style.cssText = 'font-size:16px;font-weight:bold;margin-bottom:8px'
  el.appendChild(h)
  for (const d of DISHES) {
    const have = items[d.herb] ?? 0
    const b = document.createElement('button')
    b.dataset.dish = d.id
    b.disabled = have < 1
    b.style.cssText = 'display:block;width:100%;text-align:left;margin:6px 0;padding:8px 10px;border:1px solid #b39b70;border-radius:4px;background:#f8f0dc;color:inherit;font:inherit;cursor:pointer'
    b.innerHTML = `<b>${d.title}</b> <span style="opacity:.7">(${d.herb}: ${have})</span><br><i>${d.line}</i>`
    if (have < 1) b.style.opacity = '0.5'
    b.onclick = () => {
      el.remove()
      onCook(d)
    }
    el.appendChild(b)
  }
  const close = document.createElement('div')
  close.textContent = 'Press C or click outside a dish to close.'
  close.style.cssText = 'margin-top:6px;font-size:11px;opacity:.7'
  el.appendChild(close)
  container.appendChild(el)
  return el
}
