// Client for the backend Chronicle REST handlers
// (backend/features/chronicle/rest/chroniclehandlers)

import type { EntityField } from './Entities.ts'
import { http, type ResponseStatus } from './http/client.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/chronicle/rest/chronicledto.
// uint64 IDs are strings, since a JavaScript number cannot hold every uint64.

export type EntryStatus = 'active' | 'proposed'

// A Chronicle entry. list leaves out fullSummary and fullContext.
export interface Entry {
  id: string
  created: string // RFC 3339
  updated: string
  type: string // an EntryType id
  title: string
  shortSummary: string
  fullSummary?: string
  fullContext?: string
  fields: Record<string, unknown> // the type's own fields
  parentId?: string // absent at the top of its type
  status: EntryStatus // proposed: made by the memory update, waiting to be accepted
  review?: boolean // a round that made or changed it was ignored
  indexed: boolean // embedded for vector search as it now reads
  tenantId: string
  ownerId: string // someone else's when it was shared with you: read-only
  shared?: 'tenant' | 'global' // global: the global tenant's copy of an approved share
  sourceId?: string // the entry a global copy was made from
  scope?: string // project:<page> for a project's story bible entry
  facts?: string // its fields and typed links as text, as search reads them
}

export type ShareStatus = 'pending' | 'tenantApproved' | 'approved' | 'rejected' | 'withdrawn'

// A request to share an entry with every tenant, with what was asked to be
// shared: a moderator of the owner's tenant approves it, then a global
// moderator, whose approval makes the global copy
export interface Share {
  id: string
  created: string
  tenantId: string
  ownerId: string
  ownerName?: string
  entryId: string
  status: ShareStatus
  type: string
  title: string
  shortSummary: string
  fullSummary: string
  note?: string
  tenantBy?: string
  tenantAt?: string
  globalBy?: string
  globalAt?: string
  copyId?: string
}

export interface ShareResponse extends ResponseStatus {
  entry?: Entry
  share?: Share
}

export interface SharesResponse extends ResponseStatus {
  shares: Share[]
}

export interface ModerateResponse extends ResponseStatus {
  share?: Share
}

// What the user edits of an entry
export interface EntryInput {
  type: string
  title: string
  shortSummary: string
  fullSummary: string
  fullContext: string
  fields?: Record<string, unknown>
  parentId?: string
  scope?: string // project:<page>, which the project's readers find too
}

// A related link (kind 'related', no direction) or a typed one read from
// fromId to toId, such as 'sister of'
export interface Link {
  id: string
  fromId: string
  toId: string
  kind: string
}

// A round that created or updated an entry
export interface LinkedRound {
  roundId: string
  conversationId: string
  conversationTitle?: string
  time: string
  action: 'created' | 'updated'
  text: string // the round's message, cut short
  previous?: { shortSummary: string; fullSummary: string } // an update's earlier summaries
}

export interface EntryType {
  id: string
  title: string // plural, e.g. 'Entities'
  icon: string // an emoji
  description: string
  fields: unknown // the schema of its fields, in the settings schema language
  // A tenant's or project's type (custom): its fields in the records'
  // field catalog, the types its entries go under, and where it is defined
  recordFields?: EntityField[]
  parentTypes?: string[]
  scope?: string // 'tenant' or 'project:<page>'
  custom?: boolean
}

// A set of types a project copies, such as the fantasy story bible
export interface Starter {
  id: string
  title: string
  description: string
  types: EntryType[]
}

export interface SaveTypeResponse extends ResponseStatus {
  type?: EntryType
  problems?: { path: string; message: string; fix?: string }[]
}

export interface ListResponse extends ResponseStatus {
  entries?: Entry[]
  links?: Link[]
}

export interface GetResponse extends ResponseStatus {
  entry?: Entry
  links?: Link[]
  rounds?: LinkedRound[] // newest first
}

export interface EntryResponse extends ResponseStatus {
  entry?: Entry
}

export interface UpdateRequest extends EntryInput {
  id: string
  accept?: boolean // makes a proposed entry active
  reviewed?: boolean // clears the review flag
}

export interface LinkRequest {
  fromId: string
  toId: string
  remove?: boolean
  kind?: string // 'related' (the default) or a typed link from fromId to toId
}

export interface LinkResponse extends ResponseStatus {
  links?: Link[]
}

export interface TypesResponse extends ResponseStatus {
  types?: EntryType[]
  starters?: Starter[]
}

export interface ReindexResponse extends ResponseStatus {
  indexed: number
}

export const Chronicle = {
  async list(): Promise<ListResponse> {
    const response = await http.post<ListResponse>('/chronicle/list', {})
    return response.data
  },

  async get(id: string): Promise<GetResponse> {
    const response = await http.post<GetResponse>('/chronicle/get', { id })
    return response.data
  },

  async create(request: EntryInput): Promise<EntryResponse> {
    const response = await http.post<EntryResponse>('/chronicle/create', request)
    return response.data
  },

  async update(request: UpdateRequest): Promise<EntryResponse> {
    const response = await http.post<EntryResponse>('/chronicle/update', request)
    return response.data
  },

  // Deletes an entry, or dismisses a proposed one; its children move up to its parent
  async delete(id: string): Promise<ResponseStatus> {
    const response = await http.post<ResponseStatus>('/chronicle/delete', { id })
    return response.data
  },

  async link(request: LinkRequest): Promise<LinkResponse> {
    const response = await http.post<LinkResponse>('/chronicle/link', request)
    return response.data
  },

  async types(): Promise<TypesResponse> {
    const response = await http.get<TypesResponse>('/chronicle/types')
    return response.data
  },

  // Makes or replaces a tenant's or project's type; a 400 carries problems
  async saveType(type: Omit<EntryType, 'fields'>): Promise<SaveTypeResponse> {
    const response = await http.post<SaveTypeResponse>('/chronicle/types/save', { type })
    return response.data
  },

  // Removes a type no entry uses (409 type_in_use)
  async deleteType(id: string, scope: string): Promise<ResponseStatus> {
    const response = await http.post<ResponseStatus>('/chronicle/types/delete', { id, scope })
    return response.data
  },

  // Copies a starter set's types into a scope, keeping those it has
  async copyStarter(starter: string, scope: string): Promise<TypesResponse> {
    const response = await http.post<TypesResponse>('/chronicle/types/copy-starter', { starter, scope })
    return response.data
  },

  async reindex(entryIds?: string[]): Promise<ReindexResponse> {
    const response = await http.post<ReindexResponse>('/chronicle/reindex', { entryIds })
    return response.data
  },

  // Shares one of your entries: 'tenant' at once, 'global' as a request for
  // moderators, '' to stop (withdrawing a request, removing a global copy)
  async share(id: string, target: 'tenant' | 'global' | '', note?: string): Promise<ShareResponse> {
    const response = await http.post<ShareResponse>('/chronicle/share', { id, target, note })
    return response.data
  },

  // Your global share requests, or with queue those waiting for you to moderate
  async shares(queue = false): Promise<SharesResponse> {
    const response = await http.post<SharesResponse>('/chronicle/shares', { queue })
    return response.data
  },

  async moderate(shareId: string, approve: boolean, note?: string): Promise<ModerateResponse> {
    const response = await http.post<ModerateResponse>('/chronicle/moderate', { shareId, approve, note })
    return response.data
  },
}
