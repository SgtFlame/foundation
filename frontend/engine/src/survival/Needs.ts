// The body's needs (forge 360 V10, a thin 362e): air, how well fed and
// health, each 0 to 100. Air drains in vacuum and fills again in a
// planet's air, a ship or the outpost; hunger drains always and a ration
// answers it; health falls with no air or no food and comes back slowly
// when both are fine. The rates are world settings for the user to tune.

export interface Needs {
  air: number
  fed: number
  health: number
}

export const NEEDS = {
  airSeconds: 240, // a full suit in vacuum
  refillPerSecond: 25,
  fedMinutes: 30, // full to empty
  ration: 35, // what eating one ration gives
  suffocatePerSecond: 6,
  starvePerSecond: 0.25,
  healPerSecond: 0.5,
}

export const FRESH: Needs = { air: 100, fed: 80, health: 100 }

export interface Surroundings {
  breathable: boolean
}

const clamp = (v: number) => Math.max(0, Math.min(100, v))

// One step of the needs over dt seconds
export function stepNeeds(n: Needs, s: Surroundings, dt: number, rates = NEEDS): Needs {
  const air = clamp(s.breathable ? n.air + rates.refillPerSecond * dt : n.air - (100 / rates.airSeconds) * dt)
  const fed = clamp(n.fed - (100 / (rates.fedMinutes * 60)) * dt)
  let health = n.health
  if (air <= 0) health -= rates.suffocatePerSecond * dt
  else if (fed <= 0) health -= rates.starvePerSecond * dt
  else if (fed > 20) health += rates.healPerSecond * dt
  return { air, fed, health: clamp(health) }
}

// Eating one ration
export function eat(n: Needs, rates = NEEDS): Needs {
  return { ...n, fed: clamp(n.fed + rates.ration) }
}

// Whether a ration would do any good
export function hungry(n: Needs): boolean {
  return n.fed < 90
}

// The warning to show, most urgent first
export function warning(n: Needs, s: Surroundings, rates = NEEDS): string | null {
  if (!s.breathable && n.air < 35) return n.air <= 0 ? 'No air! Get to a ship, the outpost or the planet' : `Air low: ${Math.ceil((n.air / 100) * rates.airSeconds)} s left`
  if (n.health < 30) return 'Badly hurt'
  if (n.fed <= 0) return 'Starving: eat a ration (E)'
  if (n.fed < 20) return 'Hungry: eat a ration (E)'
  return null
}

// A need saved on the server is whole numbers
export function rounded(n: Needs): Needs {
  return { air: Math.round(n.air), fed: Math.round(n.fed), health: Math.round(n.health) }
}

// Three bars at the bottom left
export class NeedsHud {
  readonly el: HTMLDivElement
  private bars: Record<keyof Needs, HTMLDivElement> = {} as Record<keyof Needs, HTMLDivElement>
  private warn: HTMLDivElement

  constructor(container: HTMLElement) {
    const el = document.createElement('div')
    el.dataset.needs = ''
    el.style.cssText = 'position:absolute;right:12px;bottom:44px;width:170px;z-index:2;font:11px system-ui,sans-serif;color:#e8eef4;pointer-events:none;display:none'
    const rows: [keyof Needs, string, string][] = [
      ['air', 'Air', '#7fc8ff'],
      ['fed', 'Food', '#e8b860'],
      ['health', 'Health', '#e86a6a'],
    ]
    for (const [k, label, color] of rows) {
      const row = document.createElement('div')
      row.style.cssText = 'display:flex;align-items:center;gap:6px;margin-top:3px'
      const name = document.createElement('span')
      name.textContent = label
      name.style.cssText = 'width:42px;text-shadow:0 1px 2px #000'
      const track = document.createElement('div')
      track.style.cssText = 'flex:1;height:7px;background:rgba(0,0,0,.55);border-radius:4px;overflow:hidden'
      const bar = document.createElement('div')
      bar.dataset.need = k
      bar.style.cssText = `height:100%;width:100%;background:${color}`
      track.appendChild(bar)
      row.append(name, track)
      el.appendChild(row)
      this.bars[k] = bar
    }
    this.warn = document.createElement('div')
    this.warn.dataset.needsWarning = ''
    this.warn.style.cssText = 'margin-top:5px;color:#ffb0a0;font-weight:600;text-shadow:0 1px 2px #000'
    el.appendChild(this.warn)
    container.appendChild(el)
    this.el = el
  }

  show(n: Needs | null, s: Surroundings): void {
    this.el.style.display = n ? 'block' : 'none'
    if (!n) return
    for (const k of ['air', 'fed', 'health'] as const) this.bars[k].style.width = `${n[k]}%`
    this.warn.textContent = warning(n, s) ?? ''
  }
}
