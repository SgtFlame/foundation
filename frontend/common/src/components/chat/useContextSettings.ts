// The chat.contextSettings controls a surface shows: those the server has a
// chat.contextProvider for, with their defaults from Settings
// (chat.context.defaults.<id>).

import { useEffect, useMemo, useState, useSyncExternalStore } from 'react'
import type { ChatContextSettingsContribution, ChatSurface } from '../../editors/chat/points.ts'
import { chatContextSettings } from '../../editors/chat/points.ts'
import { log } from '../../logging/index.ts'
import { Conversations } from '../../services/Conversation.ts'
import { configStore } from '../../settings/configStore.ts'
import { useContributions } from '../../workbench/extensions/index.ts'

let providers: Promise<Set<string>> | undefined

// The server's provider ids, fetched once per page
function serverProviders(): Promise<Set<string>> {
  providers ??= Conversations.contextProviders()
    .then((r) => new Set((r.providers ?? []).map((p) => p.id)))
    .catch((err: unknown) => {
      log.In('chat').fromError(err).warn('cannot list the chat context providers')
      providers = undefined
      return new Set<string>()
    })
  return providers
}

const defaultsSection = (id: string) => `chat.context.defaults.${id}`

export function useContextSettings(surface: ChatSurface): {
  contributions: readonly ChatContextSettingsContribution[]
  defaults: Record<string, Record<string, unknown>>
} {
  const [ids, setIds] = useState<Set<string>>()
  useEffect(() => {
    void serverProviders().then(setIds)
  }, [])
  const all = useContributions(chatContextSettings, { surface })
  const contributions = useMemo(() => all.filter((c) => ids?.has(c.id)), [all, ids])

  const key = contributions.map((c) => c.id).join(',')
  useEffect(() => {
    for (const id of key ? key.split(',') : []) configStore.ensure(defaultsSection(id), 'user')
  }, [key])
  const values = useSyncExternalStore(configStore.subscribe, () =>
    JSON.stringify(contributions.map((c) => configStore.entry(defaultsSection(c.id), 'user').view?.value ?? {})),
  )
  const defaults = useMemo(() => {
    const parsed = JSON.parse(values) as Record<string, unknown>[]
    return Object.fromEntries(contributions.map((c, i) => [c.id, parsed[i] ?? {}]))
  }, [values, contributions])
  return { contributions, defaults }
}
