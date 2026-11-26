import { DEFAULT_MOON, type MoonParams, type VoxelEdit } from './Types.ts'
import type { Carried, EditIntent } from './VoxelViewport.ts'

// The server's edit rules, for running without a server (the demo page and
// tests): the same checks the game.command blueprint makes. Returns the
// edit as committed, or the reason it is refused.
export const MAX_EDIT_RADIUS = 6
export const MIN_EDIT_RADIUS = 0.5

export function checkEdit(intent: EditIntent, params: MoonParams = DEFAULT_MOON): string | null {
  if (intent.kind !== 'dig' && intent.kind !== 'fill') return 'unknown edit kind'
  if (!(intent.radius >= MIN_EDIT_RADIUS && intent.radius <= MAX_EDIT_RADIUS)) return `radius must be ${MIN_EDIT_RADIUS} to ${MAX_EDIT_RADIUS} m`
  const r = Math.hypot(intent.x, intent.y, intent.z)
  if (!Number.isFinite(r) || r > params.radius + params.amplitude + 150) return "the edit is too far from the world's moon and asteroids"
  return null
}

// The pack's rules, as the blueprint has them (V2): a dig while playing
// (took) adds what it takes, bounded by its sphere; a fill (needs) spends
// dust first, then rock (ore is kept, V8); the pack holds PACK_CAPACITY m³
export const PACK_CAPACITY = 4

export function packChange(
  intent: EditIntent,
  pack: Carried,
): { delta: { dust: number; rock: number; ore: number }; material: number } | string {
  const V = (4 / 3) * Math.PI * intent.radius ** 3
  const r2 = (v: number) => Math.round(Math.max(v, 0) * 100) / 100
  if (intent.kind === 'dig' && intent.took) {
    const dust = Math.min(Math.max(intent.took.dust, 0), V)
    const rock = Math.min(Math.max(intent.took.rock, 0), V - dust)
    const ore = Math.min(Math.max(intent.took.ore ?? 0, 0), V - dust - rock)
    const room = pack.capacity - pack.dust - pack.rock - (pack.ore ?? 0)
    if (dust + rock + ore > room + 0.001) return `your pack is full: it has room for ${r2(room)} m³ and this dig takes ${r2(dust + rock + ore)} m³; fill somewhere to empty it`
    return { delta: { dust, rock, ore }, material: 0 }
  }
  if (intent.kind === 'fill' && intent.needs !== undefined) {
    const n = Math.min(Math.max(intent.needs, 0), V)
    const have = pack.dust + pack.rock
    if (n > have + 0.001) return `your pack holds ${r2(have)} m³ and this fill needs ${r2(n)} m³; dig to fill it`
    const sd = Math.min(pack.dust, n)
    const sr = Math.max(n - sd, 0)
    return { delta: { dust: -sd, rock: -sr, ore: 0 }, material: sr > sd ? 1 : 0 }
  }
  return { delta: { dust: 0, rock: 0, ore: 0 }, material: 0 }
}

export class LocalEditLog {
  private seq = 0
  readonly edits: VoxelEdit[] = []
  readonly pack: Carried = { dust: 0, rock: 0, ore: 0, capacity: PACK_CAPACITY }

  private readonly params: () => MoonParams

  constructor(params: () => MoonParams) {
    this.params = params
  }

  commit(intent: EditIntent): VoxelEdit | string {
    const reason = checkEdit(intent, this.params())
    if (reason) return reason
    const change = packChange(intent, this.pack)
    if (typeof change === 'string') return change
    this.pack.dust = Math.max(0, this.pack.dust + change.delta.dust)
    this.pack.rock = Math.max(0, this.pack.rock + change.delta.rock)
    this.pack.ore = Math.max(0, (this.pack.ore ?? 0) + change.delta.ore)
    const { kind, x, y, z, radius } = intent
    const e: VoxelEdit = { seq: ++this.seq, kind, x, y, z, radius, ...(kind === 'fill' ? { material: change.material } : {}) }
    this.edits.push(e)
    return e
  }
}
