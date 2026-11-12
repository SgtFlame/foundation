// The chat modes (forge 06i): the chat.modes settings with what each
// mode's graph says of itself, from GET /conversation/modes, fetched again
// when the settings change; and the mode a new conversation starts in,
// from the editor the user was in.

import { useEffect, useState } from 'react'
import { log } from '../../logging/index.ts'
import { Conversations, type ChatMode } from '../../services/Conversation.ts'
import { useSetting } from '../../settings/configStore.ts'
import type { WorkbenchState } from '../../workbench/context/WorkbenchContextStore.ts'

export function useModes(): readonly ChatMode[] {
  const configured = useSetting<unknown>('chat.modes', 'modes', 'user')
  const key = JSON.stringify(configured ?? null)
  const [modes, setModes] = useState<readonly ChatMode[]>([])
  useEffect(() => {
    let live = true
    Conversations.modes()
      .then((r) => live && setModes(r.modes ?? []))
      .catch((err: unknown) => log.In('chat').fromError(err).warn('cannot list the chat modes'))
    return () => {
      live = false
    }
  }, [key])
  return modes
}

// The editor the user was in before the chat tab: its kind and subject type
export function editorBefore(state: WorkbenchState, chatTab: string): { kind?: string; subject?: string } {
  const tab = state.focusOrder.find((t) => t !== chatTab)
  const editor = state.editors.find((e) => e.tab === tab)
  return { kind: editor?.kind, subject: editor?.subject?.type }
}

export function inFocus(mode: ChatMode, editor: { kind?: string; subject?: string }): boolean {
  const { focus } = mode
  if (!focus) return true
  return (!!editor.kind && !!focus.kinds?.includes(editor.kind)) || (!!editor.subject && !!focus.subjects?.includes(editor.subject))
}

// The modes offered where the user is: those without a focus, and those
// whose focus the editor matches
export function offeredModes(modes: readonly ChatMode[], editor: { kind?: string; subject?: string }): ChatMode[] {
  return modes.filter((m) => inFocus(m, editor))
}

// A new conversation's mode: the first whose focus the editor matches,
// else the default
export function startingMode(modes: readonly ChatMode[], editor: { kind?: string; subject?: string }): ChatMode | undefined {
  return modes.find((m) => m.focus && inFocus(m, editor)) ?? modes.find((m) => m.default) ?? modes[0]
}
