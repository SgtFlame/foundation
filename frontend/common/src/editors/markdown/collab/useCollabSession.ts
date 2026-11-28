import { useEffect, useMemo, useSyncExternalStore } from 'react'
import { CollabSession, type CollabState, type ContentFactory } from './CollabSession.ts'

// The session for a file, connected while the component is mounted;
// readOnly when the user may not write its Volume; content is what the
// room holds (a markdown Y.Text unless given), kept from the first render
export function useCollabSession(fileId: string, readOnly = false, content?: ContentFactory): CollabSession {
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const session = useMemo(() => new CollabSession(fileId, readOnly, content), [fileId, readOnly])
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
