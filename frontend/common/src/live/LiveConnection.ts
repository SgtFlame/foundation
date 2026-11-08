// The live connection (documentation/Live.md): one WebSocket per browser
// window at /api/v1/live, carrying commands, subscriptions, presence, calls
// and the frames of server extensions (UI sessions, collaboration rooms).
// It reconnects with backoff, resends unacknowledged commands (the server
// drops duplicates by id), resubscribes each query after its last position
// and tells its listeners, so UI sessions and rooms reopen.

import { API_BASE, http, ServiceError } from '../services/http/client.ts'
import { newTraceId } from '../services/http/trace.ts'
import { log } from '../logging/index.ts'
import { recordFrame } from './frames.ts'

const logger = log.In('live')

// How far a subscription has seen, as a sequence per stream
export type Position = Record<string, number>

export interface Problem {
  code?: string // the rejection's error code when not command_rejected, e.g. record_changed
  path?: string
  message: string
  fix?: string
}

export interface Ack {
  id: string
  stream: string
  target?: string // the target's id, when the command made it (sent without one)
  seq: number
  duplicate?: boolean
  request?: string // the request the operation log keeps it under
}

// What a command is sent for (forge 06a): request groups the commands of
// one gesture or chat round (each is its own when left out), and app is
// the app it was made for, which logs an op on a part no app owns under it
export interface CommandOptions {
  request?: string
  app?: string
}

// A change to a query's result: replace, upsert or remove a key, or reached
// (nothing changed, but the subscription has reached the change's
// position). With list, the change is to the ordered list at that key of
// the result, and key names the item: insert (value at index), update
// (value at from), move (the item at from taken out and value put at index)
// or remove (the item at from)
export interface Delta {
  op: 'replace' | 'upsert' | 'remove' | 'reached' | 'insert' | 'update' | 'move'
  key?: string
  value?: unknown
  list?: string
  index?: number
  from?: number
}

export interface SubscriptionHandlers {
  onSnapshot(data: unknown, pos: Position): void
  onChange(delta: Delta, pos: Position): void
  onError?(error: ServiceError): void
}

// A command the server rejected, with what was wrong
export class CommandRejected extends ServiceError {
  readonly problems: Problem[]
  constructor(status: number, message: string, errorCode: string | undefined, problems: Problem[]) {
    super(status, message, errorCode)
    this.name = 'CommandRejected'
    this.problems = problems
  }
}

export type LiveStatus = 'connecting' | 'open' | 'offline'

export interface Hello {
  connection: string
  userId: string
  color: string
}

type Frame = { type: string } & Record<string, unknown>

interface PendingCommand {
  frame: Frame
  resolve(ack: Ack): void
  reject(err: Error): void
}

interface Subscription {
  query: string
  params: unknown
  pos: Position
  handlers: SubscriptionHandlers
}

interface Call {
  onEvent(event: string, data: unknown): void
  resolve(): void
  reject(err: Error): void
}

const pingEvery = 20_000
const pongWithin = 10_000

let counter = 0
// An id unique in this window: commands, subscriptions and calls
export function newId(prefix = ''): string {
  counter++
  return `${prefix}${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}${counter.toString(36)}`
}

export class LiveConnection {
  private socket?: WebSocket
  private attempts = 0
  private retry?: ReturnType<typeof setTimeout>
  private pinger?: ReturnType<typeof setInterval>
  private lastPong = 0
  private status: LiveStatus = 'offline'
  private hello?: Hello
  private started = false
  private readonly commands = new Map<string, PendingCommand>() // in the order sent
  private readonly subs = new Map<string, Subscription>()
  private readonly calls = new Map<string, Call>()
  private readonly presence = new Map<string, unknown>()
  private readonly frameListeners = new Map<string, Set<(frame: Frame) => void>>()
  private readonly openListeners = new Set<() => void>()
  private readonly statusListeners = new Set<() => void>()
  private readonly channels = new Map<number, (payload: Uint8Array) => void>()
  private nextChannel = 1

  // Opens the socket on first use
  private start() {
    if (this.started) return
    this.started = true
    this.connect()
    window.addEventListener('online', () => this.reconnectNow())
  }

  getStatus(): LiveStatus {
    return this.status
  }

  getHello(): Hello | undefined {
    return this.hello
  }

  onStatus(listener: () => void): () => void {
    this.statusListeners.add(listener)
    return () => this.statusListeners.delete(listener)
  }

  // Called each time the connection opens again after a drop, once the
  // commands and subscriptions are resent
  onReopen(listener: () => void): () => void {
    this.openListeners.add(listener)
    return () => this.openListeners.delete(listener)
  }

  // Receives the server frames of one type (ui.tree, collab.closed, …)
  on(type: string, listener: (frame: Frame) => void): () => void {
    this.start()
    let set = this.frameListeners.get(type)
    if (!set) this.frameListeners.set(type, (set = new Set()))
    set.add(listener)
    return () => set.delete(listener)
  }

  // Sends a frame now if open; frames are not queued, so extensions resend
  // what they need from onReopen
  send(frame: Frame): boolean {
    this.start()
    if (this.socket?.readyState !== WebSocket.OPEN) return false
    const text = JSON.stringify(frame)
    recordFrame('out', frame.type, text)
    this.socket.send(text)
    return true
  }

  // Sends a command; resolves with its ack once committed (or when there
  // was nothing to commit), rejects with CommandRejected. While offline it
  // waits, and it is resent until acknowledged.
  command(command: string, target: string, args?: unknown, options?: CommandOptions): Promise<Ack> {
    const id = newId('c')
    const frame: Frame = { type: 'command', id, command, target, args, request: options?.request, app: options?.app, trace: newTraceId() }
    return new Promise<Ack>((resolve, reject) => {
      this.commands.set(id, { frame, resolve, reject })
      this.send(frame)
    })
  }

  // Opens a query; the handlers get its snapshot and changes until the
  // returned function closes it. A reconnect resumes it after its position.
  subscribe(query: string, params: unknown, handlers: SubscriptionHandlers): () => void {
    const sub = newId('s')
    this.subs.set(sub, { query, params, pos: {}, handlers })
    this.send({ type: 'subscribe', sub, query, params, trace: newTraceId() })
    return () => {
      if (this.subs.delete(sub)) this.send({ type: 'unsubscribe', sub })
    }
  }

  // Sets this window's presence in an object ("<type>.<id>"); undefined
  // leaves it
  setPresence(object: string, state: unknown) {
    if (state === undefined) {
      this.presence.delete(object)
      this.send({ type: 'presence', object, leave: true })
      return
    }
    this.presence.set(object, state)
    this.send({ type: 'presence', object, state })
  }

  // Runs a call, giving each event to onEvent, until it ends; aborting
  // cancels it. A connection lost mid-call fails it with status 0.
  call(call: string, args: unknown, onEvent: (event: string, data: unknown) => void, signal?: AbortSignal): Promise<void> {
    const id = newId('k')
    return new Promise<void>((resolve, reject) => {
      if (signal?.aborted) {
        reject(new DOMException('aborted', 'AbortError'))
        return
      }
      const send = () => {
        if (!this.send({ type: 'call', id, call, args, trace: newTraceId() })) {
          // Not open yet: start it once the connection opens
          const off = this.onReopen(() => {
            off()
            if (this.calls.has(id)) this.send({ type: 'call', id, call, args, trace: newTraceId() })
          })
        }
      }
      this.calls.set(id, { onEvent, resolve, reject })
      signal?.addEventListener('abort', () => {
        if (this.calls.delete(id)) {
          this.send({ type: 'cancel', id })
          reject(new DOMException('aborted', 'AbortError'))
        }
      })
      send()
    })
  }

  // A binary channel for an extension (a collaboration room)
  openChannel(onMessage: (payload: Uint8Array) => void): { id: number; send(payload: Uint8Array): void; close(): void } {
    this.start()
    const id = this.nextChannel++
    this.channels.set(id, onMessage)
    return {
      id,
      send: (payload) => {
        if (this.socket?.readyState !== WebSocket.OPEN) return
        const msg = new Uint8Array(4 + payload.length)
        new DataView(msg.buffer).setUint32(0, id)
        msg.set(payload, 4)
        recordFrame('out', 'binary', msg)
        this.socket.send(msg)
      },
      close: () => this.channels.delete(id),
    }
  }

  private setStatus(status: LiveStatus) {
    if (this.status === status) return
    this.status = status
    for (const l of this.statusListeners) l()
  }

  private url(): string {
    const url = new URL(`${API_BASE}/live`, window.location.href)
    url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
    return url.toString()
  }

  private reconnectNow() {
    if (this.socket) return
    clearTimeout(this.retry)
    this.attempts = 0
    this.connect()
  }

  private connect() {
    this.setStatus('connecting')
    const socket = new WebSocket(this.url())
    socket.binaryType = 'arraybuffer'
    this.socket = socket
    let opened = false

    socket.onopen = () => {
      opened = true
      this.attempts = 0
      this.lastPong = Date.now()
      this.setStatus('open')
      logger.Tags('socket').debug('connected')
      this.resume()
      this.pinger = setInterval(() => this.ping(), pingEvery)
    }
    socket.onmessage = (event: MessageEvent<string | ArrayBuffer>) => {
      if (typeof event.data === 'string') this.receive(event.data)
      else this.receiveBinary(new Uint8Array(event.data))
    }
    socket.onclose = (event) => {
      if (this.socket !== socket) return
      this.socket = undefined
      clearInterval(this.pinger)
      this.setStatus('offline')
      for (const [id, c] of this.calls) {
        this.calls.delete(id)
        c.reject(new ServiceError(0, 'the connection to the server was lost'))
      }
      // A refused upgrade (401) closes before opening, as does a server
      // that is down. An authenticated request tells them apart: a 401
      // refreshes the session as for any request, while a server that is
      // down fails it and leaves the session alone
      const retry = () => {
        const delay = Math.min(500 * 2 ** this.attempts++, 15_000)
        logger.Tags('socket').With('code', event.code).debug('disconnected, retrying in %dms', delay)
        this.retry = setTimeout(() => this.connect(), delay)
      }
      if (!opened && this.attempts > 0) void http.get('/authz/me', { capture: false }).catch(() => undefined).finally(retry)
      else retry()
    }
  }

  private ping() {
    if (Date.now() - this.lastPong > pingEvery + pongWithin) {
      logger.Tags('socket').warn('no answer from the server; reconnecting')
      this.socket?.close()
      return
    }
    this.send({ type: 'ping' })
  }

  // Resends what the server must know after a reconnect
  private resume() {
    for (const [sub, s] of this.subs) {
      this.send({ type: 'subscribe', sub, query: s.query, params: s.params, after: s.pos, trace: newTraceId() })
    }
    for (const [object, state] of this.presence) this.send({ type: 'presence', object, state })
    for (const c of this.commands.values()) this.send(c.frame)
    for (const l of this.openListeners) l()
  }

  private receive(text: string) {
    let frame: Frame
    try {
      frame = JSON.parse(text) as Frame
    } catch {
      return
    }
    recordFrame('in', frame.type, text)
    switch (frame.type) {
      case 'pong':
        this.lastPong = Date.now()
        return
      case 'hello':
        this.hello = { connection: String(frame.connection), userId: String(frame.userId), color: String(frame.color) }
        for (const l of this.statusListeners) l()
        return
      case 'ack': {
        const c = this.commands.get(String(frame.id))
        if (c) {
          this.commands.delete(String(frame.id))
          c.resolve(frame as unknown as Ack)
        }
        return
      }
      case 'rejected': {
        const c = this.commands.get(String(frame.id))
        if (c) {
          this.commands.delete(String(frame.id))
          c.reject(new CommandRejected(Number(frame.code), String(frame.message), frame.errorCode as string | undefined, (frame.problems as Problem[]) ?? []))
        }
        return
      }
      case 'snapshot':
      case 'change': {
        const s = this.subs.get(String(frame.sub))
        if (!s) return
        const pos = (frame.pos as Position) ?? {}
        for (const [k, v] of Object.entries(pos)) if (v > (s.pos[k] ?? 0)) s.pos[k] = v
        if (frame.type === 'snapshot') s.handlers.onSnapshot(frame.data, pos)
        else s.handlers.onChange(frame.delta as Delta, pos)
        return
      }
      case 'subError': {
        const s = this.subs.get(String(frame.sub))
        if (!s) return
        this.subs.delete(String(frame.sub))
        const err = new ServiceError(Number(frame.code), String(frame.message), frame.errorCode as string | undefined)
        logger.fromError(err).With('query', s.query).warn('subscription to %s failed', s.query)
        s.handlers.onError?.(err)
        return
      }
      case 'event': {
        this.calls.get(String(frame.id))?.onEvent(String(frame.event), frame.data)
        return
      }
      case 'end': {
        const c = this.calls.get(String(frame.id))
        if (!c) return
        this.calls.delete(String(frame.id))
        const code = Number(frame.code)
        if (code >= 300) c.reject(new ServiceError(code, String(frame.message ?? ''), frame.errorCode as string | undefined))
        else c.resolve()
        return
      }
    }
    const listeners = this.frameListeners.get(frame.type)
    if (listeners) for (const l of listeners) l(frame)
  }

  private receiveBinary(msg: Uint8Array) {
    if (msg.length < 4) return
    recordFrame('in', 'binary', msg)
    const channel = new DataView(msg.buffer, msg.byteOffset).getUint32(0)
    this.channels.get(channel)?.(msg.subarray(4))
  }
}

// The window's live connection
export const live = new LiveConnection()
