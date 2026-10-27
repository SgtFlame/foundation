// Extension points owned by Settings (documentation/Configuration.md)

import type { ComponentType } from 'react'
import { defineExtensionPoint, type Contribution } from '../workbench/extensions/index.ts'

export type SettingScope = 'app' | 'tenant' | 'user'

export interface SectionEditorProps {
  value: unknown
  onChange: (value: unknown) => void
  schema: unknown
  errors: Record<string, string> // by path
}

// A settings section; its id is dotted and owner-prefixed, e.g. 'ui.appearance'
export interface ConfigurationContribution extends Contribution {
  title: string
  description?: string
  category: string[] // the Settings tree path, e.g. ['AI', 'Chat']
  scopes: SettingScope[]
  schema: unknown // the JSON-Schema subset of Configuration.md
  defaults: unknown
  version: number
  migrate?: (old: unknown, fromVersion: number) => unknown
  Editor?: ComponentType<SectionEditorProps> // in place of the generic form
  server?: boolean // declared by the backend, which stores and validates it
}

export const configuration = defineExtensionPoint<ConfigurationContribution>(
  'configuration',
  'Settings sections, edited in the Settings tab',
)
