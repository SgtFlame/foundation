import { useSyncExternalStore } from 'react'
import type { RingBuffer } from '../../logging/index.ts'

// Subscribes a component to a RingBuffer singleton
export function useStore<T>(store: RingBuffer<T>): readonly T[] {
  return useSyncExternalStore(store.subscribe, store.snapshot)
}
