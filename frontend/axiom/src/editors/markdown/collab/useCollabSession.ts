import { useEffect, useMemo, useSyncExternalStore } from 'react'
import { CollabSession, type CollabState } from './CollabSession.ts'

// The session for a file, connected while the component is mounted
export function useCollabSession(fileId: string): CollabSession {
  const session = useMemo(() => new CollabSession(fileId), [fileId])
  useEffect(() => {
    session.start()
    return () => session.stop()
  }, [session])
  return session
}

// Subscribes a component to a session's state
export function useCollabState(session: CollabSession): CollabState {
  return useSyncExternalStore(session.subscribe, session.snapshot)
}
