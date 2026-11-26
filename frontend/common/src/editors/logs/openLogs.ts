// Opens the Log Explorer on a query from places that have no EditorRegistry,
// such as DevTools: the workbench listens and opens it.

import type { LogsView } from './view.ts'

type Listener = (view: LogsView) => void

const listeners = new Set<Listener>()

export function openLogs(view: LogsView) {
  for (const listener of listeners) listener(view)
}

// The workbench subscribes; returns a function that unsubscribes
export function onOpenLogs(listener: Listener): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}
