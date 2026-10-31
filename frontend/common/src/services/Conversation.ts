// Client for the backend Conversation REST handlers
// (backend/features/conversation/rest/conversationhandlers)

import { API_BASE, http, type ResponseStatus, ServiceError } from './http/client.ts'
import { serverSentEvents } from './http/sse.ts'
import { newTraceId, TRACE_HEADER } from './http/trace.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/conversation/rest/conversationdto.
// uint64 IDs are strings, since a JavaScript number cannot hold every uint64.

export interface Conversation {
  id: string
  created: string // RFC 3339
  title?: string // generated after the first round
  lastRoundAt: string
  contextSettings?: Record<string, unknown> // by chat.contextSettings id
}

// Where a cited segment opens: a Chronicle entry, or a Volume file's lines.
// For a Volume chunk the offsets and lines are exact while the file is
// unchanged since it was indexed.
export interface SegmentLocation {
  entryId?: string // a Chronicle entry
  volumeId?: string
  path?: string // relative to the knowledge base root, with '/' and the extension
  start?: number
  end?: number
  startLine?: number
  endLine?: number
  heading?: string
}

// One segment a round injected, cited as [number], with the numbers the
// Context inspector shows: score = rrf · axisMult · grounding
export interface RoundSegment {
  number: number
  sourceId: string // library or chronicle
  key: string // what feedback counts against: a chunk's content hash, an entry's id
  title: string
  snippet: string
  location: SegmentLocation
  also?: string[] // other paths holding the same text
  score: number
  rrf: number
  axisMult: number
  grounding: number
  vectorRank?: number // 1-based; absent when the vector search missed it
  keywordRank?: number
  pinned?: boolean // put in before the rest, such as the Chronicle entries chosen for the round
  reason?: string // why a pinned segment is in: checked, parent, stage 1 · retrieved, …
  tokens: number
}

// The verdict on a reply: 👍 up, 👎 down, 🚫 ignore; absent is neutral
export type RoundFeedback = 'up' | 'down' | 'ignore'

// 0 while the reply streams, 200 when done, 499 stopped by the user, else the failure's status
export const roundStopped = 499

export interface Round {
  id: string
  conversationId: string
  time: string
  text: string // the user's message
  reply: string
  level: string
  model: string
  status: number
  finishReason?: string
  error?: string // the failure's message
  workbench?: unknown // the workbench snapshot sent with the message
  inputTokens: number // includes cachedTokens
  cachedTokens: number
  outputTokens: number // excludes thinkingTokens
  thinkingTokens: number
  costMicros: number // millionths of currency
  currency?: string
  latencyMs: number
  feedback?: RoundFeedback
  segments?: RoundSegment[] // what the round injected, [1] first
  toolCalls?: ToolCall[] // in the order the model made them
  followed?: Followed[] // what the round followers did, as the reply streamed (not stored)
}

export type ToolEffect = 'none' | 'read' | 'write' | 'external' | 'ui'

// running: a server tool runs; awaiting: waits for the user's approval;
// client: waits for this browser to run it (or the user to answer it)
export type ToolCallStatus = 'running' | 'awaiting' | 'client' | 'done' | 'failed' | 'denied' | 'timeout'

export type ToolApproval = 'auto' | 'approved' | 'always' | 'denied'

// One tool call of a round. A "tool" event brings it each time its status
// changes; awaiting and client calls wait for Conversations.answer.
export interface ToolCall {
  callId: string // unique in its round
  step: number
  name: string // e.g. kb.search
  title?: string
  effect?: ToolEffect
  location?: 'server' | 'client'
  args: Record<string, unknown>
  status: ToolCallStatus
  approval?: ToolApproval
  summary?: string // one line of markdown
  error?: string
  result?: unknown // what the model saw, once it ended
  cited?: number[] // the citations the result added
  durationMs?: number
  time?: string
}

// A chat.clientTool contribution as declared with a message
export interface ClientToolDeclaration {
  name: string
  title?: string
  description: string
  input?: Record<string, unknown> // the arguments' JSON Schema
  effect: 'read' | 'ui'
  waitsForUser?: boolean
}

// What a round follower did after the reply, such as Chronicle's memory update
export interface Followed {
  follower: string // the server's chat.roundFollower id, e.g. chronicle
  summary: string // one line of markdown
  data?: unknown // the follower's details, e.g. { updated, proposed } entry refs
}

export interface ListRequest {
  before?: string // only conversations older than this one
  limit?: number // 5 by default, at most 50
  ids?: string[] // only these conversations
}

export interface ListResponse extends ResponseStatus {
  conversations?: Conversation[] // newest first
  more?: boolean
}

export interface RoundsRequest {
  conversationId: string
}

export interface RoundsResponse extends ResponseStatus {
  rounds?: Round[] // oldest first
}

export interface SendRequest {
  conversationId?: string // omitted starts a new conversation
  text: string
  level?: string // the default level when omitted
  workbench?: unknown // the workbench snapshot
  contextSettings?: Record<string, unknown>
  clientTools?: ClientToolDeclaration[] // the tools the model may call in this browser
}

// Answers a waiting tool call: approval for an awaiting one, result or
// error for a client one
export interface AnswerRequest {
  roundId: string
  callId: string
  approval?: Exclude<ToolApproval, 'auto'>
  result?: unknown // a JSON object
  error?: string
}

export interface FeedbackRequest {
  roundId: string
  feedback: RoundFeedback | '' // '' is neutral
}

export interface FeedbackResponse extends ResponseStatus {
  round?: Round
}

export interface ProviderInfo {
  id: string
  title: string
}

export interface ContextProvidersResponse extends ResponseStatus {
  providers?: ProviderInfo[]
}

export interface SendOptions {
  onStarted?: (conversation: Conversation, round: Round) => void
  onSources?: (segments: RoundSegment[]) => void // before the reply, when the round injected citable context
  onDelta?: (text: string) => void
  onTool?: (call: ToolCall) => void // each change of a tool call's status
  onDone?: (round: Round) => void // before a title, which can follow
  onTitle?: (conversation: Conversation) => void
  onFollowed?: (followed: Followed) => void // after done and any title
  signal?: AbortSignal
}

export const Conversations = {
  async list(request: ListRequest): Promise<ListResponse> {
    const response = await http.post<ListResponse>('/conversation/list', request)
    return response.data
  },

  async rounds(request: RoundsRequest): Promise<RoundsResponse> {
    const response = await http.post<RoundsResponse>('/conversation/rounds', request)
    return response.data
  },

  // Sets the verdict on a round: 👍/👎 reweight the segments it used for
  // later retrievals, 🚫 drops it from later rounds' history
  async feedback(request: FeedbackRequest): Promise<FeedbackResponse> {
    const response = await http.post<FeedbackResponse>('/conversation/feedback', request)
    return response.data
  },

  // Hands a waiting tool call its approval or its client result; 404 when
  // the call is no longer waiting
  async answer(request: AnswerRequest): Promise<ResponseStatus> {
    const response = await http.post<ResponseStatus>('/conversation/answer', request)
    return response.data
  },

  async contextProviders(): Promise<ContextProvidersResponse> {
    const response = await http.get<ContextProvidersResponse>('/conversation/context-providers')
    return response.data
  },

  // Sends a message and streams the round as Server-Sent Events, resolving
  // with the completed round once the stream ends (after the title, when one
  // is generated). A failed reply resolves too, with its status and error;
  // only a refused message throws. Like Chat.stream this uses fetch, so it is
  // logged rather than captured in DevTools' Network tab.
  async send(request: SendRequest, { onStarted, onSources, onDelta, onTool, onDone, onTitle, onFollowed, signal }: SendOptions = {}): Promise<Round> {
    const trace = newTraceId()
    let response: Response
    try {
      response = await fetch(`${API_BASE}/conversation/send`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream', [TRACE_HEADER]: trace },
        body: JSON.stringify(request),
        signal,
      })
    } catch (error) {
      if (signal?.aborted) throw error
      throw new ServiceError(0, error instanceof Error ? error.message : String(error), undefined, trace)
    }
    if (!response.ok || !response.body) {
      const body = (await response.json().catch(() => undefined)) as ResponseStatus | undefined
      throw new ServiceError(response.status, body?.message ?? response.statusText, body?.errorCode, trace)
    }

    let done: Round | undefined
    for await (const { event, data } of serverSentEvents(response.body)) {
      switch (event) {
        case 'started': {
          const started = JSON.parse(data) as { conversation: Conversation; round: Round }
          onStarted?.(started.conversation, started.round)
          break
        }
        case 'sources':
          onSources?.((JSON.parse(data) as { segments: RoundSegment[] }).segments)
          break
        case 'delta':
          onDelta?.((JSON.parse(data) as { text: string }).text)
          break
        case 'tool':
          onTool?.(JSON.parse(data) as ToolCall)
          break
        case 'done':
          done = (JSON.parse(data) as { round: Round }).round
          onDone?.(done)
          break
        case 'title':
          onTitle?.((JSON.parse(data) as { conversation: Conversation }).conversation)
          break
        case 'followed':
          onFollowed?.(JSON.parse(data) as Followed)
          break
        case 'error': {
          const failure = JSON.parse(data) as { code: number; message: string; errorCode?: string }
          throw new ServiceError(failure.code, failure.message, failure.errorCode, trace)
        }
      }
    }
    if (!done) throw new ServiceError(0, 'the reply stream ended early', undefined, trace)
    return done
  },
}
