// Client for the backend prefab REST handlers
// (backend/features/ui/rest/prefabhandlers): the published versions of
// prefabs, as the tenant database keeps them. Drafts are edited by the
// prefab.* commands and read by the prefab.describe and prefab.list
// queries on the live connection.

import { http, type ResponseStatus } from './http/client.ts'
import type { UiNode } from '../ui/types.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/ui/rest/prefabdto. A prefab travels
// as its own JSON document (ui.Prefab). uint64 IDs are strings, since a
// JavaScript number cannot hold every uint64.

// A prop, state value or payload field's type
export type PrefabType =
  | 'string'
  | 'number'
  | 'integer'
  | 'bool'
  | 'date'
  | 'dateTime'
  | 'enum'
  | 'json'
  | 'record'
  | 'records'

export interface PropDecl {
  type: PrefabType
  entity?: string // record, records
  options?: string[] // enum
  description?: string
  required?: boolean
  default?: unknown
}

export interface StateDecl {
  type: PrefabType
  entity?: string
  options?: string[]
  description?: string
  default?: unknown
}

export interface EventDecl {
  description?: string
  payload?: Record<string, PrefabType>
}

export interface SlotDecl {
  description?: string
}

export interface QueryDecl {
  query: string // a registered query or data.query graph
  params?: Record<string, unknown>
  bind?: Record<string, string> // param → props.<prop> or state.<value>
  description?: string
}

// A named preset of a prefab's props, which an instance picks with its
// node's variant
export interface VariantDecl {
  description?: string
  props: Record<string, unknown>
}

export interface Prefab {
  id: string
  name: string
  description?: string
  owner?: string // app:<appId>; none is the tenant's library
  props: Record<string, PropDecl>
  state: Record<string, StateDecl>
  events: Record<string, EventDecl>
  slots: Record<string, SlotDecl>
  queries: Record<string, QueryDecl>
  variants?: Record<string, VariantDecl>
  template?: UiNode // nodes found by key paths, such as root/row/title
  graphs: Record<string, string> // a name the template's handlers use → a graph id
  preview: { props?: Record<string, unknown> }
  version: number
}

// The prefab.describe query's result
export interface PrefabView {
  prefab: Prefab
  rev: number // 0 for a published version
  published: number
  publishedRev: number
  changed: boolean
  updatedBy?: string
  updatedAt?: string
  deleted?: boolean // hidden until prefab.restore (forge 06b)
}

// One prefab in the prefab.list query, keyed by prefab.<id>
export interface PrefabSummary {
  id: string
  name: string
  description?: string
  owner?: string
  rev: number
  published: number
  changed: boolean
  deleted?: boolean // hidden until prefab.restore: list it apart
}

export interface VersionInfo {
  prefabId: string
  version: number
  name: string
  owner?: string
  publishedBy: string
  publishedAt: string
}

export interface ListPrefabsResponse extends ResponseStatus {
  prefabs: VersionInfo[]
}

export interface GetPrefabRequest {
  id: string
  version?: number // the last when left out
}

export interface GetPrefabResponse extends ResponseStatus {
  info?: VersionInfo
  prefab?: Prefab
}

export interface ListVersionsResponse extends ResponseStatus {
  versions: VersionInfo[] // newest first
}

export interface PreviewRequest {
  prefab: string
  props?: Record<string, unknown> // its preview data when left out
  variant?: string // one of its variants, under props
  draft?: boolean // its draft, not its last published version
}

export interface PreviewProblem {
  path: string[] // the node's key path from the root
  message: string
  fix?: string
}

export interface PreviewResponse extends ResponseStatus {
  tree?: UiNode // primitives only
  problems: PreviewProblem[]
}

export const Prefabs = {
  async listPrefabs(): Promise<ListPrefabsResponse> {
    const response = await http.get<ListPrefabsResponse>('/ui/prefabs')
    return response.data
  },

  async getPrefab(request: GetPrefabRequest): Promise<GetPrefabResponse> {
    const response = await http.post<GetPrefabResponse>('/ui/prefabs/get', request)
    return response.data
  },

  async listVersions(id: string): Promise<ListVersionsResponse> {
    const response = await http.post<ListVersionsResponse>('/ui/prefabs/versions', { id })
    return response.data
  },

  // Evaluates a prefab once, as prefab.preview does
  async preview(request: PreviewRequest): Promise<PreviewResponse> {
    const response = await http.post<PreviewResponse>('/ui/prefabs/preview', request)
    return response.data
  },
}
