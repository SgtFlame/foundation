// Client for the backend Retrieval REST handlers (backend/features/retrieval/rest)

import { http, type ResponseStatus } from './http/client.ts'
import type { SegmentLocation } from './Conversation.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/retrieval/rest/retrievaldto

export interface RetrieveRequest {
  query: string
  lexicalSemantic: number // −5 (keywords) … +5 (meaning)
  chronicleLibrary: number // −5 (Chronicle memory) … +5 (Volumes)
  topK?: number // 1…50; the retrieval section's default when omitted
  contextWindow?: number // tokens; the budget is a share of it
  sources?: string[] // empty searches every source
  volumeIds?: string[]
}

// A selected segment: score = rrf · axisMult · grounding
export interface RetrievedSegment {
  number: number
  sourceId: string
  key: string
  title: string
  text: string
  location: SegmentLocation
  also?: string[]
  score: number
  rrf: number
  axisMult: number
  grounding: number
  vectorRank?: number
  keywordRank?: number
  pinned?: boolean
  tokens: number
}

export interface RetrieveResponse extends ResponseStatus {
  segments?: RetrievedSegment[]
  candidates: number
  budgetTokens: number
  vectorSkipped?: boolean // the query could not be embedded, so keywords were used alone
}

export const Retrieval = {
  async retrieve(request: RetrieveRequest): Promise<RetrieveResponse> {
    const response = await http.post<RetrieveResponse>('/retrieval/retrieve', request)
    return response.data
  },
}
