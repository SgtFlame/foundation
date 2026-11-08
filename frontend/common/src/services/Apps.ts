// Types of application manifests (backend/features/apps) and the client of
// appservice (/apps): exporting a published app as one document, checking
// one and importing one. An app's draft is edited by the app.* commands
// and read by the app.describe and app.list queries on the live
// connection, e.g. useQuery<AppView>('app.describe', { id }). uint64 IDs
// are strings, since a JavaScript number cannot hold every uint64.

import { http, type ResponseStatus } from './http/client.ts'
import type { Problem } from '../live/LiveConnection.ts'
import type { Graph } from './Cognition.ts'
import type { EntityType } from './Entities.ts'
import type { Prefab } from './Prefabs.ts'

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
  actions: string[] // entity:<id>.<read|create|update|delete|*> on the app's own entity types
  rowRules?: Record<string, string> // entity type → rule
  installer?: boolean // whoever installs the app gets it; else the first role
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
  published: number // the last published version, 0 before the first
  publishedRev: number // the rev it was published from
  changed: boolean // the draft has changes the next version will take
  createdBy?: string
  updatedBy?: string
  updatedAt?: string
  deleted?: boolean // hidden with its own parts until app.restore (forge 06b)
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
  deleted?: boolean // hidden until app.restore: list it apart
}

// useQuery<AppVersions>('app.versions', { id }): the app's published versions
export interface AppVersions {
  published: number
  publishedRev: number
  rev: number
  versions: { version: number; rev: number; publishedBy: string; publishedAt: string }[]
}

// One command or query of an installed app, by its name on the live core
// (<appId>.<name>)
export interface InstalledEntry {
  name: string
  title?: string
  description?: string
  args?: unknown // a command's args or a query's params, JSON Schema
  action?: string
}

// One app in the app.installs query, keyed "app.<id>"
export interface InstalledApp {
  app: string
  name: string
  icon?: string
  description?: string
  status: 'installed' | 'uninstalled'
  version: number
  commands: InstalledEntry[]
  queries: InstalledEntry[]
  editorKinds: AppEditorKind[]
  triggers: AppTrigger[]
  installedBy: string
  installedAt: string
  updatedAt: string
}

// An exported app (apps.Document, format forge.app/1): its manifest, its
// own parts, the tenant's parts it uses, the parts of apps it requires,
// the primitives they use and, when asked for, sample records.
export interface AppDocument {
  format: string
  app: string
  version: number
  publishedAt: string
  exportedAt: string
  manifest: AppManifest
  parts: AppDocumentParts
  tenantParts: AppDocumentParts
  appParts: AppDocumentParts
  primitives: { components: string[]; nodes: string[]; graphs: string[] }
  seed?: Record<string, { id: string; values: Record<string, unknown> }[]>
}

export interface AppDocumentParts {
  prefabs: { id: string; version: number; publishedAt?: string; prefab: Prefab }[]
  graphs: { id: string; version: number; publishedAt?: string; graph: Graph }[]
  entities: { id: string; version: number; publishedAt?: string; entity: EntityType }[]
}

export interface ExportAppRequest {
  id: string
  version?: number // the last published when left out
  seed?: boolean // include the records of the app's own entity types
}

export interface ExportAppResponse extends ResponseStatus {
  document?: AppDocument
  problems: Problem[]
}

export interface CheckAppResponse extends ResponseStatus {
  problems: Problem[]
}

export interface ImportAppRequest {
  document: AppDocument
  id?: string // the app's id here, when not the document's
  overwrite?: string[] // <type>:<id> of parts to overwrite, such as prefab:recordTable
  keep?: string[] // <type>:<id> of parts to keep as this tenant has them
  dryRun?: boolean
}

// One side of a part in an import's plan.
export interface PartVersion {
  owner?: string
  version: number
  publishedAt?: string
  updatedAt?: string
  changed?: boolean // the tenant's draft has unpublished changes
}

export interface ImportPartPlan {
  type: 'prefab' | 'graph' | 'entity'
  id: string
  group: 'parts' | 'tenantParts'
  name: string
  status: 'new' | 'same' | 'duplicate'
  decision?: 'overwrite' | 'keep'
  document: PartVersion
  target?: PartVersion
}

export interface ImportAppResponse extends ResponseStatus {
  plan: { app: string; parts: ImportPartPlan[] }
  imported: boolean
  version?: number
  problems: Problem[]
}

// The operation log (forge 06a): useQuery<OpLog>('ops.list', {app?,
// scope?, request?, principal?, after?}), requests newest first, each a
// list change as its ops arrive; scope is an undo scope (an editor's
// Activity tab, forge 06c)
export interface Op {
  command: string
  target: string
  args?: unknown
}

export interface OpEntry extends Op {
  seq: number // the sequence it committed at
  decided: number // its target's sequence when it was decided
  targetType: string
  app?: string
  request: string
  commandId: string
  principal: string
  actor?: string // who sent it for the principal: chat, agent
  inverse: Op[] // the ops that undo it, in order
  irreversible?: string // why none do
  undoes?: string // the request its request undoes (forge 06b)
  time: string
  summary?: string // the op in a few words: Add a page (tasks /mine)
}

export interface OpRequest {
  id: string
  app?: string
  principal: string
  actor?: string
  seq: number
  time: string
  ops: OpEntry[]
  summary?: string // its first op's and how many more
}

export interface OpLog {
  requests: OpRequest[]
}

// Undo and redo (forge 06b). A scope is what an editor's undo reaches:
// app:<id>, app:<id>/manifest, prefab:<id>, graph:<id> or entity:<id>.
// useQuery<UndoStack>('ops.stack', {scope}) is the caller's own requests
// in it, newest first; the ops.undo and ops.redo calls end with one
// result event, an UndoResult
export interface UndoItem {
  request: string
  summary: string // for a redo, what it brings back
  actor?: string
  time: string
  ops: number
}

export interface UndoStack {
  undo: UndoItem[]
  redo: UndoItem[]
}

export interface UndoArgs {
  scope?: string
  request?: string // any request in the log; for a redo, an undo or the request it undid
  force?: boolean // undo the rest, leaving what others changed since
  dryRun?: boolean // say what it would do, without doing it
}

// useQuery<RequestStatus>('ops.status', {request}): what a request changed
// and whether it is undone now (the chat's round-<roundId>)
export interface RequestStatus {
  request: string
  ops: number
  undoable: number // the ops with an inverse
  undone: boolean
  undoneBy?: string // the undo that undid it
}

// One thing an op changes: a prefab node, a manifest item, a field…
export interface UndoItemRef {
  target: string // <targetType>:<id>
  kind?: string // node, page, role, field, info…; none for the whole target
  key?: string
}

export interface UndoConflict {
  item: UndoItemRef
  command: string // the later change
  request: string
  principal: string
  actor?: string
  time: string
}

export interface UndoResult {
  // done; preview (dryRun); conflicts (refused: force undoes the rest);
  // nothing (nothing to undo)
  status: 'done' | 'preview' | 'conflicts' | 'nothing'
  request: string // the request undone
  undoes?: string
  app?: string
  summary: string
  ops: Op[] // the inverses sent, in order
  changes: string[] // each op taken back, in words, newest first
  conflicts: UndoConflict[]
  skipped: { command: string; target: string; why: string }[]
  sent?: string // the undo's own request, when done
  seq?: number
}

// Export, check and import resolve their refusals too (a check that
// fails, a duplicate to decide: 409 and 422 carry problems and the plan),
// and throw ServiceError on any other failure.
const withProblems = { validateStatus: (s: number) => (s >= 200 && s < 300) || s === 409 || s === 422 }

export const Apps = {
  async export(request: ExportAppRequest): Promise<ExportAppResponse> {
    return (await http.post<ExportAppResponse>('/apps/export', request, withProblems)).data
  },
  async check(document: AppDocument): Promise<CheckAppResponse> {
    return (await http.post<CheckAppResponse>('/apps/check', { document }, withProblems)).data
  },
  async import(request: ImportAppRequest): Promise<ImportAppResponse> {
    return (await http.post<ImportAppResponse>('/apps/import', request, withProblems)).data
  },
}
