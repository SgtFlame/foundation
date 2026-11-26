// The senses layer (forge 375 S12; the design's rule 6): smells and sounds
// shown as soft cues at the screen's edge, pointing toward their source
// without a minimap marker. Focus (hold F) sharpens them: they reach
// further, and the clearest names where it comes from. Placeholder cues
// from the reference's list (burnt sugar and old iron, old pennies, owls
// going silent) and the first day's own (the herbs, the choked culvert).

export interface Source {
  id: string
  x: number
  z: number
  sense: 'smell' | 'sound'
  text: string // what the character senses
  from: string // where it comes from, named when focused
  range: number // metres it carries, unfocused
}

export interface Cue {
  id: string
  sense: Source['sense']
  text: string
  strength: number // 0 to 1, by distance
  angle: number // radians in the world's x/z plane, from the player toward the source (0 = +x)
  dist: number
}

export const FOCUS_REACH = 1.75 // how much further focus carries a sense
export const MAX_CUES = 3

// The cues a player at (x, z) senses: those within reach but not at hand
// (within 3 m the thing is seen, not sensed), strongest first
export function senseCues(x: number, z: number, sources: readonly Source[], focus: boolean): Cue[] {
  const cues: Cue[] = []
  for (const s of sources) {
    const d = Math.hypot(s.x - x, s.z - z)
    const reach = s.range * (focus ? FOCUS_REACH : 1)
    if (d < 3 || d > reach) continue
    const strength = Math.min(1, 1.15 - d / reach)
    cues.push({ id: s.id, sense: s.sense, text: s.text, strength, angle: Math.atan2(s.z - z, s.x - x), dist: d })
  }
  cues.sort((a, b) => b.strength - a.strength)
  const out = cues.slice(0, MAX_CUES)
  // focus names where the clearest one comes from
  if (focus && out.length) {
    const src = sources.find((s) => s.id === out[0].id)!
    out[0] = { ...out[0], text: `${out[0].text}, ${src.from}` }
  }
  return out
}

// Where a cue sits on a w × h screen: along the direction (dx, dy) from the
// screen's centre to its source's projection, out to a box inset from the
// edges (more at the bottom, above the caption and the hint)
export function edgePoint(dx: number, dy: number, w: number, h: number, inset = { side: 120, top: 60, bottom: 130 }): { x: number; y: number } {
  const l = Math.hypot(dx, dy) || 1
  const ux = dx / l
  const uy = dy / l
  const cx = w / 2
  const cy = h / 2
  const tx = ux > 0 ? (w - inset.side - cx) / ux : ux < 0 ? (inset.side - cx) / ux : Infinity
  const ty = uy > 0 ? (h - inset.bottom - cy) / uy : uy < 0 ? (inset.top - cy) / uy : Infinity
  const t = Math.max(0, Math.min(tx, ty))
  return { x: cx + ux * t, y: cy + uy * t }
}

// Moves cues apart that would overlap (each later one steps toward the
// screen's middle until clear)
export function spread(points: { x: number; y: number }[], h: number, gap = 34): { x: number; y: number }[] {
  const out: { x: number; y: number }[] = []
  for (const p of points) {
    const q = { ...p }
    const step = q.y > h / 2 ? -gap : gap
    for (let i = 0; i < 6 && out.some((o) => Math.abs(o.x - q.x) < 200 && Math.abs(o.y - q.y) < gap); i++) q.y += step
    out.push(q)
  }
  return out
}

// The cues' layer: one soft label per cue, faded by strength
export class SensesLayer {
  readonly el: HTMLDivElement
  private readonly labels = new Map<string, HTMLDivElement>()

  constructor(container: HTMLElement) {
    this.el = document.createElement('div')
    this.el.dataset.senses = ''
    this.el.style.cssText = 'position:absolute;inset:0;pointer-events:none;z-index:2;overflow:hidden'
    container.appendChild(this.el)
  }

  // Shows cues at screen points (from edgePoint), dropping the rest
  show(cues: readonly (Cue & { at: { x: number; y: number } })[]): void {
    const keep = new Set(cues.map((c) => c.id))
    for (const [id, el] of this.labels) {
      if (!keep.has(id)) {
        el.remove()
        this.labels.delete(id)
      }
    }
    for (const c of cues) {
      let el = this.labels.get(c.id)
      if (!el) {
        el = document.createElement('div')
        el.dataset.sense = c.id
        el.style.cssText =
          'position:absolute;transform:translate(-50%,-50%);max-width:220px;padding:4px 10px;border-radius:14px;text-align:center;' +
          'font:italic 13px Georgia,serif;color:#efe2c4;background:rgba(20,16,11,0.55);transition:opacity 0.6s;white-space:normal'
        this.el.appendChild(el)
        this.labels.set(c.id, el)
      }
      el.textContent = `${c.sense === 'smell' ? '≈' : '♪'} ${c.text}`
      el.style.left = `${c.at.x.toFixed(0)}px`
      el.style.top = `${c.at.y.toFixed(0)}px`
      el.style.opacity = (0.25 + 0.75 * c.strength).toFixed(2)
    }
  }

  dispose(): void {
    this.el.remove()
  }
}
