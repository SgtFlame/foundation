// A query as React state: its snapshot, then its changes, until unmounted
// (documentation/Live.md › Queries are subscriptions).

import { useEffect, useState } from 'react'
import type { ServiceError } from '../services/http/client.ts'
import { live, type Delta, type Position } from './LiveConnection.ts'

export interface QueryState<T> {
  data?: T
  pos: Position
  loading: boolean
  error?: ServiceError
}

// Applies one change to a query's result
export function applyDelta<T>(data: T | undefined, delta: Delta): T {
  switch (delta.op) {
    case 'replace':
      return delta.value as T
    case 'upsert':
      return { ...(data as Record<string, unknown>), [delta.key ?? '']: delta.value } as T
    case 'remove': {
      const next = { ...(data as Record<string, unknown>) }
      delete next[delta.key ?? '']
      return next as T
    }
  }
  return data as T
}

// Subscribes to query with params while mounted; params are compared by
// their JSON, so a new object with the same values keeps the subscription
export function useQuery<T>(query: string | undefined, params: unknown = {}): QueryState<T> {
  const key = `${query ?? ''}|${JSON.stringify(params ?? {})}`
  // The state carries the key it is for, so a new query reads as loading
  // until its snapshot arrives
  const [state, setState] = useState<QueryState<T> & { key?: string }>({ pos: {}, loading: true })
  useEffect(() => {
    if (!query) return
    const params = JSON.parse(key.slice(key.indexOf('|') + 1)) as unknown
    return live.subscribe(query, params, {
      onSnapshot: (data, pos) => setState({ key, data: data as T, pos: { ...pos }, loading: false }),
      onChange: (delta, pos) =>
        setState((s) => (s.key === key ? { ...s, data: applyDelta(s.data, delta), pos: { ...s.pos, ...max(s.pos, pos) }, loading: false } : s)),
      onError: (error) => setState({ key, pos: {}, loading: false, error }),
    })
  }, [query, key])
  return state.key === key ? state : { pos: {}, loading: !!query }
}

function max(a: Position, b: Position): Position {
  const out: Position = {}
  for (const [k, v] of Object.entries(b)) out[k] = Math.max(v, a[k] ?? 0)
  return out
}
