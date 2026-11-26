// The browser's copy of the principal's Chronicle entries, links and entry
// types, shared by the Chronicles view, the entry editor and the chat.
// Loaded on first use; refresh() after anything changes them.

import { useEffect, useSyncExternalStore } from 'react'
import { log } from '../logging/index.ts'
import { Chronicle, type Entry, type EntryInput, type EntryType, type Link } from '../services/Chronicle.ts'

const chronicleLog = log.In('chronicle')

export interface ChronicleState {
  loaded: boolean
  entries: readonly Entry[]
  links: readonly Link[]
  types: readonly EntryType[]
  error?: string
}

let state: ChronicleState = { loaded: false, entries: [], links: [], types: [] }
const listeners = new Set<() => void>()
let loading: Promise<void> | undefined

function set(next: Partial<ChronicleState>) {
  state = { ...state, ...next }
  listeners.forEach((l) => l())
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

// Reloads the entries and links (and the types, the first time)
export function refreshChronicles(): Promise<void> {
  loading ??= (async () => {
    try {
      const [list, types] = await Promise.all([Chronicle.list(), state.types.length ? undefined : Chronicle.types()])
      set({ loaded: true, entries: list.entries ?? [], links: list.links ?? [], types: types?.types ?? state.types, error: undefined })
    } catch (err) {
      chronicleLog.fromError(err).warn('cannot load the Chronicle entries')
      set({ loaded: true, error: err instanceof Error ? err.message : String(err) })
    } finally {
      loading = undefined
    }
  })()
  return loading
}

// The entries, links and types, loaded on first use
export function useChronicles(): ChronicleState {
  const current = useSyncExternalStore(subscribe, () => state)
  useEffect(() => {
    if (!state.loaded) void refreshChronicles()
  }, [])
  return current
}

export function typeOf(types: readonly EntryType[], id: string): EntryType | undefined {
  return types.find((t) => t.id === id)
}

// The entry's id followed by its parents' ids, nearest first, stopping at a cycle
export function ancestorsOf(entries: readonly Entry[], id: string): string[] {
  const byId = new Map(entries.map((e) => [e.id, e]))
  const out: string[] = []
  for (let parent = byId.get(id)?.parentId; parent && !out.includes(parent) && byId.has(parent); parent = byId.get(parent)?.parentId) {
    out.push(parent)
  }
  return out
}

// What the user edits of an entry, for an update. The entry must be one read
// with Chronicle.get: list leaves out the full summary and context, which an
// update would otherwise clear.
export function inputOf(e: Entry): EntryInput {
  return {
    type: e.type, title: e.title, shortSummary: e.shortSummary, fullSummary: e.fullSummary ?? '', fullContext: e.fullContext ?? '',
    fields: e.fields, parentId: e.parentId,
  }
}
