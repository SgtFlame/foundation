import { RingBuffer } from '../../logging/index.ts'

export type NetworkState = 'pending' | 'done' | 'failed'

// One captured request/response exchange with the backend
export interface NetworkEntry {
  id: number
  state: NetworkState
  startedAt: number
  endedAt?: number
  durationMs?: number
  method: string
  url: string
  status?: number
  statusText?: string
  requestHeaders: Record<string, string>
  requestBody?: unknown
  responseHeaders?: Record<string, string>
  responseBody?: unknown
  sizeBytes?: number
  error?: string
}

class NetworkStore extends RingBuffer<NetworkEntry> {
  // While paused, the capture interceptors record nothing
  paused = false

  update(id: number, patch: Partial<NetworkEntry>) {
    const index = this.entries.findIndex((entry) => entry.id === id)
    if (index < 0) return
    this.entries = this.entries.with(index, { ...this.entries[index], ...patch })
    this.notify()
  }

  setPaused(paused: boolean) {
    this.paused = paused
    this.notify()
  }
}

export const networkStore = new NetworkStore(1000)
