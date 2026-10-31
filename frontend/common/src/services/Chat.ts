// Client for the backend Chat REST handlers (backend/features/ai/rest/chathandlers)

import { API_BASE, http, type ResponseStatus, ServiceError } from './http/client.ts'
import { serverSentEvents } from './http/sse.ts'
import { newTraceId, TRACE_HEADER } from './http/trace.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/ai/rest/chatdto. uint64 IDs are
// strings, since a JavaScript number cannot hold every uint64.

export type ChatRole = 'user' | 'assistant'

export interface ChatMessage {
  role: ChatRole
  content: string
}

export interface CompleteRequest {
  model?: string
  system?: string // system instructions, kept apart from the messages
  messages: ChatMessage[] // the last one is the user's
  temperature?: number
  maxTokens?: number
  jsonSchema?: unknown // asks for a JSON reply matching this schema
}

export interface CompleteResponse extends ResponseStatus {
  reply?: ChatMessage
  model?: string
  finishReason?: string // 'stop', 'length', 'safety', ...
  inputTokens?: number // includes cachedTokens
  cachedTokens?: number
  outputTokens?: number // excludes thinkingTokens
  thinkingTokens?: number
}

export type StreamRequest = CompleteRequest

export interface StreamOptions {
  onDelta: (text: string) => void
  signal?: AbortSignal
}

export const Chat = {
  async complete(request: CompleteRequest): Promise<CompleteResponse> {
    const response = await http.post<CompleteResponse>('/ai/chat/complete', request)
    return response.data
  },

  // Streams the reply as Server-Sent Events, calling onDelta with each piece,
  // and resolves with the complete response. axios cannot stream in the
  // browser, so this uses fetch and is not captured in DevTools' Network tab.
  async stream(request: StreamRequest, { onDelta, signal }: StreamOptions): Promise<CompleteResponse> {
    const trace = newTraceId()
    let response: Response
    try {
      response = await fetch(`${API_BASE}/ai/chat/stream`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Accept: 'text/event-stream',
          [TRACE_HEADER]: trace,
        },
        body: JSON.stringify(request),
        signal,
      })
    } catch (error) {
      if (signal?.aborted) throw error
      throw new ServiceError(0, error instanceof Error ? error.message : String(error), undefined, trace)
    }
    if (!response.ok || !response.body) {
      // A request that fails before generation starts gets a plain JSON error
      const body = (await response.json().catch(() => undefined)) as CompleteResponse | undefined
      throw new ServiceError(response.status, body?.message ?? response.statusText, body?.errorCode, trace)
    }

    for await (const { event, data } of serverSentEvents(response.body)) {
      switch (event) {
        case 'delta':
          onDelta((JSON.parse(data) as { text: string }).text)
          break
        case 'done':
          return JSON.parse(data) as CompleteResponse
        case 'error': {
          const failure = JSON.parse(data) as { code: number; message: string; errorCode?: string }
          throw new ServiceError(failure.code, failure.message, failure.errorCode, trace)
        }
      }
    }
    throw new ServiceError(0, 'the chat stream ended early', undefined, trace)
  },
}
