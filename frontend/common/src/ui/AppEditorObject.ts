import type { EditableObject } from '../workbench/points.ts'
import { newSessionId } from './UiSession.ts'

// An application page open as a top-level workbench tab, of the editor kind
// app:<appId>:<kind>; the tab id is app:<appId>:<kind>:<id>. The session id
// travels with the tab, so a reload resumes the session's view state.
export class AppEditorObject implements EditableObject {
  readonly kind: string
  readonly id: string
  readonly name?: string
  readonly helpText?: string
  readonly params?: unknown
  readonly session: string

  constructor({ kind, id, name, helpText, params, session }: { kind: string; id: string; name?: string; helpText?: string; params?: unknown; session?: string }) {
    this.kind = kind
    this.id = id
    this.name = name
    this.helpText = helpText
    this.params = params ?? { id }
    this.session = session ?? newSessionId()
  }
}

// app:<appId>:<kind> → { app, kind }
export function parseKind(kind: string): { app: string; kind: string } {
  const [, app = '', k = ''] = kind.split(':')
  return { app, kind: k }
}
