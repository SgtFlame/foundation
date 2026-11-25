// What the focused chat shows the Context view (forge 16c; once the
// Chronicles view): the entries chosen for its last round and those its
// last memory update changed, marked with a dot and ✎. Which entries are
// checked is the workspace's, not the conversation's.

import { useSyncExternalStore } from 'react'

export interface ChatFocus {
  owner: string // the chat's tab id
  key: string // the conversation's key: its id, or a local key until it is stored
  title?: string // the conversation's title, when it has one
  chosen: readonly string[] // entry ids chosen for its last round
  changed: readonly string[] // entry ids its last memory update updated or proposed
}

let focus: ChatFocus | undefined
const listeners = new Set<() => void>()

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

// Publishes the focused chat; undefined when no chat is open
export function setChatFocus(next: ChatFocus | undefined) {
  focus = next
  listeners.forEach((l) => l())
}

// Clears the focus if it is still owner's, when owner's chat closes
export function clearChatFocus(owner: string) {
  if (focus?.owner === owner) setChatFocus(undefined)
}

export function useChatFocus(): ChatFocus | undefined {
  return useSyncExternalStore(subscribe, () => focus)
}

// The Context side view's id, for openSideView
export const contextViewId = 'context'
