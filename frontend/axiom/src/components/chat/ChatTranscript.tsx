import { useEffect, useLayoutEffect, useRef, type ReactNode } from 'react'
import { Box, Chip, CircularProgress, Divider, IconButton, Stack, Tooltip, Typography } from '@mui/material'
import CloseIcon from '@mui/icons-material/Close'
import { useVirtualizer } from '@tanstack/react-virtual'
import type { ChatLevel } from './levels.ts'
import RoundView, { type RoundActions } from './RoundView.tsx'
import type { ChatRound, ChatThread } from './useChatStream.ts'

type Row =
  | { kind: 'more'; key: string }
  | { kind: 'divider'; key: string; thread: ChatThread; current: boolean }
  | { kind: 'round'; key: string; round: ChatRound }

function rowsOf(threads: readonly ChatThread[], more: boolean): Row[] {
  const rows: Row[] = more ? [{ kind: 'more', key: 'more' }] : []
  threads.forEach((thread, i) => {
    const current = i === threads.length - 1
    if (thread.rounds.length > 0 || i > 0) rows.push({ kind: 'divider', key: `divider-${thread.key}`, thread, current })
    for (const round of thread.rounds) rows.push({ kind: 'round', key: round.id, round })
  })
  return rows
}

function ConversationDivider({ thread, current, onEnd }: { thread: ChatThread; current: boolean; onEnd: () => void }) {
  const c = thread.conversation
  const title = c?.title || (c ? 'Untitled conversation' : 'New conversation')
  const when = c ? new Date(c.created).toLocaleString() : undefined
  return (
    <Divider sx={{ px: 2, pt: 2, pb: 1 }}>
      <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
        <Chip size="small" label={title} title={when} variant={current ? 'filled' : 'outlined'} />
        {current && thread.rounds.some((r) => !r.note) && (
          <Tooltip title="End this conversation; the next message starts a new one (/new)">
            <IconButton size="small" onClick={onEnd} aria-label="New conversation">
              <CloseIcon fontSize="inherit" />
            </IconButton>
          </Tooltip>
        )}
      </Stack>
    </Divider>
  )
}

// The continuous scroll of conversations, virtualized. It follows the reply
// while the view is at the bottom, and loads earlier conversations when
// scrolled to the top, keeping the view where it was.
function ChatTranscript({
  threads,
  more,
  loading,
  levels,
  onLoadEarlier,
  onNewConversation,
  empty,
  onOpenSegment,
  onFeedback,
  onAnswerTool,
  clientTools,
}: RoundActions & {
  threads: readonly ChatThread[]
  more: boolean
  loading: boolean
  levels: readonly ChatLevel[]
  onLoadEarlier: () => void
  onNewConversation: () => void
  empty?: ReactNode
}) {
  const scroller = useRef<HTMLDivElement>(null)
  const atBottom = useRef(true)
  const fromBottom = useRef<number | undefined>(undefined) // set while earlier conversations load
  const rows = rowsOf(threads, more)
  // The project does not use the React Compiler, which is what this warns about
  // oxlint-disable-next-line react/incompatible-library
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scroller.current,
    estimateSize: () => 120,
    getItemKey: (i) => rows[i].key,
    overscan: 4,
  })

  const firstKey = rows[0]?.key
  const lastReply = rows.length > 0 ? rows[rows.length - 1] : undefined
  const last = lastReply?.kind === 'round' ? lastReply.round : undefined
  // The reply's length and its tool calls' statuses: either grows the last round
  const tail = last ? `${last.reply.length}:${last.toolCalls?.map((c) => c.status).join(',') ?? ''}` : ''

  // Keeps the view in place when earlier conversations are prepended
  useLayoutEffect(() => {
    const el = scroller.current
    if (!el || fromBottom.current === undefined) return
    el.scrollTop = el.scrollHeight - fromBottom.current
    fromBottom.current = undefined
  }, [firstKey])

  // Follows new rounds, the streaming reply and its tool calls while at the bottom
  useEffect(() => {
    if (atBottom.current && rows.length > 0) virtualizer.scrollToIndex(rows.length - 1, { align: 'end' })
  }, [rows.length, tail, virtualizer])

  const onScroll = () => {
    const el = scroller.current
    if (!el) return
    atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40
    if (el.scrollTop < 80 && more && !loading) {
      fromBottom.current = el.scrollHeight - el.scrollTop
      onLoadEarlier()
    }
  }

  const onlyEmpty = threads.length === 1 && threads[0].rounds.length === 0
  return (
    <Box ref={scroller} onScroll={onScroll} sx={{ flex: 1, overflow: 'auto', minHeight: 0 }}>
      {onlyEmpty && !loading ? (
        empty
      ) : (
        <Box sx={{ height: virtualizer.getTotalSize(), position: 'relative', maxWidth: 860, mx: 'auto' }}>
          {virtualizer.getVirtualItems().map((item) => {
            const row = rows[item.index]
            return (
              <Box
                key={item.key}
                data-index={item.index}
                ref={virtualizer.measureElement}
                sx={{ position: 'absolute', top: 0, left: 0, width: '100%', transform: `translateY(${item.start}px)` }}
              >
                {row.kind === 'more' && (
                  <Stack sx={{ alignItems: 'center', py: 1 }}>
                    {loading ? (
                      <CircularProgress size={16} />
                    ) : (
                      <Typography variant="caption" color="text.secondary">
                        Scroll up for earlier conversations
                      </Typography>
                    )}
                  </Stack>
                )}
                {row.kind === 'divider' && <ConversationDivider thread={row.thread} current={row.current} onEnd={onNewConversation} />}
                {row.kind === 'round' && <RoundView round={row.round} levels={levels} onOpenSegment={onOpenSegment} onFeedback={onFeedback} onAnswerTool={onAnswerTool} clientTools={clientTools} />}
              </Box>
            )
          })}
        </Box>
      )}
    </Box>
  )
}

export default ChatTranscript
