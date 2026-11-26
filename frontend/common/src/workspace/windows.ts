// The other windows showing this window's workspace (forge 16b question 9).
// Each window is present in ws_<userId>.<workspaceId> (presence the server
// keeps private to that person), so a window opening a workspace another
// window already shows asks the person what this one does:
//   - Live follow: it saves its changes and takes the others' as they come,
//     so both windows show the same layout;
//   - Don't save this window: it neither saves nor follows; the other
//     window keeps the workspace.
// A window alone on its workspace saves and follows. The choice lasts for
// the tab (sessionStorage), per workspace.
import { useSyncExternalStore } from 'react'
import { live } from '../live/index.ts'
import type { PresentUser } from '../live/usePresence.ts'
import { session } from '../session/session.ts'
import { log } from '../logging/Logger.ts'

const winLog = log.In('workspace').Tags('windows')

// checking: waiting for who else is there; asking: the person is asked;
// saving: saves and follows (alone, or Live follow); unsaved: Don't save
export type WindowMode = 'checking' | 'asking' | 'saving' | 'unsaved'

export interface WindowsState {
  workspace?: string
  mode: WindowMode
  // how many other windows show this workspace
  others: number
  // the workspace was saved elsewhere while this window did not save
  changedElsewhere: boolean
}

let state: WindowsState = { mode: 'checking', others: 0, changedElsewhere: false }
const listeners = new Set<() => void>()
let leave: (() => void) | undefined

function set(next: Partial<WindowsState>) {
  state = { ...state, ...next }
  listeners.forEach((l) => l())
}

function who(): string | undefined {
  const account = session.get().account
  return account ? `${account.tenantId}.${account.user.id}` : undefined
}
const choiceKey = (ws: string) => `workspace.window.${who() ?? 'local'}.${ws}`

function storedChoice(ws: string): 'follow' | 'unsaved' | undefined {
  try {
    const v = sessionStorage.getItem(choiceKey(ws))
    return v === 'follow' || v === 'unsaved' ? v : undefined
  } catch {
    return undefined
  }
}

function storeChoice(ws: string, choice: 'follow' | 'unsaved' | undefined) {
  try {
    if (choice) sessionStorage.setItem(choiceKey(ws), choice)
    else sessionStorage.removeItem(choiceKey(ws))
  } catch {
    // the choice is asked again after a reload
  }
}

// The presence object of one of the signed-in person's workspaces
export function windowsObject(ws: string): string | undefined {
  const account = session.get().account
  return account ? `ws_${account.user.id}.${ws}` : undefined
}

export const workspaceWindows = {
  get: () => state,
  subscribe(listener: () => void) {
    listeners.add(listener)
    return () => listeners.delete(listener)
  },

  // This window now shows ws: it joins ws's presence and, from the first
  // list of who is there, saves (alone), keeps its choice, or asks
  enter(ws: string) {
    if (state.workspace === ws && leave) return
    leave?.()
    leave = undefined
    set({ workspace: ws, mode: 'checking', others: 0, changedElsewhere: false })
    const object = windowsObject(ws)
    if (!object) {
      set({ mode: 'saving' })
      return
    }
    const held = live.holdPresence(object, {})
    let decided = false
    const seen = (list: PresentUser[]) => {
      if (state.workspace !== ws) return
      const me = live.getHello()?.connection
      const others = list.filter((u) => u.connection !== me && !u.actor).length
      if (decided) {
        set({ others })
        return
      }
      decided = true
      const choice = storedChoice(ws)
      if (others === 0) {
        storeChoice(ws, undefined) // alone: a choice made beside another window lapses
        set({ others, mode: 'saving' })
      } else if (choice) set({ others, mode: choice === 'follow' ? 'saving' : 'unsaved' })
      else set({ others, mode: 'asking' })
    }
    const unsubscribe = live.subscribe('live.presence', { object }, {
      onSnapshot: (data) => seen((data as PresentUser[]) ?? []),
      onChange: (delta) => seen(((delta as { value?: PresentUser[] }).value) ?? []),
      onError: (err) => {
        winLog.fromError(err).warn('cannot follow the windows on this workspace')
        if (!decided && state.workspace === ws) {
          decided = true
          set({ mode: 'saving' })
        }
      },
    })
    leave = () => {
      unsubscribe()
      held.release()
    }
  },

  // The person's choice for this window: follow (save and take the
  // others' changes) or unsaved (neither)
  choose(choice: 'follow' | 'unsaved') {
    if (!state.workspace) return
    storeChoice(state.workspace, choice)
    set({ mode: choice === 'follow' ? 'saving' : 'unsaved', changedElsewhere: false })
  },

  // The workspace was saved by another window while this one did not save
  noteChangedElsewhere() {
    if (state.mode === 'unsaved' && !state.changedElsewhere) set({ changedElsewhere: true })
  },
}

export function useWorkspaceWindows(): WindowsState {
  return useSyncExternalStore(workspaceWindows.subscribe, workspaceWindows.get)
}
