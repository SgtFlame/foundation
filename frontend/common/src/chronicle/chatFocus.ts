// Which conversation the Chronicles view's checkboxes act on: the focused
// chat publishes its current conversation's checked entries, the entries
// chosen for its last round, and the entries its last memory update
// changed; the view reads them and checks entries through setChecked.

import { useSyncExternalStore } from 'react'

export interface ChatFocus {
  owner: string // the chat's tab id
  key: string // the conversation's key: its id, or a local key until it is stored
  title?: string // the conversation's title, when it has one
  checked: readonly string[] // entry ids the user checked for the conversation
  chosen: readonly string[] // entry ids chosen for its last round
  changed: readonly string[] // entry ids its last memory update updated or proposed
  setChecked: (ids: string[]) => void
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
