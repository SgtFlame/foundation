// The person's workspaces in the browser (forge 16a): one workspace.list
// subscription, and which workspace this tab shows. Every part that follows
// the workspace (16b's layout, 16c's active Volumes and entries, 16d's
// chats) listens through useWorkspace().
import { useEffect, useSyncExternalStore } from 'react'
import { applyDelta, live } from '../live/index.ts'
import { newRecordId } from '../services/Records.ts'
import { session } from '../session/session.ts'
import { log } from '../logging/Logger.ts'
import { workbenchContext } from '../workbench/context/WorkbenchContextStore.ts'
import { chooseCurrent, copyName, defaultWorkspaceId, type Workspace } from './current.ts'

export type { Workspace } from './current.ts'

const wsLog = log.In('workspace')

export interface WorkspaceState {
  workspaces: Workspace[]
  loaded: boolean
  // The workspace this tab shows; undefined until the list arrives
  current?: Workspace
}

// Storage keys per tenant and user: the tab's own and the browser's last
function who(): string {
  const account = session.get().account
  return account ? `${account.tenantId}.${account.user.id}` : 'local'
}
const tabKey = () => `workspace.current.${who()}`
const lastKey = () => `workspace.last.${who()}`

function read(storage: () => Storage, key: string): string | null {
  try {
    return storage().getItem(key)
  } catch {
    return null
  }
}

function write(storage: () => Storage, key: string, value: string) {
  try {
    storage().setItem(key, value)
  } catch {
    // storage unavailable: the tab shows Default after a reload
  }
}

let state: WorkspaceState = { workspaces: [], loaded: false }
const listeners = new Set<() => void>()
let unsubscribe: (() => void) | undefined
let subscribedAs = ''
// A workspace this tab switches to once the list holds it: one just made,
// whose create was acknowledged before the list's change arrived
let pending: { id: string; until: number } | undefined
// Asked before this tab switches (forge 16b): false keeps the current
// workspace, e.g. while the workbench asks about an editor with unsaved
// changes, and switches later with force
let switchGuard: ((to: string) => boolean) | undefined

function set(workspaces: Workspace[]) {
  if (pending && !workspaces.some((w) => w.id === pending!.id) && Date.now() < pending.until) {
    state = { ...state, workspaces, loaded: true } // the current one stays until it arrives
    listeners.forEach((l) => l())
    return
  }
  if (pending && workspaces.some((w) => w.id === pending!.id)) {
    write(() => sessionStorage, tabKey(), pending.id)
    write(() => localStorage, lastKey(), pending.id)
  }
  pending = undefined
  const id = chooseCurrent(workspaces, read(() => sessionStorage, tabKey()), read(() => localStorage, lastKey()))
  const current = workspaces.find((w) => w.id === id)
  if (current && current.id !== read(() => sessionStorage, tabKey())) {
    write(() => sessionStorage, tabKey(), current.id) // a fallback becomes the tab's own
  }
  state = { workspaces, loaded: true, current }
  workbenchContext.setWorkspace(current?.id)
  listeners.forEach((l) => l())
}

// start subscribes once per signed-in person; a tenant switch reloads the
// page, so a change of person here is only sign-in after single-user mode
function start() {
  const as = who()
  if (unsubscribe && subscribedAs === as) return
  unsubscribe?.()
  subscribedAs = as
  state = { workspaces: [], loaded: false }
  unsubscribe = live.subscribe('workspace.list', {}, {
    onSnapshot: (data) => set((data as { workspaces?: Workspace[] })?.workspaces ?? []),
    onChange: (delta) => set((applyDelta({ workspaces: state.workspaces }, delta) as { workspaces?: Workspace[] })?.workspaces ?? []),
    onError: (err) => wsLog.fromError(err).warn('cannot follow the workspaces'),
  })
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export const workspaces = {
  get: () => state,
  subscribe,
  start,

  // Asks guard before every switch but a forced one; returns the remover
  setSwitchGuard(guard: (to: string) => boolean) {
    switchGuard = guard
    return () => {
      if (switchGuard === guard) switchGuard = undefined
    }
  },

  // Shows another workspace in this tab, and remembers it as this
  // browser's last one. A workspace not in the list yet (one just made)
  // is shown when it arrives, if within ten seconds. False when it is not
  // shown: unknown, or held back by the switch guard (which then asks the
  // person and switches with force).
  switchTo(id: string, justMade = false, force = false) {
    if (id === state.current?.id) return true
    if (!force && switchGuard && !switchGuard(id)) return false
    if (!state.workspaces.some((w) => w.id === id)) {
      if (!justMade) return false
      pending = { id, until: Date.now() + 10_000 }
      return true
    }
    pending = undefined
    write(() => sessionStorage, tabKey(), id)
    write(() => localStorage, lastKey(), id)
    set(state.workspaces)
    return true
  },

  // Makes a workspace (a copy of from's layout and context when given) and
  // resolves with its id; the id is made here, so a resend makes one
  async create(name: string, from?: string): Promise<string> {
    const id = `w${newRecordId()}`
    await live.command('workspace.create', id, from ? { name: name.trim(), from } : { name: name.trim() })
    return id
  },

  // Makes a workspace and shows it in this tab
  async createAndSwitch(name: string, from?: string): Promise<string> {
    const id = await workspaces.create(name, from)
    workspaces.switchTo(id, true)
    return id
  },

  // Duplicates a workspace and shows the copy in this tab
  async duplicate(id: string): Promise<string> {
    const from = state.workspaces.find((w) => w.id === id)
    return workspaces.createAndSwitch(copyName(from?.name ?? 'Workspace', state.workspaces), id)
  },

  async rename(id: string, name: string) {
    await live.command('workspace.rename', id, { name: name.trim() })
  },

  async move(id: string, position: number) {
    await live.command('workspace.move', id, { position })
  },

  // Deletes a workspace; the one in use switches to Default first
  async remove(id: string) {
    if (state.current?.id === id) workspaces.switchTo(defaultWorkspaceId, false, true) // the delete was confirmed
    await live.command('workspace.delete', id, {})
  },
}

// The person's workspaces and the one this tab shows, kept current
export function useWorkspace(): WorkspaceState {
  useEffect(() => workspaces.start(), [])
  return useSyncExternalStore(subscribe, workspaces.get)
}
