// React access to the workbench context store. The context value is only the
// store, so one editor's report never re-renders the others: components
// subscribe to what they read.

import { createContext, useContext, useEffect, useSyncExternalStore } from 'react'
import {
  workbenchContext,
  type SubjectRef,
  type SubjectReport,
  type WorkbenchContextStore,
  type WorkbenchState,
} from './WorkbenchContextStore.ts'

// Provided by WorkbenchContextProvider; the page's store by default
export const StoreContext = createContext<WorkbenchContextStore>(workbenchContext)

export function useWorkbenchContext(): WorkbenchContextStore {
  return useContext(StoreContext)
}

export function useWorkbenchState(): WorkbenchState {
  const store = useWorkbenchContext()
  return useSyncExternalStore(store.subscribe, store.getState)
}

// Reports what the editor in tabId is editing, and its selection and dirty
// state; the report goes when the editor unmounts. An editor that never
// calls this still appears, with its kind and title.
export function useWorkbenchSubject(tabId: string, subject: SubjectRef | undefined, extra: SubjectReport = {}) {
  const store = useWorkbenchContext()
  const key = JSON.stringify([subject, extra])
  useEffect(() => {
    const [s, e] = JSON.parse(key) as [SubjectRef | null, SubjectReport]
    store.report(tabId, s ?? undefined, e)
  }, [store, tabId, key])
  useEffect(() => () => store.report(tabId, undefined), [store, tabId])
}
