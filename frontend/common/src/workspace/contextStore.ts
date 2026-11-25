// The active Volumes and Chronicle entries of the workspace this tab shows
// (forge 16c): read from the live workspace list, so every window follows
// at once, and written through one ContextWriter per workspace.
import { useEffect, useSyncExternalStore } from 'react'
import { live } from '../live/index.ts'
import { log } from '../logging/Logger.ts'
import { ContextWriter, isChecked, type WorkspaceContext } from './context.ts'
import { workspaces, type Workspace } from './workspaceStore.ts'

const ctxLog = log.In('workspace')

export interface ActiveKnowledge {
  workspace?: Workspace
  context: WorkspaceContext
  checked(kind: string, id: string): boolean
  setChecked(kind: string, id: string, on: boolean): void
  // The last write that failed, for the view to show
  error?: string
}

const writers = new Map<string, ContextWriter>()
const listeners = new Set<() => void>()
let epoch = 0
let error: string | undefined
let snapshot: { key: string; value: ActiveKnowledge } | undefined
let following = false

function notify() {
  epoch++
  listeners.forEach((l) => l())
}

function writerFor(id: string): ContextWriter {
  let w = writers.get(id)
  if (!w) {
    w = new ContextWriter({
      seen: () => {
        const ws = workspaces.get().workspaces.find((x) => x.id === id)
        return ws && { version: ws.version, context: ws.context }
      },
      send: async (version, context) => {
        error = undefined
        try {
          await live.command('workspace.update', id, { version, context })
        } finally {
          queueMicrotask(notify)
        }
      },
      failed: (err) => {
        ctxLog.fromError(err).With('workspace', id).warn('cannot save the active Volumes and entries')
        error = err instanceof Error ? err.message : String(err)
        notify()
      },
    })
    writers.set(id, w)
  }
  return w
}

// The list changed: writers drop toggles the list now holds, and resend stale ones
function follow() {
  if (following) return
  following = true
  workspaces.subscribe(() => {
    writers.forEach((w) => w.versionSeen())
    notify()
  })
}

function setChecked(kind: string, id: string, on: boolean) {
  const current = workspaces.get().current
  if (!current) return
  writerFor(current.id).setChecked(kind, id, on)
  notify()
}

function get(): ActiveKnowledge {
  const current = workspaces.get().current
  const key = `${epoch}|${current?.id}|${current?.version}`
  if (snapshot?.key === key) return snapshot.value
  const context = current ? writerFor(current.id).effective() : {}
  const value: ActiveKnowledge = { workspace: current, context, checked: (kind, id) => isChecked(context, kind, id), setChecked, error }
  snapshot = { key, value }
  return value
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

// The tab's workspace's active Volumes and Chronicle entries, and how to
// check one
export function useActiveKnowledge(): ActiveKnowledge {
  useEffect(() => {
    workspaces.start()
    follow()
  }, [])
  return useSyncExternalStore(subscribe, get)
}
