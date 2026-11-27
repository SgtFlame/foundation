// mui.kanban (forge 30b): columns of cards dragged between and within
// columns. Where each card is drawn, what a drop reports and where the
// keyboard moves a card are kanbanHold.ts (pure, tested); this draws it.
// Each column is windowed (@tanstack/react-virtual) and its cards are
// positioned by inline transform, so a drag makes no class per frame and
// a card moved by someone else slides to its new place. A drop is sent as
// one move event and held where it fell until the card's record changes.

import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent as ReactPointerEvent, type ReactNode } from 'react'
import { Badge, Box, IconButton, Paper, Tooltip, Typography, alpha, useTheme, type Theme } from '@mui/material'
import AddIcon from '@mui/icons-material/Add'
import { useVirtualizer } from '@tanstack/react-virtual'
import type { RendererProps } from '../../../points.ts'
import { HOLD_MS, between, layout, live, placedFrom, step, unmoved, type Hold, type Placed } from './kanbanHold.ts'

interface Column {
  id: string
  title: string
  color?: string
  count?: number
  limit?: number
}

interface Drag {
  id: string
  from: string
  x: number
  y: number
  dx: number // the pointer's offset in the card
  dy: number
  width: number
  height: number
  to: string
  index: number
}

const str = (v: unknown) => (v === undefined || v === null ? '' : String(v))
const num = (v: unknown, d: number) => (typeof v === 'number' && Number.isFinite(v) ? v : d)
const GAP = 8
const SLOP = 4

function colorOf(theme: Theme, c?: string): string {
  const named = c && (theme.palette as unknown as Record<string, { main?: string }>)[c]
  if (named && typeof named === 'object' && named.main) return named.main
  return c || theme.palette.divider
}

function columnsFrom(v: unknown): Column[] {
  if (v && !Array.isArray(v) && typeof v === 'object') v = (v as { records?: unknown }).records
  if (!Array.isArray(v)) return []
  return v.map((c) => {
    const o = (c ?? {}) as Record<string, unknown>
    return {
      id: str(o.id ?? o._id),
      title: str(o.title ?? o.name ?? o._title ?? o.id),
      color: o.color === undefined ? undefined : str(o.color),
      count: typeof o.count === 'number' ? o.count : undefined,
      limit: typeof o.limit === 'number' ? o.limit : undefined,
    }
  })
}

export default function Kanban(p: RendererProps) {
  const theme = useTheme()
  const columns = useMemo(() => columnsFrom(p.props.columns), [p.props.columns])
  const order = useMemo(() => columns.map((c) => c.id), [columns])
  const placed = useMemo(() => placedFrom(p.props), [p.props])
  const readOnly = p.props.readOnly === true || !p.handles('move')
  const collapsed = useMemo(() => new Set(((p.props.collapsed as unknown[]) ?? []).map(str)), [p.props.collapsed])
  const busy = (p.props.busy ?? {}) as Record<string, { name?: string }[]>
  const cardHeight = num(p.props.cardHeight, 72)
  const columnWidth = num(p.props.columnWidth, 280)
  const height = typeof p.props.height === 'number' ? p.props.height : undefined
  const titleField = str(p.props.titleField) || 'title'

  // The cards' own records, for a card drawn without a template
  const records = useMemo(() => {
    const m = new Map<string, Record<string, unknown>>()
    let cards = p.props.cards
    if (cards && !Array.isArray(cards) && typeof cards === 'object') cards = (cards as { records?: unknown }).records
    if (Array.isArray(cards)) for (const c of cards) m.set(str((c as Record<string, unknown>)?._id ?? (c as Record<string, unknown>)?.id), c as Record<string, unknown>)
    return m
  }, [p.props.cards])

  // Drops held where they fell, until the server's version of the card moves
  const [holds, setHolds] = useState<Hold[]>([])
  useEffect(() => {
    setHolds((hs) => {
      const next = live(hs, placed, Date.now()).holds
      return next.length === hs.length ? hs : next
    })
  }, [placed])
  useEffect(() => {
    if (!holds.length) return
    const soonest = Math.min(...holds.map((h) => h.at)) + HOLD_MS - Date.now()
    const t = setTimeout(() => setHolds((hs) => live(hs, placed, Date.now()).holds), Math.max(0, soonest) + 10)
    return () => clearTimeout(t)
  }, [holds, placed])
  const cols = useMemo(() => layout(placed, order, holds), [placed, order, holds])

  const emit = useRef(p.emit)
  emit.current = p.emit
  const lift = (id: string) => {
    if (p.handles('lift')) emit.current('lift', { id })
  }
  const drop = (id: string, to: string, index: number) => {
    const from = placed.find((c) => c.id === id)?.column ?? ''
    if (unmoved(cols, id, to, index)) return
    const b = between(cols[to] ?? [], id, index)
    const version = placed.find((c) => c.id === id)?.version
    setHolds((hs) => [...hs.filter((h) => h.id !== id), { id, column: to, index: b.index, version, at: Date.now() }])
    emit.current('move', { id, from, to, index: b.index, after: b.after, before: b.before })
  }

  // Pointer dragging: past SLOP pixels a press becomes a drag; the target
  // is the column under the pointer and the gap between its cards nearest
  const [drag, setDrag] = useState<Drag | null>(null)
  const dragRef = useRef<Drag | null>(null)
  dragRef.current = drag
  const board = useRef<HTMLDivElement>(null)
  const press = useRef<{ id: string; x: number; y: number; el: HTMLElement; pointer: number } | null>(null)

  const targetAt = (x: number, y: number, id: string): { to: string; index: number } | null => {
    const root = board.current
    if (!root) return null
    for (const colEl of Array.from(root.querySelectorAll<HTMLElement>('[data-kanban-column]'))) {
      const r = colEl.getBoundingClientRect()
      if (x < r.left || x > r.right) continue
      const to = colEl.dataset.kanbanColumn ?? ''
      const cards = Array.from(colEl.querySelectorAll<HTMLElement>('[data-kanban-card]')).filter((el) => el.dataset.kanbanCard !== id)
      const rest = (cols[to] ?? []).filter((c) => c.id !== id)
      let index = rest.length
      for (const el of cards) {
        const cr = el.getBoundingClientRect()
        if (y < cr.top + cr.height / 2) {
          index = rest.findIndex((c) => c.id === el.dataset.kanbanCard)
          break
        }
      }
      if (!cards.length) index = 0
      return { to, index: Math.max(0, index) }
    }
    return null
  }

  const onPointerDown = (e: ReactPointerEvent<HTMLElement>, id: string) => {
    if (readOnly || e.button !== 0) return
    press.current = { id, x: e.clientX, y: e.clientY, el: e.currentTarget, pointer: e.pointerId }
  }
  useEffect(() => {
    const move = (e: PointerEvent) => {
      const pr = press.current
      const d = dragRef.current
      if (!d && pr) {
        if (Math.abs(e.clientX - pr.x) + Math.abs(e.clientY - pr.y) < SLOP) return
        const r = pr.el.getBoundingClientRect()
        const from = placed.find((c) => c.id === pr.id)?.column ?? ''
        const t = targetAt(e.clientX, e.clientY, pr.id) ?? { to: from, index: 0 }
        setDrag({ id: pr.id, from, x: e.clientX, y: e.clientY, dx: pr.x - r.left, dy: pr.y - r.top, width: r.width, height: r.height, ...t })
        lift(pr.id)
        return
      }
      if (!d) return
      const t = targetAt(e.clientX, e.clientY, d.id)
      setDrag({ ...d, x: e.clientX, y: e.clientY, ...(t ?? {}) })
    }
    const up = () => {
      const d = dragRef.current
      press.current = null
      if (!d) return
      setDrag(null)
      lift('')
      drop(d.id, d.to, d.index)
    }
    const cancel = (e: globalThis.KeyboardEvent) => {
      if (e.key !== 'Escape' || !dragRef.current) return
      press.current = null
      setDrag(null)
      lift('')
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
    window.addEventListener('keydown', cancel)
    return () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
      window.removeEventListener('keydown', cancel)
    }
  })

  // Keyboard: Space lifts and drops, arrows move, Escape puts it back
  const [lifted, setLifted] = useState<{ id: string; column: string; index: number } | null>(null)
  const [said, setSaid] = useState('')
  // A lifted card moved to another column is drawn by that column: keep
  // the focus on it
  useEffect(() => {
    if (!lifted) return
    const el = board.current?.querySelector<HTMLElement>(`[data-kanban-card="${CSS.escape(lifted.id)}"]`)
    if (el && document.activeElement !== el) el.focus({ preventScroll: false })
  }, [lifted])
  const titleOf = (id: string) => str(records.get(id)?.[titleField] ?? records.get(id)?._title) || 'card'
  const columnTitle = (id: string) => columns.find((c) => c.id === id)?.title ?? id
  const onCardKey = (e: KeyboardEvent<HTMLElement>, id: string, column: string, index: number) => {
    if (e.key === 'Enter' && !lifted) {
      p.emit('open', { id })
      return
    }
    if (readOnly) return
    if (e.key === ' ') {
      e.preventDefault()
      if (!lifted) {
        setLifted({ id, column, index })
        lift(id)
        setSaid(`Lifted ${titleOf(id)}, position ${index + 1} in ${columnTitle(column)}. Arrows move it, Space drops it, Escape cancels.`)
      } else {
        setLifted(null)
        lift('')
        drop(lifted.id, lifted.column, lifted.index)
        setSaid(`Dropped ${titleOf(lifted.id)} in ${columnTitle(lifted.column)}, position ${lifted.index + 1}.`)
      }
      return
    }
    if (lifted && e.key === 'Escape') {
      e.preventDefault()
      setLifted(null)
      lift('')
      setSaid(`${titleOf(lifted.id)} put back.`)
      return
    }
    if (lifted && (e.key === 'ArrowUp' || e.key === 'ArrowDown' || e.key === 'ArrowLeft' || e.key === 'ArrowRight')) {
      e.preventDefault()
      const at = step(cols, order, lifted, lifted.id, e.key)
      setLifted({ ...lifted, ...at })
      setSaid(`${columnTitle(at.column)}, position ${at.index + 1}.`)
    }
  }

  const card = (c: Placed): ReactNode => {
    if (c.key) return p.renderChild(c.key)
    const rec = records.get(c.id)
    return (
      <Paper variant="outlined" sx={{ p: 1.5 }}>
        <Typography variant="body2">{str(rec?.[titleField] ?? rec?._title) || c.id}</Typography>
      </Paper>
    )
  }

  // Columns dragged by their headers
  const [columnDrag, setColumnDrag] = useState<string | null>(null)
  const reorder = p.props.reorderColumns === true && p.handles('columnMove')

  // A card lifted by the keyboard is drawn where it would drop
  const drawn = useMemo(
    () => (lifted ? layout(placed, order, [...holds.filter((h) => h.id !== lifted.id), { id: lifted.id, column: lifted.column, index: lifted.index, version: undefined, at: Number.MAX_SAFE_INTEGER }]) : cols),
    [lifted, placed, order, holds, cols],
  )
  const target = drag ? { column: drag.to, index: drag.index, id: drag.id } : lifted ? { column: lifted.column, index: lifted.index, id: lifted.id } : null

  return (
    <Box
      ref={board}
      sx={{ display: 'flex', gap: 1.5, overflowX: 'auto', height: height ?? '100%', minHeight: 0, p: 0.5, ...((p.props.sx as object) ?? {}) }}
    >
      {columns.map((col, ci) => {
        const items = drawn[col.id] ?? []
        const shown = col.count ?? items.length
        const over = col.limit !== undefined && shown > col.limit
        if (collapsed.has(col.id)) {
          return (
            <Paper key={col.id} variant="outlined" data-kanban-column={col.id} sx={{ width: 44, flex: '0 0 auto', py: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 1 }}>
              <Badge badgeContent={shown} color={over ? 'error' : 'default'} max={9999} />
              <Typography variant="subtitle2" sx={{ writingMode: 'vertical-rl' }}>
                {col.title}
              </Typography>
            </Paper>
          )
        }
        return (
          <Paper
            key={col.id}
            variant="outlined"
            sx={{
              width: columnWidth,
              flex: '0 0 auto',
              display: 'flex',
              flexDirection: 'column',
              minHeight: 0,
              bgcolor: alpha(theme.palette.text.primary, 0.03),
              borderTop: `3px solid ${colorOf(theme, col.color)}`,
              opacity: columnDrag === col.id ? 0.5 : 1,
            }}
            onDragOver={reorder && columnDrag ? (e) => e.preventDefault() : undefined}
            onDrop={
              reorder && columnDrag
                ? (e) => {
                    e.preventDefault()
                    if (columnDrag !== col.id) p.emit('columnMove', { id: columnDrag, index: ci })
                    setColumnDrag(null)
                  }
                : undefined
            }
          >
            <Box
              draggable={reorder}
              onDragStart={reorder ? () => setColumnDrag(col.id) : undefined}
              onDragEnd={reorder ? () => setColumnDrag(null) : undefined}
              sx={{ display: 'flex', alignItems: 'center', gap: 1, px: 1.5, py: 1, cursor: reorder ? 'grab' : undefined }}
            >
              <Typography variant="subtitle2" sx={{ flex: 1, minWidth: 0 }} noWrap>
                {col.title}
              </Typography>
              <Tooltip title={col.limit !== undefined ? `${shown} of a limit of ${col.limit}` : `${shown} card${shown === 1 ? '' : 's'}`}>
                <Typography variant="caption" color={over ? 'error' : 'text.secondary'} sx={{ fontWeight: over ? 700 : undefined }}>
                  {col.limit !== undefined ? `${shown}/${col.limit}` : shown}
                </Typography>
              </Tooltip>
              {p.props.addable === true && p.handles('add') && (
                <IconButton size="small" aria-label={`Add to ${col.title}`} onClick={() => p.emit('add', { column: col.id })}>
                  <AddIcon fontSize="small" />
                </IconButton>
              )}
            </Box>
            <ColumnCards
              column={col.id}
              items={items}
              estimate={cardHeight}
              target={target && target.column === col.id ? target : null}
              dragging={drag?.id ?? ''}
              liftedId={lifted?.id ?? ''}
              busy={busy}
              readOnly={readOnly}
              theme={theme}
              card={card}
              onPointerDown={onPointerDown}
              onKey={onCardKey}
              onOpen={(id) => {
                if (!dragRef.current) p.emit('open', { id })
              }}
            />
          </Paper>
        )
      })}
      {drag && (
        <Box
          sx={{ position: 'fixed', left: 0, top: 0, pointerEvents: 'none', zIndex: theme.zIndex.modal, boxShadow: theme.shadows[8], borderRadius: 1, opacity: 0.95 }}
          style={{ width: drag.width, height: drag.height, transform: `translate(${drag.x - drag.dx}px, ${drag.y - drag.dy}px) rotate(2deg)` }}
        >
          {(() => {
            const c = placed.find((x) => x.id === drag.id)
            return c ? card(c) : null
          })()}
        </Box>
      )}
      <Box aria-live="assertive" sx={{ position: 'absolute', width: 1, height: 1, overflow: 'hidden', clip: 'rect(0 0 0 0)' }}>
        {said}
      </Box>
    </Box>
  )
}

interface ColumnProps {
  column: string
  items: Placed[]
  estimate: number
  target: { index: number; id: string } | null
  dragging: string
  liftedId: string
  busy: Record<string, { name?: string }[]>
  readOnly: boolean
  theme: Theme
  card(c: Placed): ReactNode
  onPointerDown(e: ReactPointerEvent<HTMLElement>, id: string): void
  onKey(e: KeyboardEvent<HTMLElement>, id: string, column: string, index: number): void
  onOpen(id: string): void
}

// ColumnCards is one column's cards, windowed. While a card is dragged or
// lifted over it, the column leaves a gap where it would fall.
function ColumnCards(c: ColumnProps) {
  const scroller = useRef<HTMLDivElement>(null)
  // The cards drawn: the moving card left out, so the gap shows where it goes
  const moving = c.dragging || c.liftedId
  const rows = c.items.filter((x) => x.id !== c.dragging)
  const gapAt = c.target ? Math.min(c.target.index, rows.filter((x) => x.id !== c.target!.id).length) : -1
  const virtual = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scroller.current,
    estimateSize: () => c.estimate + GAP,
    getItemKey: (i) => rows[i]?.id ?? i,
    overscan: 6,
  })
  // Where each row sits, with the gap opened under the target
  let restIndex = 0
  const gap = c.estimate + GAP
  return (
    <Box ref={scroller} data-kanban-column={c.column} sx={{ flex: 1, minHeight: 60, overflowY: 'auto', px: 1, pb: 1 }}>
      <Box sx={{ position: 'relative' }} style={{ height: virtual.getTotalSize() + (gapAt >= 0 && !c.liftedId ? gap : 0) }}>
        {virtual.getVirtualItems().map((v) => {
          const x = rows[v.index]
          if (!x) return null
          const isLifted = x.id === c.liftedId
          // the index among the other cards, for the gap
          if (!isLifted) restIndex = c.items.filter((y) => y.id !== moving).findIndex((y) => y.id === x.id)
          const shift = !c.liftedId && gapAt >= 0 && restIndex >= gapAt ? gap : 0
          const others = c.busy[x.id] ?? []
          const ring = isLifted ? c.theme.palette.primary.main : others.length ? c.theme.palette.secondary.main : undefined
          return (
            <Box
              key={x.id}
              ref={virtual.measureElement}
              data-index={v.index}
              data-kanban-card={x.id}
              role="button"
              tabIndex={0}
              aria-roledescription="Draggable card"
              aria-grabbed={isLifted || undefined}
              onPointerDown={(e) => c.onPointerDown(e, x.id)}
              onClick={() => c.onOpen(x.id)}
              onKeyDown={(e) => c.onKey(e, x.id, c.column, c.items.findIndex((y) => y.id === x.id))}
              sx={{
                position: 'absolute',
                left: 0,
                right: 0,
                pb: `${GAP}px`,
                cursor: c.readOnly ? 'pointer' : 'grab',
                outline: 'none',
                '&:focus-visible > *': { outline: `2px solid ${c.theme.palette.primary.main}`, outlineOffset: 1 },
                touchAction: c.readOnly ? undefined : 'none',
                userSelect: c.readOnly ? undefined : 'none',
              }}
              style={{ transform: `translateY(${v.start + shift}px)`, transition: 'transform 180ms ease' }}
            >
              <Tooltip title={others.length ? others.map((o) => o.name ?? 'Someone').join(', ') : ''} placement="right" disableHoverListener={!others.length}>
                <Box sx={{ borderRadius: 1, boxShadow: ring ? `0 0 0 2px ${ring}` : undefined, transform: isLifted ? 'scale(1.02)' : undefined }}>{c.card(x)}</Box>
              </Tooltip>
            </Box>
          )
        })}
      </Box>
    </Box>
  )
}
