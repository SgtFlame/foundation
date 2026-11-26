import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { Box, Checkbox, Chip, CircularProgress, Divider, IconButton, ListItemIcon, ListItemText, Menu, MenuItem, Stack, Tooltip, Typography } from '@mui/material'
import CloseIcon from '@mui/icons-material/Close'
import MoreVertIcon from '@mui/icons-material/MoreVert'
import ChevronRightIcon from '@mui/icons-material/ChevronRight'
import { useVirtualizer } from '@tanstack/react-virtual'
import type { ChatMode } from '../../services/Conversation.ts'
import type { ChatLevel } from './levels.ts'
import RoundView, { type RoundActions } from './RoundView.tsx'
import type { ChatRound, ChatThread, WorkspacesChange } from './useChatStream.ts'

// The person's workspaces, for a conversation's Show in… and Move to…
// (forge 16d)
export interface ChatPlaces {
  workspaces: readonly { id: string; name: string }[]
  current?: string // the tab's workspace
  onChange: (thread: ChatThread, change: WorkspacesChange) => void
}

type Row =
  | { kind: 'more'; key: string }
  | { kind: 'divider'; key: string; thread: ChatThread; current: boolean }
  | { kind: 'round'; key: string; round: ChatRound; switched: boolean } // switched: its mode differs from the round before (forge 15e), or the pre-mode switched it (15f)
  | { kind: 'tail'; key: string }

function rowsOf(threads: readonly ChatThread[], more: boolean, tail: boolean): Row[] {
  const rows: Row[] = more ? [{ kind: 'more', key: 'more' }] : []
  threads.forEach((thread, i) => {
    const current = i === threads.length - 1
    if (thread.rounds.length > 0 || i > 0) rows.push({ kind: 'divider', key: `divider-${thread.key}`, thread, current })
    let mode: string | undefined
    for (const round of thread.rounds) {
      // A round the pre-mode switched is marked even as a conversation's first (forge 15f)
      const preSwitched = round.preMode?.outcome === 'switched' || round.preMode?.outcome === 'asked-switched'
      const switched = !round.note && !!round.mode && ((!!mode && round.mode !== mode) || (preSwitched && round.preMode?.from !== round.mode))
      if (!round.note && round.mode) mode = round.mode
      rows.push({ kind: 'round', key: round.id, round, switched })
    }
  })
  if (tail) rows.push({ kind: 'tail', key: 'tail' })
  return rows
}

// A conversation's menu (forge 16d): Show in… a checklist of the person's
// other workspaces, kept at once, and Move to… another workspace
function PlacesMenu({ thread, places, title }: { thread: ChatThread; places: ChatPlaces; title: string }) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null)
  const [sub, setSub] = useState<{ which: 'show' | 'move'; at: HTMLElement } | null>(null)
  const c = thread.conversation
  if (!c?.workspace || places.workspaces.length < 2) return null
  const own = c.workspace
  const shown = c.shownIn ?? []
  const others = places.workspaces.filter((w) => w.id !== own)
  const close = () => {
    setSub(null)
    setAnchor(null)
  }
  return (
    <>
      <Tooltip title="Show in or move to another workspace">
        <IconButton size="small" aria-label={`Workspaces of ${title}`} onClick={(e) => setAnchor(e.currentTarget)}>
          <MoreVertIcon fontSize="inherit" />
        </IconButton>
      </Tooltip>
      <Menu anchorEl={anchor} open={!!anchor} onClose={close}>
        <MenuItem onClick={(e) => setSub({ which: 'show', at: e.currentTarget })}>
          <ListItemText>Show in…</ListItemText>
          <ChevronRightIcon fontSize="small" sx={{ ml: 2 }} />
        </MenuItem>
        <MenuItem onClick={(e) => setSub({ which: 'move', at: e.currentTarget })}>
          <ListItemText>Move to…</ListItemText>
          <ChevronRightIcon fontSize="small" sx={{ ml: 2 }} />
        </MenuItem>
      </Menu>
      <Menu
        anchorEl={sub?.at}
        open={!!sub}
        onClose={() => setSub(null)}
        anchorOrigin={{ vertical: 'top', horizontal: 'right' }}
        transformOrigin={{ vertical: 'top', horizontal: 'left' }}
      >
        {sub?.which === 'show' &&
          others.map((w) => {
            const on = shown.includes(w.id)
            return (
              <MenuItem
                key={w.id}
                dense
                role="menuitemcheckbox"
                aria-checked={on}
                onClick={() => places.onChange(thread, { shownIn: on ? shown.filter((id) => id !== w.id) : [...shown, w.id] })}
              >
                <ListItemIcon>
                  <Checkbox size="small" edge="start" checked={on} tabIndex={-1} disableRipple sx={{ p: 0 }} />
                </ListItemIcon>
                <ListItemText>{w.name}</ListItemText>
              </MenuItem>
            )
          })}
        {sub?.which === 'move' &&
          others.map((w) => (
            <MenuItem
              key={w.id}
              dense
              onClick={() => {
                places.onChange(thread, { move: w.id })
                close()
              }}
            >
              <ListItemText>{w.name}</ListItemText>
            </MenuItem>
          ))}
      </Menu>
    </>
  )
}

function ConversationDivider({ thread, current, onEnd, places }: { thread: ChatThread; current: boolean; onEnd: () => void; places?: ChatPlaces }) {
  const c = thread.conversation
  const title = c?.title || (c ? 'Untitled conversation' : 'New conversation')
  const when = c ? new Date(c.created).toLocaleString() : undefined
  // A conversation of another workspace, shown here (or moved away)
  const from = places && c?.workspace && places.current && c.workspace !== places.current ? places.workspaces.find((w) => w.id === c.workspace)?.name : undefined
  return (
    <Divider sx={{ px: 2, pt: 2, pb: 1 }}>
      <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
        <Chip size="small" label={title} title={when} variant={current ? 'filled' : 'outlined'} />
        {from && (
          <Typography variant="caption" color="text.secondary">
            From your {from} workspace
          </Typography>
        )}
        {places && <PlacesMenu thread={thread} places={places} title={title} />}
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
  modes,
  onLoadEarlier,
  onNewConversation,
  empty,
  onOpenSegment,
  onFeedback,
  onAnswerTool,
  onUndoSwitch,
  clientTools,
  onOpenLink,
  tail,
  places,
}: RoundActions & {
  threads: readonly ChatThread[]
  more: boolean
  loading: boolean
  levels: readonly ChatLevel[]
  modes?: readonly ChatMode[] // for the rounds' mode badges and switch notices
  onLoadEarlier: () => void
  onNewConversation: () => void
  empty?: ReactNode
  // What follows the last round, before the next message: the person's
  // gestures since then (forge 15d)
  tail?: ReactNode
  places?: ChatPlaces // Show in… and Move to… on each conversation (forge 16d)
}) {
  const scroller = useRef<HTMLDivElement>(null)
  const atBottom = useRef(true)
  const fromBottom = useRef<number | undefined>(undefined) // set while earlier conversations load
  const rows = rowsOf(threads, more, !!tail)
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
  const lastRound = rows.findLast((r) => r.kind === 'round')
  const last = lastRound?.kind === 'round' ? lastRound.round : undefined
  // The reply's length and its tool calls' statuses: either grows the last round
  const growth = last ? `${last.reply.length}:${last.toolCalls?.map((c) => c.status).join(',') ?? ''}` : ''

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
  }, [rows.length, growth, virtualizer])

  // A scroll too short to scroll loads earlier conversations itself, since
  // no scroll event would ask for them (forge 16d: a workspace's own chat
  // and those shown there)
  const totalSize = virtualizer.getTotalSize()
  useEffect(() => {
    const el = scroller.current
    if (!el || !more || loading || el.scrollHeight > el.clientHeight + 80) return
    fromBottom.current = el.scrollHeight - el.scrollTop
    onLoadEarlier()
  }, [more, loading, totalSize, onLoadEarlier])

  const onScroll = () => {
    const el = scroller.current
    if (!el) return
    atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40
    if (el.scrollTop < 80 && more && !loading) {
      fromBottom.current = el.scrollHeight - el.scrollTop
      onLoadEarlier()
    }
  }

  const onlyEmpty = threads.length === 1 && threads[0].rounds.length === 0 && !tail
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
                {row.kind === 'divider' && <ConversationDivider thread={row.thread} current={row.current} onEnd={onNewConversation} places={places} />}
                {row.kind === 'tail' && tail}
                {row.kind === 'round' && <RoundView round={row.round} levels={levels} modes={modes} switched={row.switched} onOpenSegment={onOpenSegment} onFeedback={onFeedback} onAnswerTool={onAnswerTool} onUndoSwitch={onUndoSwitch} clientTools={clientTools} onOpenLink={onOpenLink} />}
              </Box>
            )
          })}
        </Box>
      )}
    </Box>
  )
}

export default ChatTranscript
