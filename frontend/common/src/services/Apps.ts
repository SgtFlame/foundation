// Types of application manifests (backend/features/apps). An app has no
// REST handlers yet: its draft is edited by the app.* commands and read by
// the app.describe and app.list queries on the live connection, e.g.
// useQuery<AppView>('app.describe', { id }). uint64 IDs are strings,
// since a JavaScript number cannot hold every uint64.

export interface AppPage {
  route: string // /mine, or /task/:id: its params become the prefab's props
  prefab: string
  title?: string
}

export interface AppNavItem {
  title: string
  route: string
  icon?: string
}

export interface AppEditorKind {
  kind: string
  page: string // a page's route
  title: string // a template over the page's data, such as {record.title}
  icon?: string
  subject?: { type?: string; label?: string }
}

export interface AppCommand {
  type: string
  graph: string
  action?: string
  title?: string
  description?: string
}

export interface AppQuery {
  name: string
  graph: string
  description?: string
}

export interface AppRole {
  id: string
  title?: string
  description?: string
  actions: string[]
  rowRules?: Record<string, string> // entity type → rule
}

export interface AppSettings {
  schema?: Record<string, unknown>
  defaults?: Record<string, unknown>
}

export interface AppTrigger {
  id: string
  title?: string
  description?: string
  config?: Record<string, unknown>
}

export interface AppRequires {
  primitives?: string[]
  apps?: string[]
}

// An application (apps.Manifest)
export interface AppManifest {
  id: string
  name: string
  icon?: string
  description?: string
  home?: string
  frame?: string // prefab.<id>; empty for the default frame
  pages: AppPage[]
  nav: AppNavItem[]
  editorKinds: AppEditorKind[]
  entities: string[]
  commands: AppCommand[]
  queries: AppQuery[]
  roles: AppRole[]
  settings: AppSettings
  triggers: AppTrigger[]
  requires: AppRequires
  version: number
}

// A prefab, graph or entity type the manifest names or the app owns
export interface AppPart {
  type: 'prefab' | 'graph' | 'entity'
  id: string
  name?: string
  kind?: string // a graph's kind
  owner?: string
  rev?: number
  published?: number
  changed?: boolean
  named: boolean
  owned: boolean
  builtin?: boolean
  missing?: boolean
}

// The app.describe query's result
export interface AppView {
  manifest: AppManifest
  rev: number
  createdBy?: string
  updatedBy?: string
  updatedAt?: string
  parts: AppPart[]
}

// One app in the app.list query, keyed "app.<id>"
export interface AppSummary {
  id: string
  name: string
  icon?: string
  description?: string
  rev: number
  pages: number
  updatedAt?: string
}
