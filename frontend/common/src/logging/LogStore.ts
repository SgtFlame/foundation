import type { LogRecord } from './LogRecord.ts'

type Listener = () => void

// A bounded, subscribable buffer that drops its oldest entries once full.
// snapshot() returns a new array after every change so it can back
// useSyncExternalStore.
export class RingBuffer<T> {
  readonly capacity: number
  protected entries: T[] = []
  private readonly listeners = new Set<Listener>()

  constructor(capacity: number) {
    this.capacity = capacity
  }

  push(entry: T) {
    const entries = [...this.entries, entry]
    this.entries =
      entries.length > this.capacity
        ? entries.slice(entries.length - this.capacity)
        : entries
    this.notify()
  }

  clear() {
    this.entries = []
    this.notify()
  }

  snapshot = (): readonly T[] => this.entries

  subscribe = (listener: Listener) => {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }

  protected notify() {
    for (const listener of this.listeners) listener()
  }
}

export const logStore = new RingBuffer<LogRecord>(1000)
