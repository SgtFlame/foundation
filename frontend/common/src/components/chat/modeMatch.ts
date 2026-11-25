// /mode's matcher (forge 15e): a name typed by a person against the chat
// modes, offered where they are or not. Kept free of React and the
// services so `node --test` runs its tests.

import type { ChatMode } from '../../services/Conversation.ts'

export type ModeMatch =
  | { kind: 'found'; mode: ChatMode }
  | { kind: 'notOffered'; mode: ChatMode } // a mode that exists, but not on this tab
  | { kind: 'ambiguous'; modes: ChatMode[] }
  | { kind: 'unknown' }

// A name as it is compared: lower case, without emoji, spaces or punctuation
export function normalizeModeName(name: string): string {
  return name.toLowerCase().replace(/[^\p{L}\p{N}]/gu, '')
}

// The mode named by query: by its id or its label (ignoring case, emoji and
// spaces), else by a unique prefix of either. Only offered modes switch; a
// mode found among the others is notOffered. Exact names win over
// prefixes, and offered modes over the rest.
export function matchMode(query: string, offered: readonly ChatMode[], all: readonly ChatMode[]): ModeMatch {
  const q = normalizeModeName(query)
  if (!q) return { kind: 'unknown' }
  const names = (m: ChatMode) => [normalizeModeName(m.id), normalizeModeName(m.label)]
  const usable = offered.filter((m) => !m.missing)
  const others = all.filter((m) => !offered.some((o) => o.id === m.id))

  const exact = (list: readonly ChatMode[]) => list.filter((m) => names(m).includes(q))
  const prefixed = (list: readonly ChatMode[]) => list.filter((m) => names(m).some((n) => n.startsWith(q)))

  for (const [find, list, kind] of [
    [exact, usable, 'found'],
    [exact, others, 'notOffered'],
    [prefixed, usable, 'found'],
    [prefixed, others, 'notOffered'],
  ] as const) {
    const hits = find(list)
    if (hits.length === 1) return { kind, mode: hits[0] }
    if (hits.length > 1) return { kind: 'ambiguous', modes: hits }
  }
  return { kind: 'unknown' }
}

// A mode as a person sees it: its icon and label
export function modeTitle(mode: Pick<ChatMode, 'icon' | 'label'>): string {
  return `${mode.icon ? `${mode.icon} ` : ''}${mode.label}`
}

// Whether the picker shows: there is a choice, or the conversation's mode is
// not offered here
export function modePickerShown(modes: readonly ChatMode[], value?: ChatMode): boolean {
  return modes.length >= 2 || (!!value && !modes.some((m) => m.id === value.id))
}

// Where a mode is offered, from its focus, in words: "on any app's tab",
// "on Prefab or Blueprint tabs". kindName names an editor kind.
export function whereOffered(mode: ChatMode, kindName: (kind: string) => string | undefined): string {
  const { focus } = mode
  if (!focus) return 'everywhere'
  const places: string[] = []
  for (const kind of focus.kinds ?? []) {
    if (kind === '*') places.push('any')
    else if (kind.endsWith(':*')) places.push(`any ${kind.slice(0, -2)}'s`)
    else if (kind.endsWith('*')) places.push(`any ${kind.slice(0, -1)}`)
    else places.push(kindName(kind) ?? kind)
  }
  // Subjects name the same places as the kinds; they are said only alone
  if (places.length === 0) places.push(...(focus.subjects ?? []).map((s) => `${s}`))
  const unique = [...new Set(places)]
  if (unique.length === 0) return 'nowhere'
  const list = unique.length === 1 ? unique[0] : `${unique.slice(0, -1).join(', ')} or ${unique[unique.length - 1]}`
  return `on ${list} ${unique.length === 1 && unique[0].startsWith('any ') ? 'tab' : 'tabs'}`
}

// The modes as a markdown list, each with the /mode that picks it
export function modeList(modes: readonly ChatMode[]): string {
  return modes.map((m) => `- \`/mode ${m.label.toLowerCase()}\`: ${modeTitle(m)}${m.description ? `, ${m.description}` : ''}`).join('\n')
}

// What /mode says after matching a name, in markdown
export function modeMatchMessage(query: string, match: ModeMatch, offered: readonly ChatMode[], kindName: (kind: string) => string | undefined): string {
  const list = modeList
  switch (match.kind) {
    case 'notOffered':
      return `${modeTitle(match.mode)} is not offered on this tab. It is offered ${whereOffered(match.mode, kindName)}. The modes here are:\n\n${list(offered)}`
    case 'ambiguous':
      return `\`${query}\` could be ${match.modes.map(modeTitle).join(' or ')}. Type more of its name.`
    case 'unknown':
      return `There is no mode \`${query}\`. The modes here are:\n\n${list(offered)}`
    case 'found':
      return `Switched to ${modeTitle(match.mode)}`
  }
}
