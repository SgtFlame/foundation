import { DEFAULT_MOON, type MoonParams, type VoxelEdit } from './Types.ts'
import type { EditIntent } from './VoxelViewport.ts'

// The server's edit rules, for running without a server (the demo page and
// tests): the same checks the game.command blueprint makes. Returns the
// edit as committed, or the reason it is refused.
export const MAX_EDIT_RADIUS = 6
export const MIN_EDIT_RADIUS = 0.5

export function checkEdit(intent: EditIntent, params: MoonParams = DEFAULT_MOON): string | null {
  if (intent.kind !== 'dig' && intent.kind !== 'fill') return 'unknown edit kind'
  if (!(intent.radius >= MIN_EDIT_RADIUS && intent.radius <= MAX_EDIT_RADIUS)) return `radius must be ${MIN_EDIT_RADIUS} to ${MAX_EDIT_RADIUS} m`
  const r = Math.hypot(intent.x, intent.y, intent.z)
  if (!Number.isFinite(r) || r > params.radius * 1.5 + 50) return 'too far from the moon'
  return null
}

export class LocalEditLog {
  private seq = 0
  readonly edits: VoxelEdit[] = []

  private readonly params: () => MoonParams

  constructor(params: () => MoonParams) {
    this.params = params
  }

  commit(intent: EditIntent): VoxelEdit | string {
    const reason = checkEdit(intent, this.params())
    if (reason) return reason
    const e: VoxelEdit = { seq: ++this.seq, ...intent, ...(intent.kind === 'fill' ? { material: 0 } : {}) }
    this.edits.push(e)
    return e
  }
}
