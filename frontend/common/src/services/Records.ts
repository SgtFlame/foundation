// Client for the backend record REST handlers
// (backend/features/records/rest/recordhandlers): reading records once
// (find: filtered, sorted pages from the query planner; aggregate: counts
// and sums) and importing rows. Records are changed by the record.* commands
// and read live by the records.get, records.list, records.find and
// records.aggregate queries on the live connection; use those in editors.

import { http, type ResponseStatus } from './http/client.ts'
import type { Problem } from '../live/LiveConnection.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/records/rest/recorddto.

// A record: values by field id, and its own data under keys starting
// with '_'. uint64 ids are strings.
export interface RecordData {
  _id: string
  _entity: string
  _seq: number // the last event's sequence: record.update's expect
  _createdAt: string
  _updatedAt: string
  _createdBy: string
  _updatedBy: string
  _problems?: Record<string, string> // field id → why a type change cleared its value
  [field: string]: unknown
}

export interface GetRecordRequest {
  entity: string
  id: string
}

export interface GetRecordResponse extends ResponseStatus {
  record: RecordData | null
}

// A condition on a field, or an and/or of filters. op is eq when left
// out; field may also be _id, _createdAt or _updatedAt.
export interface RecordFilter {
  and?: RecordFilter[]
  or?: RecordFilter[]
  field?: string
  op?: 'eq' | 'ne' | 'lt' | 'lte' | 'gt' | 'gte' | 'in' | 'prefix' | 'contains'
  value?: unknown
}

// Ties are broken by record id, in the last sort field's direction
export interface RecordSort {
  field: string
  desc?: boolean
}

export interface ListRecordsRequest {
  entity: string
  where?: Record<string, unknown> // field id → value the records must have
  filter?: RecordFilter
}

export interface ListRecordsResponse extends ResponseStatus {
  records: RecordData[] // oldest first
}

export interface FindRecordsRequest {
  entity: string
  where?: Record<string, unknown>
  filter?: RecordFilter
  sort?: RecordSort[]
  limit?: number // every record when left out
  cursor?: string // a previous response's next
  fields?: string[] // every field when left out
  // Relation fields to follow one level: each record gets the related
  // records (for user, {_id, username, displayName}) under _expanded.<field>
  expand?: string[]
}

// How a query was answered
export interface QueryPlan {
  index?: string // the index used ('+' joins several, for an or)
  ordered?: boolean // the index gave the order, so only the page was read
  scan?: boolean // every record of the type was read
  read: number
  complete: boolean // false when a scan stopped at the scan limit
}

export interface FindRecordsResponse extends ResponseStatus {
  records: RecordData[]
  next?: string
  plan: QueryPlan
}

// The live records.find query's result, a FindRecordsRequest's page kept
// current: useQuery<RecordPage>('records.find', request). Changes insert,
// update, move and remove rows of records, and next follows the last row
export interface RecordPage {
  records: RecordData[]
  next?: string
  plan: QueryPlan
}

// An aggregation over the records where and filter keep, grouped by a
// field (one group, key null, when left out). count counts records (with a
// field, those with a value); sum and avg take a number, decimal or money
// field with a currency; min and max a field with an order.
export interface AggregateRecordsRequest {
  entity: string
  where?: Record<string, unknown>
  filter?: RecordFilter
  groupBy?: string
  aggregates: { op: 'count' | 'sum' | 'avg' | 'min' | 'max'; field?: string; as?: string }[]
}

// One group: its key (the field's value, null for none) and each
// aggregate by its as (op_field, or count when left out)
export interface RecordGroup {
  key: unknown
  values: Record<string, unknown>
}

export interface AggregateRecordsResponse extends ResponseStatus {
  groups: RecordGroup[] // ordered by key
  plan: QueryPlan
}

// The live records.aggregate query's result:
// useQuery<RecordGroups>('records.aggregate', request). It runs again,
// debounced, when the records change; groups change by insert, update,
// move and remove, keyed by the key's JSON
export interface RecordGroups {
  groups: RecordGroup[]
  plan: QueryPlan
}

export interface ImportRecordsRequest {
  entity: string
  rows: Record<string, unknown>[]
}

export interface ImportRecordsResponse extends ResponseStatus {
  created: number
  ids: string[] // in row order; '' for a row that failed
  failed: { row: number; problems: Problem[] }[]
}

const tsidEpoch = BigInt(Date.UTC(2026, 0, 1))
let lastTsid = 0n

// A new record id, a TSID like the server's (features.NewTSID): sent as
// record.create's target, so a resent command is a duplicate rather than
// a second record.
export function newRecordId(): string {
  const random = BigInt(Math.floor(Math.random() * (1 << 22)))
  let id = ((BigInt(Date.now()) - tsidEpoch) << 22n) | random
  if (id <= lastTsid) id = lastTsid + 1n
  lastTsid = id
  return id.toString()
}

// Record ids are TSIDs: a longer one is later
export function compareRecordIds(a: string, b: string): number {
  return a.length - b.length || (a < b ? -1 : a > b ? 1 : 0)
}

export const Records = {
  async get(request: GetRecordRequest): Promise<GetRecordResponse> {
    const response = await http.post<GetRecordResponse>('/records/records/get', request)
    return response.data
  },

  async list(request: ListRecordsRequest): Promise<ListRecordsResponse> {
    const response = await http.post<ListRecordsResponse>('/records/records/list', request)
    return response.data
  },

  async find(request: FindRecordsRequest): Promise<FindRecordsResponse> {
    const response = await http.post<FindRecordsResponse>('/records/records/find', request)
    return response.data
  },

  async aggregate(request: AggregateRecordsRequest): Promise<AggregateRecordsResponse> {
    const response = await http.post<AggregateRecordsResponse>('/records/records/aggregate', request)
    return response.data
  },

  async import(request: ImportRecordsRequest): Promise<ImportRecordsResponse> {
    const response = await http.post<ImportRecordsResponse>('/records/records/import', request)
    return response.data
  },
}
