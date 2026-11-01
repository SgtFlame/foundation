// Client for the backend record REST handlers
// (backend/features/records/rest/recordhandlers): reading records once and
// importing rows. Records are changed by the record.* commands and read
// live by the records.get and records.list queries on the live
// connection; use those in editors.

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

export interface ListRecordsRequest {
  entity: string
  where?: Record<string, unknown> // field id → value the records must have
}

export interface ListRecordsResponse extends ResponseStatus {
  records: RecordData[] // oldest first
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

  async import(request: ImportRecordsRequest): Promise<ImportRecordsResponse> {
    const response = await http.post<ImportRecordsResponse>('/records/records/import', request)
    return response.data
  },
}
