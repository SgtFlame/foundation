// Client for the backend Chat handlers (backend/features/ai/rest/chathandlers):
// complete over REST, stream as a live call

import { live } from '../live/LiveConnection.ts'
import { http, type ResponseStatus, ServiceError } from './http/client.ts'

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

  // Streams the reply over the live connection (the ai.chat.stream call),
  // calling onDelta with each piece, and resolves with the complete
  // response; a failure rejects with its ServiceError.
  async stream(request: StreamRequest, { onDelta, signal }: StreamOptions): Promise<CompleteResponse> {
    let done: CompleteResponse | undefined
    await live.call(
      'ai.chat.stream',
      request,
      (event, data) => {
        if (event === 'delta') onDelta((data as { text: string }).text)
        else if (event === 'done') done = data as CompleteResponse
      },
      signal,
    )
    if (!done) throw new ServiceError(0, 'the chat stream ended early')
    return done
  },
}
