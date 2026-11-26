// The live connection's recent frames, for DevTools' Live tab: the Network
// tab shows a WebSocket only as one row. Binary frames (collaboration
// rooms) are recorded by size only.

import { RingBuffer } from '../logging/index.ts'

export interface LiveFrame {
  seq: number
  at: number
  direction: 'in' | 'out'
  type: string
  size: number
  text?: string // JSON frames, cut to 4 KB
}

export const liveFrames = new RingBuffer<LiveFrame>(500)

let seq = 0
let paused = false

export function setFramesPaused(p: boolean) {
  paused = p
}

export function recordFrame(direction: 'in' | 'out', type: string, body: string | Uint8Array) {
  if (paused || type === 'ping' || type === 'pong') return
  const size = typeof body === 'string' ? body.length : body.length
  liveFrames.push({ seq: ++seq, at: Date.now(), direction, type, size, text: typeof body === 'string' ? body.slice(0, 4096) : undefined })
}
