// Client for the backend Logs REST handlers (backend/features/logs/rest)

import { API_BASE, http, type ResponseStatus, ServiceError } from './http/client.ts'
import { serverSentEvents } from './http/sse.ts'
import { newTraceId, TRACE_HEADER } from './http/trace.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/logs/rest/logsdto

// A stored record: VictoriaLogs returns every field as a string
export type LogRow = Record<string, string>

export interface TimeRange {
  start?: string // RFC 3339; omitted, end is now and start an hour before it
  end?: string
}

// Where a LogsQL query stopped parsing
export interface QueryError {
  message: string
  offset: number // in bytes; -1 when unknown
}

// Responses to a query may carry where it failed to parse (status 400, errorCode 'bad_query')
export interface QueryStatus extends ResponseStatus {
  queryError?: QueryError
}

export interface ValueHits {
  value: string
  hits: number
}

export interface StatusResponse extends ResponseStatus {
  configured: boolean
  url?: string
  storedLevel: string
  browserLevel: string
  queued: number
  dropped: number
  lastError?: string
  lastErrorAt?: string
}

export interface QueryRequest extends TimeRange {
  query: string
  limit?: number // 500 by default, at most 5000
}

export interface QueryResponse extends QueryStatus {
  rows: LogRow[] // newest first
  limited?: boolean // more matched than limit
}

export interface HitsRequest extends TimeRange {
  query: string
  stepMs?: number // a 60th of the range by default
  field?: string // splits the counts by this field's values
}

export interface HitsSeries {
  fields: Record<string, string>
  timestamps: string[]
  values: number[]
  total: number
}

export interface HitsResponse extends QueryStatus {
  stepMs: number
  series: HitsSeries[]
}

export interface FieldNamesRequest extends TimeRange {
  query: string
}

export interface FieldNamesResponse extends QueryStatus {
  names: ValueHits[]
}

export interface FieldValuesRequest extends TimeRange {
  query: string
  field: string
  limit?: number // 100 by default, at most 1000
}

export interface FieldValuesResponse extends QueryStatus {
  values: ValueHits[]
}

export interface StreamsRequest extends TimeRange {
  query: string
  field?: string // lists this stream field's values instead of the streams
}

export interface StreamsResponse extends QueryStatus {
  values: ValueHits[]
}

export interface TailRequest {
  query: string
}

// A browser LogRecord as forwarded (src/logging/LogRecord.ts); the server
// takes the user and tenant from the session, never from here
export interface BrowserRecord {
  time: string
  level: string
  message: string
  domain?: string
  tags?: string[]
  code?: string
  trace?: string
  span?: string
  hint?: string
  public?: string
  attrs?: Record<string, unknown>
  error?: BrowserError
  url?: string
}

export interface BrowserError {
  name: string
  message: string
  stack?: string
  cause?: BrowserError
}

export interface IngestRequest {
  records: BrowserRecord[] // at most 200
}

export interface IngestResponse extends ResponseStatus {
  accepted: number
}

// A ServiceError for a query that did not parse, with where it stopped
export class QueryServiceError extends ServiceError {
  readonly queryError?: QueryError

  constructor(error: ServiceError, queryError?: QueryError) {
    super(error.status, error.message, error.errorCode, error.trace)
    this.queryError = queryError
  }
}

// Query endpoints resolve a 400 so its queryError can be kept
const keepQueryErrors = { validateStatus: (status: number) => status < 300 || status === 400 }

async function post<Resp extends QueryStatus>(path: string, request: unknown): Promise<Resp> {
  const response = await http.post<Resp>(path, request, keepQueryErrors)
  if (response.status === 400) {
    const body = response.data
    throw new QueryServiceError(
      new ServiceError(400, body.message ?? 'Bad request', body.errorCode, response.config.traceId),
      body.queryError,
    )
  }
  return response.data
}

export interface TailOptions {
  onRow: (row: LogRow) => void
  onReady?: () => void
  signal: AbortSignal
}

// Full logging for one user (documentation/plans/Logging.md)
export interface UserLevel {
  userId: string
  level: string // debug
  setBy: string
  expiresAt: string // RFC 3339
}

export interface SetUserLevelRequest {
  userId: string
  level: 'debug' | '' // '' turns it off
  hours?: number // 24 by default, at most 168
}

export interface SetUserLevelResponse extends ResponseStatus {
  level?: UserLevel
}

export interface UserLevelsResponse extends ResponseStatus {
  levels: UserLevel[]
}

export interface LevelResponse extends ResponseStatus {
  browserLevel: string // where this browser forwards from
  until?: string // set while full logging is on for this user
}

export const Logs = {
  async status(): Promise<StatusResponse> {
    const response = await http.get<StatusResponse>('/logs/status')
    return response.data
  },

  query: (request: QueryRequest) => post<QueryResponse>('/logs/query', request),
  hits: (request: HitsRequest) => post<HitsResponse>('/logs/hits', request),
  fieldNames: (request: FieldNamesRequest) => post<FieldNamesResponse>('/logs/fields/names', request),
  fieldValues: (request: FieldValuesRequest) => post<FieldValuesResponse>('/logs/fields/values', request),
  streams: (request: StreamsRequest) => post<StreamsResponse>('/logs/streams', request),

  // Forwarding is not captured in DevTools' Network tab, or every warning
  // would add a request to it
  async ingest(request: IngestRequest): Promise<IngestResponse> {
    const response = await http.post<IngestResponse>('/logs/ingest', request, { capture: false })
    return response.data
  },

  // For administrators: full logging for one user on or off
  async setUserLevel(request: SetUserLevelRequest): Promise<SetUserLevelResponse> {
    return (await http.post<SetUserLevelResponse>('/logs/user-level', request)).data
  },

  async userLevels(): Promise<UserLevelsResponse> {
    return (await http.post<UserLevelsResponse>('/logs/user-levels', {})).data
  },

  // The level this browser forwards from
  async level(): Promise<LevelResponse> {
    return (await http.get<LevelResponse>('/logs/level', { capture: false })).data
  },

  // Follows new records as Server-Sent Events until signal aborts. axios
  // cannot stream in the browser, so this uses fetch and is not captured.
  async tail(request: TailRequest, { onRow, onReady, signal }: TailOptions): Promise<void> {
    const trace = newTraceId()
    let response: Response
    try {
      response = await fetch(`${API_BASE}/logs/tail`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream', [TRACE_HEADER]: trace },
        body: JSON.stringify(request),
        signal,
      })
    } catch (error) {
      if (signal.aborted) return
      throw new ServiceError(0, error instanceof Error ? error.message : String(error), undefined, trace)
    }
    if (!response.ok || !response.body) {
      const body = (await response.json().catch(() => undefined)) as QueryStatus | undefined
      throw new QueryServiceError(
        new ServiceError(response.status, body?.message ?? response.statusText, body?.errorCode, trace),
        body?.queryError,
      )
    }
    try {
      for await (const { event, data } of serverSentEvents(response.body)) {
        if (event === 'ready') onReady?.()
        else if (event === 'row') onRow(JSON.parse(data) as LogRow)
      }
    } catch (error) {
      if (!signal.aborted) throw error
    }
  },
}
