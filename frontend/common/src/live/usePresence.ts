// Presence in an object: sets this window's state there while mounted and
// returns everyone present, from the live.presence query
// (documentation/Live.md › Presence).

import { useEffect, useSyncExternalStore } from 'react'
import { live } from './LiveConnection.ts'
import { useQuery } from './useQuery.ts'

export interface PresentUser {
  connection: string
  userId: string
  name: string
  color: string
  // Who is there for the user when not them by hand: 'builder', the chat
  // working for them (forge 06g), set by the server for a round
  actor?: string
  state?: Record<string, unknown>
  at: string
}

export interface Presence {
  // Everyone present, this window included
  all: PresentUser[]
  // The other windows, this user's other windows included
  others: PresentUser[]
  me?: string // this window's connection id
}

// object is "<type>.<id>"; state is what this window has selected or is
// doing there (undefined keeps it present with no state)
export function usePresence(object: string | undefined, state?: Record<string, unknown>): Presence {
  const stateKey = JSON.stringify(state ?? {})
  useEffect(() => {
    if (!object) return
    live.setPresence(object, JSON.parse(stateKey))
  }, [object, stateKey])
  useEffect(() => {
    if (!object) return
    return () => live.setPresence(object, undefined)
  }, [object])
  const me = useSyncExternalStore(
    (l) => live.onStatus(l),
    () => live.getHello()?.connection,
  )
  const { data } = useQuery<PresentUser[]>(object ? 'live.presence' : undefined, { object })
  const all = data ?? []
  return { all, others: all.filter((u) => u.connection !== me), me }
}
