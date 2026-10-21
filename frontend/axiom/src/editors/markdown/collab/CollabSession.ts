// One open markdown file: the shared Yjs document all three views bind to,
// its connection to the file's room, and the writer duties (seeding the
// document from the file and autosaving it) when the server elects us.

import { Awareness } from 'y-protocols/awareness'
import * as Y from 'yjs'
import { log } from '../../../logging/index.ts'
import { Collab } from '../../../services/Collab.ts'
import { CollabProvider, type Control } from '../../../services/collab/CollabProvider.ts'
import { bridgeCursor, trackCursorSource } from './cursorBridge.ts'
import { localUser, type LocalUser } from './localUser.ts'

export type CollabStatus = 'connecting' | 'syncing' | 'ready' | 'offline' | 'closed'

export interface Peer extends LocalUser {
  clientId: number
}

export interface CollabState {
  status: CollabStatus
  loaded: boolean // the document has been ready at least once, so views can show it
  writer: boolean
  saving: boolean
  lastSavedAt?: number
  saveError?: string
  closedReason?: string
  peers: readonly Peer[]
}

const saveDelayMs = 1000

const logger = log.In('collab').Tags('session')

type Listener = () => void

export class CollabSession {
  readonly fileId: string
  readonly doc = new Y.Doc()
  readonly ytext = this.doc.getText('markdown')
  readonly awareness = new Awareness(this.doc)
  private provider?: CollabProvider
  private readonly listeners = new Set<Listener>()
  private state: CollabState = { status: 'connecting', loaded: false, writer: false, saving: false, peers: [] }
  private synced = false
  private ready = false
  private lastSavedText?: string
  private saveTimer?: ReturnType<typeof setTimeout>
  private untrackCursor?: () => void

  constructor(fileId: string) {
    this.fileId = fileId
  }

  // Connects to the file's room; stop() disconnects, and the session can be started again
  start() {
    // setLocalState, not setLocalStateField: stop() cleared the state, and a field cannot be set on none
    this.awareness.setLocalState({ user: localUser() })
    this.awareness.on('change', this.onAwarenessChange)
    this.untrackCursor = trackCursorSource(this.awareness)
    this.doc.on('update', this.onDocUpdate)
    window.addEventListener('pagehide', this.flush)
    this.provider = new CollabProvider(Collab.socketUrl(this.fileId), this.doc, this.awareness, {
      onConnected: (connected) => {
        this.synced = false
        this.ready = false
        this.update({ status: connected ? 'syncing' : 'offline' })
      },
      onSynced: () => {
        this.synced = true
        this.updateStatus()
      },
      onControl: this.onControl,
      onClosed: (_code, reason) => this.update({ status: 'closed', closedReason: reason }),
      mapAwarenessState: bridgeCursor,
    })
  }

  stop() {
    this.flush()
    window.removeEventListener('pagehide', this.flush)
    this.awareness.off('change', this.onAwarenessChange)
    this.untrackCursor?.()
    this.doc.off('update', this.onDocUpdate)
    this.provider?.destroy()
    this.provider = undefined
    this.update({ status: 'connecting', writer: false, saving: false, peers: [] })
  }

  snapshot = (): CollabState => this.state

  subscribe = (listener: Listener) => {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }

  private readonly onControl = (control: Control) => {
    switch (control.op) {
      case 'role':
        this.update({ writer: !!control.writer })
        if (control.seed !== undefined) {
          // A fresh room: load the file, then let everyone else edit
          if (control.seed) this.ytext.insert(0, control.seed)
          this.lastSavedText = control.seed
          this.provider?.sendControl({ op: 'seeded' })
          this.ready = true
        } else {
          this.ready = !!control.ready
        }
        if (control.writer && control.dirty) this.save()
        this.updateStatus()
        break
      case 'ready':
        this.ready = true
        this.updateStatus()
        break
      case 'saved':
        if (control.code === 200) this.lastSavedText = this.ytext.toString()
        this.update({
          saving: false,
          lastSavedAt: control.code === 200 ? control.at : this.state.lastSavedAt,
          saveError: control.code === 200 ? undefined : control.message,
        })
        break
      case 'compact':
        this.provider?.sendCompacted(Y.encodeStateAsUpdate(this.doc))
        break
    }
  }

  private readonly onDocUpdate = () => {
    if (!this.state.writer || !this.ready) return
    clearTimeout(this.saveTimer)
    this.saveTimer = setTimeout(() => this.save(), saveDelayMs)
  }

  // Saves now if a save is pending, e.g. when the page is closing
  private readonly flush = () => {
    if (this.saveTimer === undefined) return
    this.save()
  }

  private save() {
    clearTimeout(this.saveTimer)
    this.saveTimer = undefined
    const markdown = this.ytext.toString()
    if (!this.state.writer || markdown === this.lastSavedText) return
    logger.With('fileId', this.fileId).debug('saving %d characters', markdown.length)
    this.provider?.sendControl({ op: 'save', markdown })
    this.update({ saving: true })
  }

  private readonly onAwarenessChange = () => {
    const peers: Peer[] = []
    this.awareness.getStates().forEach((state, clientId) => {
      const user = state.user as LocalUser | undefined
      if (clientId !== this.doc.clientID && user) peers.push({ clientId, ...user })
    })
    this.update({ peers })
  }

  private updateStatus() {
    if (this.state.status === 'closed' || this.state.status === 'offline') return
    const ready = this.synced && this.ready
    this.update({ status: ready ? 'ready' : 'syncing', loaded: this.state.loaded || ready })
  }

  private update(patch: Partial<CollabState>) {
    this.state = { ...this.state, ...patch }
    for (const listener of this.listeners) listener()
  }
}
