// Client for the backend Index REST handlers (backend/features/index/rest/indexhandlers)

import { http, type ResponseStatus } from './http/client.ts'
import type { VolumePath } from './KnowledgeBase.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/index/rest/indexdto. uint64 IDs are
// strings, since a JavaScript number cannot hold every uint64.

export interface FindRequest {
  query: string
  weight: number // 0..1: the vector search's share of the ranking; keyword (BM25) search gets the rest
  count?: number // the most documents to return, default 10
  volumeIds?: string[] // only search these volumes
}

// Locates a chunk in its file. start/end are byte offsets (UTF-8, not
// JavaScript string indices) and the lines are 1-based; both are exact while
// the file is unchanged since it was indexed, and head/tail find the chunk
// again after it has drifted.
export interface ChunkRef {
  start: number
  end: number
  startLine: number
  endLine: number
  heading?: string
  head: string
  tail: string
  snippet: string
  score: number
  vectorRank?: number
  keywordRank?: number
}

export interface Hit {
  volumeId: string
  path: VolumePath // folders down to the file's name, without its extension
  type: string
  score: number
  chunks: ChunkRef[] // best first
}

export interface FindResponse extends ResponseStatus {
  hits: Hit[] // best first, one per document
}

export interface IndexFileRequest {
  path: VolumePath
  type: string
}

export interface IndexFileResponse extends ResponseStatus {
  chunks: number
  embedded: number // chunks that needed a new embedding
}

export interface ReindexRequest {
  force?: boolean // reindex every file, not only the changed ones
}

export interface ReindexResponse extends ResponseStatus {
  queued: number
}

export const Index = {
  async find(request: FindRequest): Promise<FindResponse> {
    const response = await http.post<FindResponse>('/index/find', request)
    return response.data
  },

  async indexFile(request: IndexFileRequest): Promise<IndexFileResponse> {
    const response = await http.post<IndexFileResponse>('/index/files/index', request)
    return response.data
  },

  async reindex(request: ReindexRequest = {}): Promise<ReindexResponse> {
    const response = await http.post<ReindexResponse>('/index/reindex', request)
    return response.data
  },
}
