// One UI session in the browser (documentation/ServerUI.md › Sessions): it
// opens a page over the live connection, keeps the tree the server sends
// and applies its patches, holds the page's local state (keystroke-level
// input that never round-trips), sends component events, and reopens the
// session (with the same id, so its view state comes back) after a
// reconnect.

import { live, newId } from '../live/LiveConnection.ts'
import { newTraceId } from '../services/http/trace.ts'
import { log } from '../logging/index.ts'
import { applyPatch, type UiAction, type UiNode, type UiOn, type UiPatch, type UiSubject } from './types.ts'

const logger = log.In('ui')

export interface UiSessionState {
  tree?: UiNode
  rev: number
  title?: string
  subject?: UiSubject
  // Drawn in its app's frame, which shows its title and presence
  framed?: boolean
  // The page could not open, or a handler failed (handler is set)
  error?: { message: string; errorCode?: string; handler?: string }
  status: 'opening' | 'open' | 'closed'
}

// Local state: values by dotted path, for local.* bindings
export class LocalState {
  private values: Record<string, unknown> = {}
  private readonly listeners = new Set<() => void>()

  get(path: string): unknown {
    return this.values[path]
  }

  set(path: string, value: unknown) {
    if (this.values[path] === value) return
    this.values = { ...this.values, [path]: value }
    for (const l of this.listeners) l()
  }

  snapshot = () => this.values

  subscribe = (listener: () => void) => {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }
}

// A new session id, kept in the tab so a reload resumes the session
export function newSessionId(): string {
  return newId('ui')
}

export class UiSession {
  readonly id: string
  readonly page: string
  readonly params: unknown
  readonly local = new LocalState()
  private state: UiSessionState = { rev: 0, status: 'opening' }
  private readonly listeners = new Set<() => void>()
  private readonly offs: (() => void)[] = []
  private onActions?: (actions: UiAction[]) => void
  // Events sent while the connection was down, sent once the session has
  // reopened
  private queued: Record<string, unknown>[] = []

  constructor(id: string, page: string, params: unknown) {
    this.id = id
    this.page = page
    this.params = params
    this.offs.push(
      live.on('ui.tree', (f) => {
        if (f.session !== this.id) return
        if (f.stale) logger.With('session', this.id).debug('the tree changed under an event; showing the current one')
        this.set({
          tree: f.tree as UiNode,
          rev: Number(f.rev),
          title: f.title as string | undefined,
          subject: f.subject as UiSubject | undefined,
          framed: !!f.framed,
          status: 'open',
          error: undefined,
        })
        const queued = this.queued
        this.queued = []
        for (const frame of queued) live.send({ ...frame, type: 'ui.event', rev: this.state.rev })
      }),
      live.on('ui.patch', (f) => {
        if (f.session !== this.id || !this.state.tree) return
        let tree = this.state.tree
        for (const p of (f.patches as UiPatch[]) ?? []) tree = applyPatch(tree, p)
        // A page whose heading follows its data sends a new title and subject
        const heading = f.title || f.subject ? { title: (f.title as string) || this.state.title, subject: (f.subject as UiSubject) ?? this.state.subject } : {}
        this.set({ ...this.state, ...heading, tree, rev: Number(f.rev) })
        const actions = f.actions as UiAction[] | undefined
        if (actions?.length) this.onActions?.(actions)
      }),
      live.on('ui.error', (f) => {
        if (f.session !== this.id) return
        const error = { message: String(f.message), errorCode: f.errorCode as string | undefined, handler: f.handler as string | undefined }
        logger.With('session', this.id, 'errorCode', error.errorCode).warn('%s', error.message)
        if (error.errorCode === 'session_not_open' || error.errorCode === 'page_changed') {
          this.open() // the server lost it (a restart), or its app was upgraded: open it again
          return
        }
        this.set({ ...this.state, error, status: this.state.tree ? this.state.status : 'closed' })
      }),
      live.onReopen(() => this.open()),
    )
    this.open()
  }

  private open() {
    live.send({ type: 'ui.open', session: this.id, page: this.page, params: this.params ?? {}, trace: newTraceId() })
  }

  getState = () => this.state

  // Sets who performs the client actions handlers return (the hosting tab)
  setActionHandler(handler: ((actions: UiAction[]) => void) | undefined) {
    this.onActions = handler
  }

  subscribe = (listener: () => void) => {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }

  private set(state: UiSessionState) {
    this.state = state
    for (const l of this.listeners) l()
  }

  // Dismisses a handler's error
  clearError() {
    this.set({ ...this.state, error: undefined })
  }

  // Sends a component event of the node at path, filling the payload's
  // local.* and event.* values
  emit(path: string[], on: UiOn, data: Record<string, unknown> = {}) {
    const payload: Record<string, unknown> = {}
    for (const [k, v] of Object.entries(on.payload ?? {})) {
      if (typeof v === 'string' && v.startsWith('local.')) payload[k] = this.local.get(v.slice(6))
      else if (typeof v === 'string' && v.startsWith('event.')) payload[k] = data[v.slice(6)]
      else if (v === 'event') payload[k] = data
      else payload[k] = v
    }
    const frame = { type: 'ui.event', session: this.id, handler: on.handler, path, payload, rev: this.state.rev, trace: newTraceId() }
    if (!live.send(frame)) this.queued.push(frame)
  }

  // Ends the session; forget (the tab was closed) drops it on the server
  close(forget: boolean) {
    for (const off of this.offs) off()
    if (forget) live.send({ type: 'ui.close', session: this.id })
    this.set({ ...this.state, status: 'closed' })
  }
}
