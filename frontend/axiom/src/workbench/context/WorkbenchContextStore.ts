// What the user is doing in the workbench, kept in this browser tab only
// (documentation/plans/WorkbenchContext.md): the open editors, what each one
// is editing, and the subjects recently focused or edited. Chat sends a small
// snapshot of it with every message, so the assistant can tell what "this"
// refers to. Identity is never part of it: the server knows who is asking.

// What an editor is editing, as a typed reference the server can resolve
export interface SubjectRef {
  type: string // a resource type: 'markdown', 'volume', 'logQuery', ...
  id: string // TSID as a string
  label: string // "Plan.md"
  summary?: string // one short line: "/projects/Plan.md"
}

// A line range in a text editor (from 1), or the selected rows
export interface Selection {
  lines?: [number, number]
  items?: string[]
}

export interface EditorEntry {
  tab: string // `<kind>:<id>`
  kind: string
  title: string
  subject?: SubjectRef
  selection?: Selection
  dirty?: boolean
}

export type RecentEvent = 'viewed' | 'edited' | 'created'

export interface RecentEntry {
  subject: SubjectRef
  event: RecentEvent
  at: string // RFC 3339
}

export interface WorkbenchState {
  editors: readonly EditorEntry[] // in layout order
  focused?: string // the focused editor's tab
  focusOrder: readonly string[] // tabs, most recently focused first
  recent: readonly RecentEntry[] // newest first
}

// What Chat sends with a message (backend workbenchcontext.Snapshot)
export interface WorkbenchSnapshot {
  surface: string // 'main' or 'document'
  focused?: string
  previous?: string // the editor focused before the chat surface: usually what "this" means
  editors: EditorEntry[]
  recent: RecentEntry[]
}

const maxRecent = 20
const maxSnapshotBytes = 4096

type Listener = () => void

export interface SubjectReport {
  selection?: Selection
  dirty?: boolean
}

export class WorkbenchContextStore {
  private state: WorkbenchState = { editors: [], focusOrder: [], recent: [] }
  private readonly listeners = new Set<Listener>()
  // What editors reported, kept apart from the layout's tabs so either can come first
  private readonly reports = new Map<string, { subject?: SubjectRef } & SubjectReport>()

  subscribe = (listener: Listener) => {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }

  getState = (): WorkbenchState => this.state

  // The layout's editor tabs and the focused one, from the workbench's
  // model; records a view of a subject when its tab gains focus
  setLayout(tabs: readonly { tab: string; kind: string; title: string }[], focused?: string) {
    const open = new Set(tabs.map((t) => t.tab))
    for (const tab of this.reports.keys()) if (!open.has(tab)) this.reports.delete(tab)
    const changedFocus = focused !== undefined && focused !== this.state.focused
    const focusOrder = changedFocus
      ? [focused, ...this.state.focusOrder.filter((t) => t !== focused && open.has(t))]
      : this.state.focusOrder.filter((t) => open.has(t))
    const editors = tabs.map((t) => ({ ...t, ...this.reports.get(t.tab) }))
    if (
      !changedFocus &&
      focusOrder.length === this.state.focusOrder.length &&
      sameEditors(editors, this.state.editors)
    ) {
      return
    }
    this.state = { ...this.state, editors, focused: focused ?? this.state.focused, focusOrder }
    const subject = changedFocus ? this.reports.get(focused)?.subject : undefined
    if (subject) this.pushRecent(subject, 'viewed')
    this.changed()
  }

  // An editor's report of what it is editing; undefined clears it
  report(tab: string, subject: SubjectRef | undefined, extra: SubjectReport = {}) {
    if (subject) this.reports.set(tab, { subject, ...extra })
    else this.reports.delete(tab)
    const i = this.state.editors.findIndex((e) => e.tab === tab)
    if (i < 0) return
    const editors = [...this.state.editors]
    const { kind, title } = editors[i]
    editors[i] = { tab, kind, title, ...this.reports.get(tab) }
    if (sameEditors(editors, this.state.editors)) return
    this.state = { ...this.state, editors }
    if (subject && this.state.focused === tab && !this.state.recent.some((r) => sameSubject(r.subject, subject))) {
      this.pushRecent(subject, 'viewed') // focused before it reported
    }
    this.changed()
  }

  // Records that the user edited or created the subject a tab is editing
  touch(tab: string, event: RecentEvent) {
    const subject = this.reports.get(tab)?.subject
    if (!subject) return
    this.pushRecent(subject, event)
    this.changed()
  }

  // The snapshot sent with a chat message from surfaceTab, cut to about
  // 4 KB by dropping the oldest recent entries, then the editors that are
  // neither focused nor previous
  snapshot(surface: string, surfaceTab: string): WorkbenchSnapshot {
    const { editors, focusOrder, recent } = this.state
    const previous = focusOrder.find((t) => t !== surfaceTab)
    const snap: WorkbenchSnapshot = { surface, focused: surfaceTab, previous, editors: [...editors], recent: [...recent] }
    const size = () => new TextEncoder().encode(JSON.stringify(snap)).length
    while (size() > maxSnapshotBytes && snap.recent.length > 0) snap.recent.pop()
    while (size() > maxSnapshotBytes) {
      const i = snap.editors.findLastIndex((e) => e.tab !== surfaceTab && e.tab !== previous)
      if (i < 0) break
      snap.editors.splice(i, 1)
    }
    return snap
  }

  private pushRecent(subject: SubjectRef, event: RecentEvent) {
    const at = new Date().toISOString()
    const [latest] = this.state.recent
    // Repeated edits of one subject keep one entry, moved to the front
    const rest = this.state.recent.filter((r) => !(sameSubject(r.subject, subject) && (r.event === event || r === latest)))
    this.state = { ...this.state, recent: [{ subject, event, at }, ...rest].slice(0, maxRecent) }
  }

  private changed() {
    for (const listener of this.listeners) listener()
  }
}

function sameSubject(a?: SubjectRef, b?: SubjectRef): boolean {
  return a?.type === b?.type && a?.id === b?.id
}

function sameEditors(a: readonly EditorEntry[], b: readonly EditorEntry[]): boolean {
  return a.length === b.length && a.every((e, i) => JSON.stringify(e) === JSON.stringify(b[i]))
}

// The store of this browser tab
export const workbenchContext = new WorkbenchContextStore()
