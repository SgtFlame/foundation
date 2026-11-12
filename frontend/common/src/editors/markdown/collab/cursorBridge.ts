// The views publish the local cursor under different awareness fields:
// y-codemirror (Live Preview) uses `cursor` and y-monaco (Source) uses
// `selection`, both holding Yjs relative positions of the same shape. Each
// client says which one is current in `cursorFrom`, and receivers copy it to
// the other field, so every view shows every remote cursor.

import type { Awareness } from 'y-protocols/awareness'

type CursorField = 'cursor' | 'selection'

// The field of the editor holding focus, if it is one of ours
function focusedField(): CursorField | undefined {
  const focused = document.activeElement
  if (focused?.closest('.cm-editor')) return 'cursor'
  if (focused?.closest('.monaco-editor')) return 'selection'
  return undefined
}

// Keeps the local state's cursorFrom on the editor being typed in
export function trackCursorSource(awareness: Awareness): () => void {
  const listener = (_changes: unknown, origin: unknown) => {
    if (origin !== 'local') return
    const field = focusedField()
    const state = awareness.getLocalState()
    if (field && state && state.cursorFrom !== field) awareness.setLocalStateField('cursorFrom', field)
  }
  awareness.on('change', listener)
  return () => awareness.off('change', listener)
}

// Fills in a remote state's other cursor field from the current one
export function bridgeCursor(state: Record<string, unknown> | null): Record<string, unknown> | null {
  const from = state?.cursorFrom
  if (!state || (from !== 'cursor' && from !== 'selection')) return state
  const to: CursorField = from === 'cursor' ? 'selection' : 'cursor'
  return { ...state, [to]: state[from] ?? null }
}
