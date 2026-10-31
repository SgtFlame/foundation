// Client for the backend Usage REST handlers (backend/features/usage/rest)

import { http, type ResponseStatus } from './http/client.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/usage/rest/usagedto. uint64 IDs are
// strings, since a JavaScript number cannot hold every uint64.

export interface UsageRecord {
  id: string
  time: string // RFC 3339
  userId?: string // absent for background work
  feature: string // chat, embed, embed-index, embed-query, ...
  provider: string
  model: string
  level?: string
  conversationId?: string
  roundId?: string
  fileId?: string
  inputTokens: number // includes cachedTokens
  cachedTokens: number
  outputTokens: number
  thinkingTokens: number
  embedTokens: number
  estimated?: boolean // the provider reported no count, so it was estimated from the text
  costMicros: number // millionths of currency
  currency?: string // absent when the model is not in the catalog
  latencyMs: number
  status: number
  captured?: boolean // Usage.capture has its text, until it expires
}

export interface RecordsRequest {
  from?: string // RFC 3339
  to?: string
  feature?: string
  page?: number // from 0
  pageSize?: number // 50 by default, at most 500
}

export interface RecordsResponse extends ResponseStatus {
  records?: UsageRecord[]
  total: number
}

export type SummaryBucket = 'hour' | 'day' | 'month'
export type SummaryGroup = 'feature' | 'model' | 'level' | 'conversation'

export interface SummaryRequest {
  from?: string // RFC 3339
  to?: string
  bucket?: SummaryBucket
  groupBy?: SummaryGroup
  feature?: string
  tzOffsetMinutes?: number // minutes east of UTC, so buckets are the viewer's days
  limit?: number // keep the costliest groups
}

export interface UsageTotals {
  requests: number
  inputTokens: number // includes cachedTokens
  cachedTokens: number
  outputTokens: number
  thinkingTokens: number
  embedTokens: number
  costMicros: number // millionths of USD (Usage.md question 1)
}

export interface SummaryRow extends UsageTotals {
  bucket?: string // "2026-10-23" (day), "2026-10-23 14" (hour), "2026-10" (month)
  group?: string // the feature, model, level or conversation id
}

export interface SummaryResponse extends ResponseStatus {
  totals: UsageTotals
  rows?: SummaryRow[] // bucket order, the costliest group first
}

export interface CaptureRequest {
  recordId: string
}

export interface CapturedMessage {
  role: 'user' | 'assistant'
  content: string
}

// The prompt and reply of one chat call, kept while Settings › System ›
// Debug capture is on; 404 capture_not_found once it has expired
export interface CaptureResponse extends ResponseStatus {
  recordId?: string
  time?: string
  system?: string
  messages?: CapturedMessage[]
  reply?: string
}

export const Usage = {
  async summary(request: SummaryRequest): Promise<SummaryResponse> {
    const response = await http.post<SummaryResponse>('/usage/summary', request)
    return response.data
  },

  async records(request: RecordsRequest): Promise<RecordsResponse> {
    const response = await http.post<RecordsResponse>('/usage/records', request)
    return response.data
  },

  async capture(request: CaptureRequest): Promise<CaptureResponse> {
    const response = await http.post<CaptureResponse>('/usage/capture', request)
    return response.data
  },
}
