// Client for the backend entity type REST handlers
// (backend/features/records/rest/entityhandlers): the published versions
// of entity types, as the tenant database keeps them. Drafts are edited by
// the entity.* commands and read by the entity.describe and entity.list
// queries on the live connection.

import { http, type ResponseStatus } from './http/client.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/records/rest/entitydto. An entity
// type travels as its own JSON document (records.EntityType). uint64 IDs
// are strings, since a JavaScript number cannot hold every uint64.

export interface FieldOption {
  id: string
  name: string
  color?: string
}

export interface EntityField {
  id: string // records keep values by field id, so a rename never touches them
  name: string
  type: string // a field type: text, bool, date, enum, relation, … (entity.listFieldTypes)
  description?: string
  required?: boolean
  default?: unknown
  hidden?: boolean // removed: values are kept until a purge
  options?: FieldOption[] // enum, multiEnum
  relation?: { entity: string; many?: boolean; inverse?: string } // relation; entity 'user' is the tenant's users
  scale?: number // decimal, money
  currency?: string // money
  items?: EntityField // list
  fields?: EntityField[] // object
  schema?: Record<string, unknown> // json
  formula?: string
  rollup?: { relation: string; field?: string; aggregate: string }
}

export interface EntityIndex {
  id: string
  fields: string[]
  unique?: boolean
}

export interface EntityAccess {
  read?: string
  create?: string
  update?: string
  delete?: string
}

export interface EntityType {
  id: string
  name: string
  description?: string
  owner?: string // app:<appId>, or tenant
  titleField?: string
  fields: EntityField[]
  indexes: EntityIndex[]
  access: EntityAccess
  version: number
}

export interface VersionInfo {
  entityId: string
  version: number
  name: string
  owner?: string
  publishedBy: string
  publishedAt: string
}

export interface ListEntityTypesResponse extends ResponseStatus {
  entityTypes: VersionInfo[]
}

export interface GetEntityTypeRequest {
  id: string
  version?: number // the last when left out
}

export interface GetEntityTypeResponse extends ResponseStatus {
  info?: VersionInfo
  entityType?: EntityType
}

export interface ListVersionsResponse extends ResponseStatus {
  versions: VersionInfo[] // newest first
}

export const Entities = {
  async listEntityTypes(): Promise<ListEntityTypesResponse> {
    const response = await http.get<ListEntityTypesResponse>('/records/entities')
    return response.data
  },

  async getEntityType(request: GetEntityTypeRequest): Promise<GetEntityTypeResponse> {
    const response = await http.post<GetEntityTypeResponse>('/records/entities/get', request)
    return response.data
  },

  async listVersions(id: string): Promise<ListVersionsResponse> {
    const response = await http.post<ListVersionsResponse>('/records/entities/versions', { id })
    return response.data
  },
}
