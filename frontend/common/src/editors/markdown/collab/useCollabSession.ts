import { useEffect, useMemo, useSyncExternalStore } from 'react'
import { CollabSession, type CollabState } from './CollabSession.ts'

// The session for a file, connected while the component is mounted;
// readOnly when the user may not write its Volume
export function useCollabSession(fileId: string, readOnly = false): CollabSession {
  const session = useMemo(() => new CollabSession(fileId, readOnly), [fileId, readOnly])
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
