// The chat modes (forge 06i): the chat.modes settings with what each
// mode's graph says of itself, from GET /conversation/modes, fetched again
// when the settings change or a mode someone made changes (the live
// chat.modes.made query, forge 15h); and the mode a new conversation
// starts in, from the editor the user was in.

import { useEffect, useState } from 'react'
import { useQuery } from '../../live/useQuery.ts'
import { log } from '../../logging/index.ts'
import { Conversations, type ChatMode } from '../../services/Conversation.ts'
import { useSetting } from '../../settings/configStore.ts'
import type { WorkbenchState } from '../../workbench/context/WorkbenchContextStore.ts'

export function useModes(): readonly ChatMode[] {
  const configured = useSetting<unknown>('chat.modes', 'modes', 'user')
  const made = useQuery<unknown>('chat.modes.made')
  const key = JSON.stringify([configured ?? null, made.data ?? null])
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

// Whether a mode is offered beside an editor: its focus's kinds and
// subjects, and its offerIn patterns (forge 18f; '*' is every tab, and none)
export function inFocus(mode: ChatMode, editor: { kind?: string; subject?: string }): boolean {
  const { focus } = mode
  if (!focus) return true
  const { kind } = editor
  return chooses(mode, editor) || !!focus.offerIn?.some((k) => k === '*' || (!!kind && kindMatches(k, kind)))
}

// Whether a mode's own focus holds an editor, leaving out offerIn: where it
// may be chosen for a new conversation
export function chooses(mode: ChatMode, editor: { kind?: string; subject?: string }): boolean {
  const { focus } = mode
  if (!focus) return true
  const { kind } = editor
  return (!!kind && !!focus.kinds?.some((k) => kindMatches(k, kind))) || (!!editor.subject && !!focus.subjects?.includes(editor.subject))
}

// A focus's kind matches an editor kind; one ending in * matches every
// kind it begins (app:* is any installed app's tab, forge 7p-6)
export function kindMatches(pattern: string, kind: string): boolean {
  return pattern.endsWith('*') ? kind.startsWith(pattern.slice(0, -1)) : pattern === kind
}

// The modes offered where the user is: those without a focus, and those
// whose focus the editor matches
export function offeredModes(modes: readonly ChatMode[], editor: { kind?: string; subject?: string }): ChatMode[] {
  return modes.filter((m) => inFocus(m, editor))
}

// A new conversation's mode: the first whose focus the editor matches and
// chooses (not offerOnly), else the default
export function startingMode(modes: readonly ChatMode[], editor: { kind?: string; subject?: string }): ChatMode | undefined {
  return modes.find((m) => m.focus && !m.focus.offerOnly && chooses(m, editor)) ?? modes.find((m) => m.default) ?? modes[0]
}
