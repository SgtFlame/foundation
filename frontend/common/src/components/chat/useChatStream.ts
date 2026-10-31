// The state of a chat surface: its conversations, oldest first, the last
// being the one new messages go to, and the reply streaming into it.

import { useCallback, useEffect, useRef, useState } from 'react'
import { log } from '../../logging/index.ts'
import type { ChatClientToolContribution, ClientToolContext } from '../../editors/chat/points.ts'
import {
  Conversations,
  roundStopped,
  type ClientToolDeclaration,
  type Conversation,
  type Round,
  type RoundFeedback,
  type ToolCall,
} from '../../services/Conversation.ts'
import type { ToolAnswer } from './ToolCallCard.tsx'

const chatLog = log.In('chat')

export interface ChatRound extends Round {
  note?: boolean // a local note, such as /help's, never sent
}

export interface ChatThread {
  key: string // the conversation's id, or a local key until it is stored
  conversation?: Conversation
  rounds: ChatRound[]
  contextSettings: Record<string, Record<string, unknown>> // this conversation's, by contribution id
}

export interface SendOptions {
  level?: string
  workbench?: unknown
  contextSettings?: Record<string, unknown>
  // The chat.clientTool contributions the model may call, and what they run with
  clientTools?: readonly ChatClientToolContribution[]
  toolContext?: ClientToolContext
}

// A client tool as declared to the server: one with a card waits for the user
function declare(tool: ChatClientToolContribution): ClientToolDeclaration {
  return { name: tool.id, title: tool.title, description: tool.description, input: tool.input, effect: tool.effect, waitsForUser: !!tool.Card }
}

function upsertCall(calls: readonly ToolCall[] | undefined, call: ToolCall): ToolCall[] {
  const all = calls ?? []
  return all.some((c) => c.callId === call.callId) ? all.map((c) => (c.callId === call.callId ? call : c)) : [...all, call]
}

const waitingStatuses = ['running', 'awaiting', 'client']

let localKeys = 0

function newThread(): ChatThread {
  return { key: `new-${++localKeys}`, rounds: [], contextSettings: {} }
}

function threadOf(conversation: Conversation, rounds: Round[]): ChatThread {
  return {
    key: conversation.id,
    conversation,
    rounds,
    contextSettings: (conversation.contextSettings ?? {}) as ChatThread['contextSettings'],
  }
}

function localRound(conversationId: string, text: string, reply: string, extra: Partial<ChatRound> = {}): ChatRound {
  return {
    id: `local-${++localKeys}`, conversationId, time: new Date().toISOString(), text, reply, level: '', model: '', status: 0,
    inputTokens: 0, cachedTokens: 0, outputTokens: 0, thinkingTokens: 0, costMicros: 0, latencyMs: 0, ...extra,
  }
}

async function load(before?: string, limit = 1): Promise<{ threads: ChatThread[]; more: boolean }> {
  const list = await Conversations.list({ before, limit })
  const conversations = [...(list.conversations ?? [])].reverse() // oldest first
  const threads = await Promise.all(
    conversations.map(async (c) => threadOf(c, (await Conversations.rounds({ conversationId: c.id })).rounds ?? [])),
  )
  return { threads, more: !!list.more }
}

export function useChatStream() {
  const [threads, setThreads] = useState<ChatThread[]>(() => [newThread()])
  const [more, setMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string>()
  const abort = useRef<AbortController | undefined>(undefined)
  const started = useRef(false)

  const update = useCallback((key: string, fn: (t: ChatThread) => ChatThread) => {
    setThreads((all) => all.map((t) => (t.key === key ? fn(t) : t)))
  }, [])

  // Opens on the latest conversation, so it can be continued
  useEffect(() => {
    if (started.current) return
    started.current = true
    load()
      .then(({ threads: latest, more }) => {
        setMore(more)
        setThreads((all) => (all.length === 1 && all[0].rounds.length === 0 && latest.length > 0 ? latest : all))
      })
      .catch((err: unknown) => {
        chatLog.fromError(err).warn('cannot load the latest conversation')
        setError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => setLoading(false))
  }, [])

  const loadEarlier = useCallback(async () => {
    const oldest = threads.find((t) => t.conversation)?.conversation?.id
    if (!more || loading || !oldest) return
    setLoading(true)
    try {
      const earlier = await load(oldest, 3)
      setMore(earlier.more)
      setThreads((all) => [...earlier.threads, ...all])
    } catch (err) {
      chatLog.fromError(err).warn('cannot load earlier conversations')
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [threads, more, loading])

  const current = threads[threads.length - 1]

  const send = useCallback(
    async (text: string, options: SendOptions = {}) => {
      if (pending) return
      const key = current.key
      const placeholder = localRound(current.conversation?.id ?? '', text, '', { level: options.level ?? '' })
      let roundId = placeholder.id
      const patchRound = (fn: (r: ChatRound) => ChatRound) =>
        update(key, (t) => ({ ...t, rounds: t.rounds.map((r) => (r.id === roundId ? fn(r) : r)) }))
      update(key, (t) => ({ ...t, rounds: [...t.rounds, placeholder] }))
      setPending(true)
      setError(undefined)
      const controller = new AbortController()
      abort.current = controller
      chatLog.With('level', options.level ?? 'default').info('sending a message')
      try {
        await Conversations.send(
          {
            conversationId: current.conversation?.id, text, level: options.level, workbench: options.workbench, contextSettings: options.contextSettings,
            clientTools: options.clientTools?.map(declare),
          },
          {
            signal: controller.signal,
            onStarted: (conversation, round) => {
              const previous = roundId
              roundId = round.id
              update(key, (t) => ({ ...t, conversation, rounds: t.rounds.map((r) => (r.id === previous ? round : r)) }))
            },
            onSources: (segments) => patchRound((r) => ({ ...r, segments })),
            onDelta: (delta) => patchRound((r) => ({ ...r, reply: r.reply + delta })),
            onTool: (call) => {
              patchRound((r) => ({ ...r, toolCalls: upsertCall(r.toolCalls, call) }))
              if (call.status !== 'client') return
              // A client tool runs here, unless its card waits for the user
              const tool = options.clientTools?.find((t) => t.id === call.name)
              if (tool?.Card) return
              const run = async () => {
                if (!tool?.run || !options.toolContext) throw new Error(`this browser has no tool ${call.name}`)
                return await tool.run(call.args, options.toolContext)
              }
              const id = roundId
              void run()
                .then((result) => Conversations.answer({ roundId: id, callId: call.callId, result }))
                .catch((err: unknown) => {
                  chatLog.fromError(err).With('tool', call.name).warn('client tool %s failed', call.name)
                  return Conversations.answer({ roundId: id, callId: call.callId, error: err instanceof Error ? err.message : String(err) })
                })
                .catch((err: unknown) => chatLog.fromError(err).With('tool', call.name).warn('cannot answer tool call %s', call.callId))
            },
            onDone: (round) => {
              patchRound((r) => ({ ...round, segments: r.segments, toolCalls: r.toolCalls }))
              setPending(false)
              if (round.status >= 300) chatLog.With('status', round.status).warn('the reply failed: %s', round.error ?? '')
            },
            onTitle: (conversation) => update(key, (t) => ({ ...t, conversation: { ...t.conversation, ...conversation } })),
            onFollowed: (followed) => patchRound((r) => ({ ...r, followed: [...(r.followed ?? []), followed] })),
          },
        )
      } catch (err) {
        if (controller.signal.aborted) {
          // The server keeps the reply as far as it got, and denies the calls still waiting
          patchRound((r) => ({
            ...r, status: roundStopped, finishReason: 'stopped',
            toolCalls: r.toolCalls?.map((c) => (waitingStatuses.includes(c.status) ? { ...c, status: 'denied', error: 'Stopped' } : c)),
          }))
        } else {
          chatLog.fromError(err).warn('cannot send the message')
          const message = err instanceof Error ? err.message : String(err)
          patchRound((r) => ({ ...r, status: (err as { status?: number }).status || 500, error: message }))
        }
      } finally {
        setPending(false)
        if (abort.current === controller) abort.current = undefined
      }
    },
    [current, pending, update],
  )

  const stop = useCallback(() => abort.current?.abort(), [])

  // Ends the current conversation; the next message starts a new one
  const newConversation = useCallback(() => {
    setThreads((all) => (all[all.length - 1].rounds.some((r) => !r.note) ? [...all, newThread()] : all))
  }, [])

  // A note in the transcript, shown but never sent
  const say = useCallback(
    (markdown: string) => {
      update(current.key, (t) => ({ ...t, rounds: [...t.rounds, localRound('', '', markdown, { note: true, status: 200 })] }))
    },
    [current.key, update],
  )

  // The verdict on a round, shown at once and undone if the server refuses it
  const setFeedback = useCallback(
    async (round: ChatRound, feedback: RoundFeedback | undefined) => {
      const key = threads.find((t) => t.rounds.some((r) => r.id === round.id))?.key
      if (!key) return
      const patch = (value: RoundFeedback | undefined) =>
        update(key, (t) => ({ ...t, rounds: t.rounds.map((r) => (r.id === round.id ? { ...r, feedback: value } : r)) }))
      const previous = round.feedback
      patch(feedback)
      try {
        await Conversations.feedback({ roundId: round.id, feedback: feedback ?? '' })
      } catch (err) {
        chatLog.fromError(err).With('roundId', round.id).warn('cannot store the feedback')
        setError(err instanceof Error ? err.message : String(err))
        patch(previous)
      }
    },
    [threads, update],
  )

  // Answers a tool call the round waits on: the user's approval, or a card's result
  const answerTool = useCallback(async (round: ChatRound, callId: string, answer: ToolAnswer) => {
    try {
      await Conversations.answer({ roundId: round.id, callId, ...answer })
    } catch (err) {
      chatLog.fromError(err).With('roundId', round.id).warn('cannot answer tool call %s', callId)
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [])

  const setContextSetting = useCallback(
    (id: string, value: Record<string, unknown>) => {
      update(current.key, (t) => ({ ...t, contextSettings: { ...t.contextSettings, [id]: value } }))
    },
    [current.key, update],
  )

  return { threads, current, more, loading, pending, error, send, stop, newConversation, say, loadEarlier, setContextSetting, setFeedback, answerTool }
}
