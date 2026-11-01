// A y-websocket compatible provider for the backend's collab rooms, with our
// own control messages (role, seeded, ready, save, saved, compact, edit,
// edited) on top of the standard sync and awareness messages. A room is a
// channel on the live connection (collab.join, then binary frames on the
// channel; collab.closed when the room ends it), rejoined when the
// connection comes back.

import * as decoding from 'lib0/decoding'
import * as encoding from 'lib0/encoding'
import * as awarenessProtocol from 'y-protocols/awareness'
import * as syncProtocol from 'y-protocols/sync'
import type * as Y from 'yjs'
import { live } from '../../live/LiveConnection.ts'
import { log } from '../../logging/index.ts'

const messageSync = 0
const messageAwareness = 1
const messageControl = 100
const messageCompacted = 101

// A control message; which fields are set depends on op
export interface Control {
  op: 'role' | 'seeded' | 'ready' | 'save' | 'saved' | 'compact' | 'edit' | 'edited'
  writer?: boolean
  ready?: boolean
  dirty?: boolean
  seed?: string
  markdown?: string
  code?: number
  message?: string
  at?: number
  id?: string // edit, and the edited answer to it
  edits?: { find: string; replace: string }[]
}

export interface CollabProviderEvents {
  onConnected(connected: boolean): void
  onSynced(): void
  onControl(control: Control): void
  // The server closed the room for good, with the HTTP status (404: open
  // the file again)
  onClosed(code: number, reason: string): void
  // Rewrites each remote awareness state before it is applied
  mapAwarenessState?(state: Record<string, unknown> | null): Record<string, unknown> | null
}

const logger = log.In('collab')

export class CollabProvider {
  private readonly fileId: string
  private readonly doc: Y.Doc
  private readonly awareness: awarenessProtocol.Awareness
  private readonly events: CollabProviderEvents
  private channel?: ReturnType<typeof live.openChannel>
  private joined = false
  private attempts = 0
  private retryTimer?: ReturnType<typeof setTimeout>
  private destroyed = false
  private readonly offs: (() => void)[] = []

  constructor(fileId: string, doc: Y.Doc, awareness: awarenessProtocol.Awareness, events: CollabProviderEvents) {
    this.fileId = fileId
    this.doc = doc
    this.awareness = awareness
    this.events = events
    doc.on('update', this.onDocUpdate)
    awareness.on('update', this.onAwarenessUpdate)
    this.offs.push(
      live.on('collab.closed', (frame) => {
        if (frame.channel === this.channel?.id) this.closed(Number(frame.code), String(frame.message ?? ''))
      }),
      live.onReopen(() => this.join()),
      live.onStatus(() => {
        if (live.getStatus() !== 'open' && this.joined) this.lost()
      }),
    )
    this.join()
  }

  sendControl(control: Control) {
    this.send((encoder) => {
      encoding.writeVarUint(encoder, messageControl)
      encoding.writeVarString(encoder, JSON.stringify(control))
    })
  }

  sendCompacted(update: Uint8Array) {
    this.send((encoder) => {
      encoding.writeVarUint(encoder, messageCompacted)
      encoding.writeVarUint8Array(encoder, update)
    })
  }

  destroy() {
    this.destroyed = true
    clearTimeout(this.retryTimer)
    awarenessProtocol.removeAwarenessStates(this.awareness, [this.doc.clientID], 'local')
    this.doc.off('update', this.onDocUpdate)
    this.awareness.off('update', this.onAwarenessUpdate)
    for (const off of this.offs) off()
    if (this.channel) {
      live.send({ type: 'collab.leave', channel: this.channel.id })
      this.channel.close()
    }
  }

  // Joins the room on a fresh channel, then syncs as a new socket would
  private join() {
    if (this.destroyed) return
    this.channel?.close()
    const channel = live.openChannel((payload) => this.receive(payload))
    this.channel = channel
    if (!live.send({ type: 'collab.join', channel: channel.id, fileId: this.fileId })) return // joined on reopen
    this.joined = true
    this.attempts = 0
    logger.Tags('room').debug('joined room %s', this.fileId)
    this.events.onConnected(true)
    this.send((encoder) => {
      encoding.writeVarUint(encoder, messageSync)
      syncProtocol.writeSyncStep1(encoder, this.doc)
    })
    if (this.awareness.getLocalState() !== null) {
      this.sendAwareness([this.doc.clientID])
    }
  }

  // The connection dropped: others' cursors go until it is back
  private lost() {
    this.joined = false
    const others = [...this.awareness.getStates().keys()].filter((id) => id !== this.doc.clientID)
    awarenessProtocol.removeAwarenessStates(this.awareness, others, this)
    this.events.onConnected(false)
  }

  // The room ended our channel: for good (4000 + an HTTP status), or to be
  // joined again
  private closed(code: number, reason: string) {
    this.lost()
    this.channel?.close()
    this.channel = undefined
    if (this.destroyed) return
    if (code >= 4000) {
      logger.Tags('room').With('code', code).warn('room closed: %s', reason)
      this.events.onClosed(code - 4000, reason)
      return
    }
    const delay = Math.min(1000 * 2 ** this.attempts++, 10_000)
    logger.Tags('room').debug('left the room, joining again in %dms', delay)
    this.retryTimer = setTimeout(() => this.join(), delay)
  }

  private receive(data: Uint8Array) {
    const decoder = decoding.createDecoder(data)
    const encoder = encoding.createEncoder()
    switch (decoding.readVarUint(decoder)) {
      case messageSync: {
        encoding.writeVarUint(encoder, messageSync)
        const subtype = syncProtocol.readSyncMessage(decoder, encoder, this.doc, this)
        if (encoding.length(encoder) > 1) this.channel?.send(encoding.toUint8Array(encoder))
        if (subtype === syncProtocol.messageYjsSyncStep2) this.events.onSynced()
        break
      }
      case messageAwareness: {
        const update = decoding.readVarUint8Array(decoder)
        const map = this.events.mapAwarenessState
        awarenessProtocol.applyAwarenessUpdate(this.awareness, map ? mapAwarenessUpdate(update, map) : update, this)
        break
      }
      case messageControl:
        this.events.onControl(JSON.parse(decoding.readVarString(decoder)) as Control)
        break
    }
  }

  private send(write: (encoder: encoding.Encoder) => void) {
    if (!this.joined || !this.channel) return
    const encoder = encoding.createEncoder()
    write(encoder)
    this.channel.send(encoding.toUint8Array(encoder))
  }

  private sendAwareness(clients: number[]) {
    this.send((encoder) => {
      encoding.writeVarUint(encoder, messageAwareness)
      encoding.writeVarUint8Array(encoder, awarenessProtocol.encodeAwarenessUpdate(this.awareness, clients))
    })
  }

  private readonly onDocUpdate = (update: Uint8Array, origin: unknown) => {
    if (origin === this) return
    this.send((encoder) => {
      encoding.writeVarUint(encoder, messageSync)
      syncProtocol.writeUpdate(encoder, update)
    })
  }

  private readonly onAwarenessUpdate = (
    { added, updated, removed }: { added: number[]; updated: number[]; removed: number[] },
    origin: unknown,
  ) => {
    if (origin === this) return
    this.sendAwareness([...added, ...updated, ...removed])
  }
}

// Decodes an awareness update (count, then client ID, clock and JSON state per
// client), maps each state and encodes it again
function mapAwarenessUpdate(
  update: Uint8Array,
  map: (state: Record<string, unknown> | null) => Record<string, unknown> | null,
): Uint8Array {
  const decoder = decoding.createDecoder(update)
  const encoder = encoding.createEncoder()
  const count = decoding.readVarUint(decoder)
  encoding.writeVarUint(encoder, count)
  for (let i = 0; i < count; i++) {
    encoding.writeVarUint(encoder, decoding.readVarUint(decoder))
    encoding.writeVarUint(encoder, decoding.readVarUint(decoder))
    const state = JSON.parse(decoding.readVarString(decoder)) as Record<string, unknown> | null
    encoding.writeVarString(encoder, JSON.stringify(map(state)))
  }
  return encoding.toUint8Array(encoder)
}
