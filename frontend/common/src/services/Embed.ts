// Client for the backend Embed REST handlers (backend/features/ai/rest/embedhandlers)

import { http, type ResponseStatus } from './http/client.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/ai/rest/embeddto. uint64 IDs are
// strings, since a JavaScript number cannot hold every uint64.

// Which side of a search a text is on; providers such as Gemini embed them differently
export type EmbedPurpose = 'document' | 'query'

export interface EmbedRequest {
  texts: string[]
  purpose?: EmbedPurpose // default 'document'
  title?: string
  model?: string
}

export interface EmbedResponse extends ResponseStatus {
  vectors?: number[][] // unit length, one per text
  model?: string
  dimensions?: number
  inputTokens?: number
}

export interface DescribeRequest {
}

export interface DescribeResponse extends ResponseStatus {
  model?: string
  dimensions?: number
  maxBatch?: number
}

export const Embed = {
  async embed(request: EmbedRequest): Promise<EmbedResponse> {
    const response = await http.post<EmbedResponse>('/ai/embed', request)
    return response.data
  },

  async describe(request: DescribeRequest = {}): Promise<DescribeResponse> {
    const response = await http.post<DescribeResponse>('/ai/embed/describe', request)
    return response.data
  },
}
