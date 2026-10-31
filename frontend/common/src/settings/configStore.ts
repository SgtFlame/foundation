// Caches resolved settings sections for the browser and refetches one when
// the server says it changed (documentation/Configuration.md). Every
// read goes through useConfig or useSetting.

import { useEffect, useSyncExternalStore } from 'react'
import { log } from '../logging/index.ts'
import { Config, type Scope, type SectionInfo, type View } from '../services/Config.ts'
import { extensions } from '../workbench/extensions/index.ts'
import { configuration } from './points.ts'
import { getAt } from './schema.ts'

export interface ConfigEntry {
  view?: View
  error?: string
  loading: boolean
}

const configLog = log.In('config')
const empty: ConfigEntry = { loading: true }

class ConfigStore {
  private readonly entries = new Map<string, ConfigEntry>()
  private readonly listeners = new Set<() => void>()
  private stopEvents?: () => void

  subscribe = (listener: () => void) => {
    this.listeners.add(listener)
    this.watch()
    return () => {
      this.listeners.delete(listener)
    }
  }

  entry(section: string, scope: Scope): ConfigEntry {
    return this.entries.get(key(section, scope)) ?? empty
  }

  // Loads a section unless it is cached or loading
  ensure(section: string, scope: Scope) {
    if (!this.entries.has(key(section, scope))) void this.refresh(section, scope)
  }

  async refresh(section: string, scope: Scope) {
    const k = key(section, scope)
    this.set(k, { ...(this.entries.get(k) ?? {}), loading: true })
    try {
      const view = await Config.get({ section, scope })
      this.set(k, { view, loading: false })
    } catch (err) {
      configLog.fromError(err).With('section', section).warn('cannot load settings %s', section)
      this.set(k, { error: err instanceof Error ? err.message : String(err), loading: false })
    }
  }

  // Stores a view the server returned from set or reset
  put(view: View) {
    if (view.section) this.set(key(view.section, view.scope ?? 'app'), { view, loading: false })
  }

  private set(k: string, entry: ConfigEntry) {
    this.entries.set(k, entry)
    for (const listener of this.listeners) listener()
  }

  // One event stream for the page, opened by the first subscriber
  private watch() {
    if (this.stopEvents) return
    this.stopEvents = Config.events((e) => {
      for (const k of this.entries.keys()) {
        const [section, scope] = k.split('|') as [string, Scope]
        if (section === e.section) void this.refresh(section, scope)
      }
    })
  }
}

function key(section: string, scope: Scope) {
  return `${section}|${scope}`
}

export const configStore = new ConfigStore()

// A section resolved as seen from scope: { view: { value, sources, revision }, error, loading }.
// The default is the user scope: what the server resolves for the user, with
// their tenant's and their own overrides on the app's values
export function useConfig(section: string, scope: Scope = 'user'): ConfigEntry {
  const entry = useSyncExternalStore(configStore.subscribe, () => configStore.entry(section, scope))
  useEffect(() => configStore.ensure(section, scope), [section, scope])
  return entry
}

// One resolved setting, e.g. useSetting<Level[]>('ai.chat.levels', 'levels')
export function useSetting<T>(section: string, path: string, scope: Scope = 'user'): T | undefined {
  return getAt(useConfig(section, scope).view?.value, path) as T | undefined
}

// Adds the server's sections to the configuration point, keeping a custom
// Editor a frontend contribution already set for the same id
export async function loadServerSections(): Promise<void> {
  let sections: SectionInfo[]
  try {
    sections = (await Config.sections()).sections ?? []
  } catch (err) {
    configLog.fromError(err).warn('cannot load the server settings sections')
    return
  }
  const existing = extensions.get(configuration)
  for (const s of sections) {
    const own = existing.find((c) => c.id === s.id)
    extensions.contribute(configuration, { ...s, server: true, Editor: own?.Editor })
  }
}
