// mui.calendar (forge 28d): dated items in month, week, work week, day and
// agenda layouts. The layout is layout.ts (pure, tested); this draws it and
// turns pointer gestures into the node's events: move and resize on drop,
// select over empty slots, open on a click or Enter, range and view when
// the toolbar changes what is shown. A dropped item stays where it was
// dropped until the server's value for it changes, as inputs do, and
// others' changes arrive as ordinary prop patches. Items are positioned by
// inline style, not sx, so a drag does not make a class per frame.

import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent as ReactPointerEvent, type ReactNode } from 'react'
import { Box, Button, IconButton, ToggleButton, ToggleButtonGroup, Tooltip, Typography, alpha, useTheme, type Theme } from '@mui/material'
import ChevronLeftIcon from '@mui/icons-material/ChevronLeft'
import ChevronRightIcon from '@mui/icons-material/ChevronRight'
import type { RendererProps } from '../../../points.ts'
import {
  addDays,
  clock,
  dayOf,
  inStrip,
  itemsFrom,
  itemsOn,
  layoutDay,
  layoutStrip,
  minutesOf,
  moved,
  movedDays,
  parseDate,
  snap,
  spanOf,
  startOfDay,
  step,
  todayIn,
  validZone,
  visibleDays,
  visibleRange,
  at,
  type Item,
  type View,
} from './layout.ts'

const VIEWS: { id: View; label: string }[] = [
  { id: 'day', label: 'Day' },
  { id: 'workWeek', label: 'Work week' },
  { id: 'week', label: 'Week' },
  { id: 'month', label: 'Month' },
  { id: 'agenda', label: 'Agenda' },
]
const HOUR = 48 // pixels per hour
const PX = HOUR / 60 // pixels per minute
const GUTTER = 56
const LANE = 22
const MONTH_SHOWN = 3

const str = (v: unknown) => (v === undefined || v === null ? '' : String(v))
const isView = (v: unknown): v is View => VIEWS.some((x) => x.id === v)

function colorOf(theme: Theme, c?: string): string {
  const named = c && (theme.palette as unknown as Record<string, { main?: string }>)[c]
  if (named && typeof named === 'object' && named.main) return named.main
  return c || theme.palette.primary.main
}

const dayName = (key: string, style: 'short' | 'long' = 'short') => {
  const { y, m, d } = parseDate(key)
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(undefined, { weekday: style, timeZone: 'UTC' })
}
const dayLabel = (key: string, opts: Intl.DateTimeFormatOptions) => {
  const { y, m, d } = parseDate(key)
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(undefined, { ...opts, timeZone: 'UTC' })
}

function title(view: View, days: string[], date: string): string {
  if (view === 'month') return dayLabel(date, { month: 'long', year: 'numeric' })
  if (view === 'day') return dayLabel(date, { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })
  const a = days[0]
  const b = days[days.length - 1]
  return `${dayLabel(a, { day: 'numeric', month: 'short' })} – ${dayLabel(b, { day: 'numeric', month: 'short', year: 'numeric' })}`
}

// The value an item is drawn with: the server's, or where it was dropped
// until the server's changes
interface Pending {
  start: string
  end: string
  group?: string
  was: string // the server's start|end|group when dropped
}

const sig = (it: Item) => `${it.start}|${it.end}|${it.group ?? ''}`

export function Calendar({ props, emit, handles }: RendererProps) {
  const theme = useTheme()
  const zone = validZone(str(props.zone) || undefined)
  const editable = props.editable === true
  const selectable = props.selectable === true
  const slot = typeof props.slotMinutes === 'number' && props.slotMinutes >= 5 ? props.slotMinutes : 15
  const showNow = props.showNow !== false
  const toolbar = props.toolbar !== false
  const wh = (props.workingHours ?? {}) as { start?: string; end?: string }
  const workStart = minutesOf(wh.start, 9 * 60)
  const workEnd = minutesOf(wh.end, 17 * 60)
  const groups = useMemo(
    () =>
      (Array.isArray(props.groups) ? props.groups : [])
        .filter((g): g is Record<string, unknown> => !!g && typeof g === 'object')
        .map((g) => ({ id: str(g.id), title: str(g.title) || str(g.id) })),
    [props.groups],
  )
  const busy = (props.busy ?? {}) as Record<string, { name?: string }[]>

  // The view and date: the server's, changed here by the toolbar
  const [view, setView] = useState<View>(isView(props.view) ? props.view : 'week')
  const [date, setDate] = useState<string>(str(props.date) || todayIn(zone))
  useEffect(() => {
    if (isView(props.view)) setView(props.view)
  }, [props.view])
  useEffect(() => {
    if (props.date) setDate(str(props.date))
  }, [props.date])
  const days = useMemo(() => visibleDays(view, date), [view, date])

  // Range on first draw and whenever what is shown changes
  const emitRef = useRef(emit)
  emitRef.current = emit
  useEffect(() => {
    if (!handles('range')) return
    emitRef.current('range', { ...visibleRange(view, date, zone), view, date })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view, date, zone])

  const go = (v: View, d: string) => {
    setView(v)
    setDate(d)
    if (handles('view')) emit('view', { view: v, date: d })
  }

  // Items with drops not yet answered
  const [pending, setPending] = useState<Record<string, Pending>>({})
  const raw = useMemo(() => itemsFrom(props.items), [props.items])
  const items = useMemo(
    () =>
      raw.map((it) => {
        const p = pending[it.id]
        return p && p.was === sig(it) ? { ...it, start: p.start, end: p.end, group: p.group ?? it.group } : it
      }),
    [raw, pending],
  )
  useEffect(() => {
    // Forget drops the server has answered (its value changed) or refused
    // by sending the same value again after a while
    setPending((ps) => {
      let changed = false
      const next: Record<string, Pending> = {}
      for (const [id, p] of Object.entries(ps)) {
        const it = raw.find((x) => x.id === id)
        if (it && sig(it) === p.was) next[id] = p
        else changed = true
      }
      return changed ? next : ps
    })
  }, [raw])
  const drop = (it: Item, to: { start: string; end: string; group?: string }) => {
    const orig = raw.find((x) => x.id === it.id)
    if (!orig) return
    setPending((ps) => ({ ...ps, [it.id]: { ...to, was: sig(orig) } }))
    // A refused move is redrawn where it was after a few seconds
    setTimeout(() => setPending((ps) => (ps[it.id]?.was === sig(orig) ? omit(ps, it.id) : ps)), 8000)
  }

  const canDrag = (it: Item) => editable && !it.readOnly && handles('move')
  const open = (it: Item) => handles('open') && emit('open', { id: it.id })

  const itemLabel = (it: Item) => {
    const { start, end } = spanOf(it, zone)
    const when = it.allDay ? dayLabel(it.start.slice(0, 10), { weekday: 'long', day: 'numeric', month: 'long' }) : `${clock(start, zone)} to ${clock(end, zone)}`
    return `${it.title || 'Untitled'}, ${when}`
  }

  // Arrow keys move focus between items in time order; Enter opens
  const root = useRef<HTMLDivElement>(null)
  const onItemKey = (e: KeyboardEvent, it: Item) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      open(it)
      return
    }
    if (!['ArrowDown', 'ArrowUp', 'ArrowLeft', 'ArrowRight'].includes(e.key)) return
    const all = Array.from(root.current?.querySelectorAll<HTMLElement>('[data-cal-item]') ?? [])
    all.sort((a, b) => Number(a.dataset.calStart) - Number(b.dataset.calStart))
    const i = all.findIndex((el) => el.dataset.calItem === it.id)
    const j = e.key === 'ArrowDown' || e.key === 'ArrowRight' ? i + 1 : i - 1
    if (all[j]) {
      e.preventDefault()
      all[j].focus()
    }
  }

  const chip = (it: Item, extra: { style?: React.CSSProperties; compact?: boolean; onPointerDown?: (e: ReactPointerEvent) => void; children?: ReactNode; ghost?: boolean }) => {
    const c = colorOf(theme, it.color)
    const others = busy[it.id]?.filter((x) => x && x.name) ?? []
    const body = (
      <Box
        role="button"
        tabIndex={extra.ghost ? -1 : 0}
        aria-label={itemLabel(it)}
        data-cal-item={extra.ghost ? undefined : it.id}
        data-cal-start={spanOf(it, zone).start}
        onKeyDown={(e) => onItemKey(e, it)}
        onPointerDown={extra.onPointerDown}
        onClick={extra.onPointerDown ? undefined : () => open(it)}
        style={{
          background: alpha(c, theme.palette.mode === 'dark' ? 0.35 : 0.18),
          borderLeft: `3px solid ${c}`,
          color: theme.palette.text.primary,
          boxShadow: others.length ? `0 0 0 2px ${theme.palette.warning.main}` : undefined,
          opacity: extra.ghost ? 0.6 : 1,
          cursor: canDrag(it) ? 'grab' : handles('open') ? 'pointer' : 'default',
          touchAction: canDrag(it) ? 'none' : undefined,
          ...extra.style,
        }}
        sx={{ borderRadius: 1, px: 0.5, overflow: 'hidden', userSelect: 'none', fontSize: 12, lineHeight: 1.3, '&:focus-visible': { outline: 2, outlineColor: 'primary.main' } }}
      >
        <Typography component="span" sx={{ fontSize: 12, fontWeight: 600, display: 'block', whiteSpace: extra.compact ? 'nowrap' : 'normal', textOverflow: 'ellipsis', overflow: 'hidden' }}>
          {!it.allDay && extra.compact ? `${clock(spanOf(it, zone).start, zone)} ` : ''}
          {it.title || 'Untitled'}
        </Typography>
        {extra.children}
      </Box>
    )
    return others.length ? (
      <Tooltip key={it.id} title={others.map((o) => o.name).join(', ')}>
        {body}
      </Tooltip>
    ) : (
      body
    )
  }

  let content: ReactNode
  if (view === 'month') {
    content = <MonthView days={days} date={date} items={items} zone={zone} chip={chip} canDrag={canDrag} drop={drop} emit={emit} handles={handles} selectable={selectable} go={go} theme={theme} />
  } else if (view === 'agenda') {
    content = <AgendaView days={days} items={items} zone={zone} chip={chip} />
  } else {
    content = (
      <TimeGrid
        days={days}
        items={items}
        zone={zone}
        slot={slot}
        groups={view === 'day' ? groups : []}
        workStart={workStart}
        workEnd={workEnd}
        showNow={showNow}
        selectable={selectable && handles('select')}
        editable={editable}
        canDrag={canDrag}
        chip={chip}
        open={open}
        drop={drop}
        emit={emit}
        handles={handles}
        theme={theme}
      />
    )
  }

  return (
    <Box
      ref={root}
      sx={{ display: 'flex', flexDirection: 'column', minHeight: 0, height: typeof props.height === 'number' ? props.height : '100%', ...((props.sx as object) ?? {}) }}
    >
      {toolbar && (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, pb: 1, flexWrap: 'wrap' }}>
          <Button size="small" variant="outlined" onClick={() => go(view, todayIn(zone))}>
            Today
          </Button>
          <IconButton size="small" aria-label="Back" onClick={() => go(view, step(view, date, -1))}>
            <ChevronLeftIcon fontSize="small" />
          </IconButton>
          <IconButton size="small" aria-label="Forward" onClick={() => go(view, step(view, date, 1))}>
            <ChevronRightIcon fontSize="small" />
          </IconButton>
          <Typography variant="subtitle1" sx={{ flex: 1, minWidth: 160 }}>
            {title(view, days, date)}
          </Typography>
          <ToggleButtonGroup size="small" exclusive value={view} onChange={(_, v) => v && go(v as View, date)}>
            {VIEWS.map((v) => (
              <ToggleButton key={v.id} value={v.id} sx={{ px: 1, py: 0.25, textTransform: 'none' }}>
                {v.label}
              </ToggleButton>
            ))}
          </ToggleButtonGroup>
        </Box>
      )}
      <Box sx={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>{content}</Box>
    </Box>
  )
}

function omit<T>(o: Record<string, T>, k: string): Record<string, T> {
  const next = { ...o }
  delete next[k]
  return next
}

type Chip = (it: Item, extra: { style?: React.CSSProperties; compact?: boolean; onPointerDown?: (e: ReactPointerEvent) => void; children?: ReactNode; ghost?: boolean }) => ReactNode

interface GridProps {
  days: string[]
  items: Item[]
  zone: string
  slot: number
  groups: { id: string; title: string }[]
  workStart: number
  workEnd: number
  showNow: boolean
  selectable: boolean
  editable: boolean
  canDrag(it: Item): boolean
  chip: Chip
  open(it: Item): void
  drop(it: Item, to: { start: string; end: string; group?: string }): void
  emit: RendererProps['emit']
  handles: RendererProps['handles']
  theme: Theme
}

// A gesture in progress on the time grid
type Gesture =
  | { kind: 'move'; it: Item; x0: number; y0: number; grab: number; col: number; minute: number; movedYet: boolean }
  | { kind: 'resize'; it: Item; y0: number; col: number; bottom: number }
  | { kind: 'select'; col: number; from: number; to: number }

function TimeGrid(p: GridProps) {
  const { days, items, zone, groups, theme } = p
  // Columns: the days, or in day view with groups one per group
  const cols = groups.length ? groups.map((g) => ({ day: days[0], group: g.id, title: g.title })) : days.map((d) => ({ day: d, group: undefined as string | undefined, title: '' }))
  const timed = items.filter((it) => !inStrip(it, zone))
  const strip = items.filter((it) => inStrip(it, zone))
  const lanes = layoutStrip(strip, days, zone)
  const laneCount = lanes.reduce((n, l) => Math.max(n, l.lane + 1), 0)
  const today = todayIn(zone)

  const scroller = useRef<HTMLDivElement>(null)
  const body = useRef<HTMLDivElement>(null)
  useEffect(() => {
    // Scroll to an hour before the working day starts
    if (scroller.current) scroller.current.scrollTop = Math.max(0, (p.workStart - 60) * PX)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 60000)
    return () => clearInterval(t)
  }, [])

  const [g, setG] = useState<Gesture | null>(null)
  const gRef = useRef<Gesture | null>(null)
  gRef.current = g

  // Pointer position as a column and a minute of its day
  const where = (e: { clientX: number; clientY: number }) => {
    const el = body.current
    if (!el) return { col: 0, minute: 0 }
    const r = el.getBoundingClientRect()
    const w = (r.width - GUTTER) / cols.length
    const col = Math.min(cols.length - 1, Math.max(0, Math.floor((e.clientX - r.left - GUTTER) / w)))
    const minute = Math.min(24 * 60, Math.max(0, (e.clientY - r.top) / PX))
    return { col, minute }
  }

  useEffect(() => {
    if (!g) return
    const onMove = (e: PointerEvent) => {
      const cur = gRef.current
      if (!cur) return
      const w = where(e)
      if (cur.kind === 'move') {
        const far = Math.abs(e.clientX - cur.x0) + Math.abs(e.clientY - cur.y0) > 4
        setG({ ...cur, col: w.col, minute: Math.max(0, snap(w.minute - cur.grab, p.slot)), movedYet: cur.movedYet || far })
      } else if (cur.kind === 'resize') {
        setG({ ...cur, bottom: Math.max(snap(w.minute, p.slot), 0) })
      } else {
        setG({ ...cur, to: snap(w.minute, p.slot) })
      }
    }
    const onUp = () => {
      const cur = gRef.current
      setG(null)
      if (!cur) return
      if (cur.kind === 'move') {
        if (!cur.movedYet) {
          p.open(cur.it)
          return
        }
        const c = cols[cur.col]
        const to = moved(cur.it, c.day, cur.minute, zone)
        const group = c.group !== undefined && c.group !== cur.it.group ? c.group : undefined
        if (to.start === cur.it.start && group === undefined) return
        p.drop(cur.it, { ...to, group: group ?? cur.it.group })
        p.emit('move', group !== undefined ? { id: cur.it.id, ...to, group } : { id: cur.it.id, ...to })
      } else if (cur.kind === 'resize') {
        const c = cols[cur.col]
        const { start } = spanOf(cur.it, zone)
        const sMin = (start - startOfDay(c.day, zone)) / 60000
        const end = at(c.day, Math.max(cur.bottom, sMin + p.slot), zone)
        if (end === cur.it.end) return
        p.drop(cur.it, { start: cur.it.start, end, group: cur.it.group })
        p.emit('resize', { id: cur.it.id, end })
      } else {
        const a = Math.min(cur.from, cur.to)
        const b = Math.max(cur.from, cur.to)
        const c = cols[cur.col]
        const range = { start: at(c.day, a, zone), end: at(c.day, b > a ? b : a + p.slot, zone), allDay: false }
        p.emit('select', c.group !== undefined ? { ...range, group: c.group } : range)
      }
    }
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp, { once: true })
    return () => {
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onUp)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [g !== null])

  const startMove = (e: ReactPointerEvent, it: Item, col: number, top: number) => {
    if (e.button !== 0) return
    e.stopPropagation()
    if (!p.canDrag(it)) {
      p.open(it)
      return
    }
    const w = where(e)
    setG({ kind: 'move', it, x0: e.clientX, y0: e.clientY, grab: w.minute - top, col, minute: top, movedYet: false })
  }
  const startResize = (e: ReactPointerEvent, it: Item, col: number, bottom: number) => {
    e.stopPropagation()
    setG({ kind: 'resize', it, y0: e.clientY, col, bottom })
  }
  const startSelect = (e: ReactPointerEvent) => {
    if (!p.selectable || e.button !== 0) return
    const w = where(e)
    const m = Math.floor(w.minute / p.slot) * p.slot
    setG({ kind: 'select', col: w.col, from: m, to: m + p.slot })
  }

  const colWidth = `calc((100% - ${GUTTER}px) / ${cols.length})`
  const colLeft = (i: number) => `calc(${GUTTER}px + (100% - ${GUTTER}px) * ${i} / ${cols.length})`
  const line = theme.palette.divider

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', flex: 1, minHeight: 0, border: 1, borderColor: 'divider', borderRadius: 1, overflow: 'hidden' }}>
      {/* Column heads */}
      <Box sx={{ display: 'flex', borderBottom: 1, borderColor: 'divider' }} style={{ paddingLeft: GUTTER }}>
        {cols.map((c, i) => (
          <Box key={i} sx={{ flex: 1, textAlign: 'center', py: 0.5, borderLeft: 1, borderColor: 'divider' }}>
            {c.group !== undefined ? (
              <Typography variant="caption" sx={{ fontWeight: 600 }}>
                {c.title}
              </Typography>
            ) : (
              <>
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
                  {dayName(c.day)}
                </Typography>
                <Typography variant="body2" sx={{ fontWeight: c.day === today ? 700 : 400, color: c.day === today ? 'primary.main' : undefined }}>
                  {parseDate(c.day).d}
                </Typography>
              </>
            )}
          </Box>
        ))}
      </Box>
      {/* The all-day strip */}
      {laneCount > 0 && (
        <Box sx={{ position: 'relative', borderBottom: 1, borderColor: 'divider' }} style={{ height: laneCount * LANE + 4 }}>
          {lanes.map((l) => {
            const it = strip.find((x) => x.id === l.id)!
            const first = groups.length ? 0 : l.first
            const last = groups.length ? cols.length - 1 : l.last
            return (
              <Box key={l.id} style={{ position: 'absolute', top: 2 + l.lane * LANE, left: `calc(${colLeft(first)} + 2px)`, width: `calc(${colWidth} * ${last - first + 1} - 4px)`, height: LANE - 2 }}>
                {p.chip(it, { compact: true, style: { height: '100%' } })}
              </Box>
            )
          })}
        </Box>
      )}
      {/* The grid */}
      <Box ref={scroller} sx={{ flex: 1, overflowY: 'auto', minHeight: 0 }}>
        <Box ref={body} sx={{ position: 'relative' }} style={{ height: 24 * HOUR }} onPointerDown={startSelect}>
          {/* Working hours shading and hour lines */}
          {cols.map((_, i) => (
            <Box
              key={`w${i}`}
              style={{
                position: 'absolute',
                left: colLeft(i),
                width: colWidth,
                top: p.workStart * PX,
                height: Math.max(0, p.workEnd - p.workStart) * PX,
                background: alpha(theme.palette.primary.main, 0.04),
                borderLeft: `1px solid ${line}`,
              }}
            />
          ))}
          {Array.from({ length: 24 }, (_, h) => (
            <Box key={h} style={{ position: 'absolute', top: h * HOUR, left: 0, right: 0, height: HOUR, borderTop: h ? `1px solid ${line}` : undefined, pointerEvents: 'none' }}>
              <Typography variant="caption" color="text.secondary" style={{ position: 'absolute', top: -8, left: 6, width: GUTTER - 10, textAlign: 'right' }}>
                {h ? `${String(h).padStart(2, '0')}:00` : ''}
              </Typography>
            </Box>
          ))}
          {/* Items */}
          {cols.map((c, ci) => {
            const mine = timed.filter((it) => (groups.length ? (it.group ?? groups[0]?.id) === c.group : true))
            return layoutDay(mine, c.day, zone).map((pl) => {
              const it = mine.find((x) => x.id === pl.id)!
              const dragging = g && g.kind !== 'select' && g.it.id === it.id
              const bottom = g && g.kind === 'resize' && g.it.id === it.id ? Math.max(g.bottom, pl.top + p.slot) : pl.bottom
              return (
                <Box
                  key={`${ci}-${it.id}`}
                  style={{
                    position: 'absolute',
                    top: pl.top * PX,
                    height: Math.max(16, (bottom - pl.top) * PX - 1),
                    left: `calc(${colLeft(ci)} + (${colWidth}) * ${pl.col} / ${pl.cols} + 1px)`,
                    width: `calc((${colWidth}) / ${pl.cols} - 3px)`,
                    opacity: dragging && g!.kind === 'move' && g!.movedYet ? 0.35 : 1,
                  }}
                >
                  {p.chip(it, {
                    style: { height: '100%' },
                    compact: bottom - pl.top < 40,
                    onPointerDown: (e) => startMove(e, it, ci, pl.top),
                    children:
                      bottom - pl.top >= 40 ? (
                        <Typography component="span" sx={{ fontSize: 11, color: 'text.secondary' }}>
                          {clock(spanOf(it, zone).start, zone)} – {clock(spanOf(it, zone).end, zone)}
                        </Typography>
                      ) : null,
                  })}
                  {p.canDrag(it) && p.handles('resize') && (
                    <Box
                      aria-hidden
                      onPointerDown={(e) => startResize(e, it, ci, pl.bottom)}
                      style={{ position: 'absolute', left: 0, right: 0, bottom: 0, height: 6, cursor: 'ns-resize', touchAction: 'none' }}
                    />
                  )}
                </Box>
              )
            })
          })}
          {/* The ghost of a move */}
          {g && g.kind === 'move' && g.movedYet && (() => {
            const { start, end } = spanOf(g.it, zone)
            const len = (end - start) / 60000
            return (
              <Box style={{ position: 'absolute', top: g.minute * PX, height: Math.max(16, len * PX - 1), left: `calc(${colLeft(g.col)} + 1px)`, width: `calc(${colWidth} - 3px)`, pointerEvents: 'none', zIndex: 3 }}>
                {p.chip(g.it, { ghost: true, style: { height: '100%' }, children: <Typography component="span" sx={{ fontSize: 11 }}>{clockOfMinute(g.minute)}</Typography> })}
              </Box>
            )
          })()}
          {/* The selection */}
          {g && g.kind === 'select' && (
            <Box
              style={{
                position: 'absolute',
                top: Math.min(g.from, g.to) * PX,
                height: Math.max(p.slot, Math.abs(g.to - g.from)) * PX,
                left: `calc(${colLeft(g.col)} + 1px)`,
                width: `calc(${colWidth} - 3px)`,
                background: alpha(theme.palette.primary.main, 0.25),
                border: `1px dashed ${theme.palette.primary.main}`,
                borderRadius: 4,
                pointerEvents: 'none',
                zIndex: 3,
              }}
            />
          )}
          {/* Now */}
          {p.showNow &&
            cols.map((c, i) =>
              c.day === dayOf(now, zone) ? (
                <Box
                  key={`now${i}`}
                  aria-hidden
                  style={{ position: 'absolute', top: ((now - startOfDay(c.day, zone)) / 60000) * PX, left: colLeft(i), width: colWidth, height: 2, background: theme.palette.error.main, pointerEvents: 'none', zIndex: 2 }}
                />
              ) : null,
            )}
        </Box>
      </Box>
    </Box>
  )
}

const clockOfMinute = (m: number) => `${String(Math.floor(m / 60) % 24).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`

interface MonthProps {
  days: string[]
  date: string
  items: Item[]
  zone: string
  chip: Chip
  canDrag(it: Item): boolean
  drop(it: Item, to: { start: string; end: string }): void
  emit: RendererProps['emit']
  handles: RendererProps['handles']
  selectable: boolean
  go(v: View, d: string): void
  theme: Theme
}

function MonthView(p: MonthProps) {
  const { days, items, zone, theme } = p
  const month = parseDate(p.date).m
  const today = todayIn(zone)
  const [dragging, setDragging] = useState<{ it: Item; from: string } | null>(null)
  const [over, setOver] = useState<string | null>(null)
  const [sel, setSel] = useState<{ a: string; b: string } | null>(null)
  const selRef = useRef(sel)
  selRef.current = sel
  useEffect(() => {
    if (!sel) return
    const up = () => {
      const s = selRef.current
      setSel(null)
      if (!s) return
      const [a, b] = s.a <= s.b ? [s.a, s.b] : [s.b, s.a]
      p.emit('select', { start: a, end: addDays(b, 1), allDay: true })
    }
    window.addEventListener('pointerup', up, { once: true })
    return () => window.removeEventListener('pointerup', up)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sel !== null])
  const inSel = (d: string) => sel && ((d >= sel.a && d <= sel.b) || (d >= sel.b && d <= sel.a))

  return (
    <Box sx={{ flex: 1, minHeight: 0, display: 'grid', gridTemplateColumns: 'repeat(7, 1fr)', gridTemplateRows: 'auto repeat(6, 1fr)', border: 1, borderColor: 'divider', borderRadius: 1, overflow: 'hidden' }}>
      {days.slice(0, 7).map((d) => (
        <Typography key={d} variant="caption" color="text.secondary" sx={{ textAlign: 'center', py: 0.5, borderBottom: 1, borderColor: 'divider' }}>
          {dayName(d)}
        </Typography>
      ))}
      {days.map((d) => {
        const list = itemsOn(items, d, zone)
        const extra = list.length - MONTH_SHOWN
        return (
          <Box
            key={d}
            onDragOver={(e) => {
              if (dragging) {
                e.preventDefault()
                setOver(d)
              }
            }}
            onDragLeave={() => setOver((o) => (o === d ? null : o))}
            onDrop={(e) => {
              e.preventDefault()
              setOver(null)
              if (!dragging) return
              const n = Math.round((Date.UTC(parseDate(d).y, parseDate(d).m - 1, parseDate(d).d) - Date.UTC(parseDate(dragging.from).y, parseDate(dragging.from).m - 1, parseDate(dragging.from).d)) / 86400000)
              setDragging(null)
              if (!n) return
              const to = movedDays(dragging.it, n, zone)
              p.drop(dragging.it, to)
              p.emit('move', { id: dragging.it.id, ...to })
            }}
            onPointerDown={(e) => {
              if (p.selectable && p.handles('select') && e.button === 0 && e.target === e.currentTarget) setSel({ a: d, b: d })
            }}
            onPointerEnter={() => sel && setSel({ a: sel.a, b: d })}
            style={{
              background: over === d || inSel(d) ? alpha(theme.palette.primary.main, 0.15) : parseDate(d).m !== month ? alpha(theme.palette.text.primary, 0.03) : undefined,
            }}
            sx={{ borderTop: 1, borderLeft: 1, borderColor: 'divider', p: 0.5, minHeight: 72, display: 'flex', flexDirection: 'column', gap: 0.25, overflow: 'hidden' }}
          >
            <Typography
              variant="caption"
              sx={{ fontWeight: d === today ? 700 : 400, color: d === today ? 'primary.main' : parseDate(d).m !== month ? 'text.disabled' : 'text.secondary', cursor: 'pointer', pointerEvents: 'auto' }}
              onClick={() => p.go('day', d)}
            >
              {parseDate(d).d}
            </Typography>
            {list.slice(0, extra > 0 ? MONTH_SHOWN - 1 : MONTH_SHOWN).map((it) => (
              <Box
                key={it.id}
                draggable={p.canDrag(it)}
                onDragStart={(e) => {
                  e.dataTransfer.effectAllowed = 'move'
                  e.dataTransfer.setData('text/plain', it.id)
                  setDragging({ it, from: d })
                }}
                onDragEnd={() => setDragging(null)}
              >
                {p.chip(it, { compact: true })}
              </Box>
            ))}
            {extra > 0 && (
              <Typography variant="caption" color="text.secondary" sx={{ cursor: 'pointer' }} onClick={() => p.go('day', d)}>
                +{extra + 1} more
              </Typography>
            )}
          </Box>
        )
      })}
    </Box>
  )
}

function AgendaView({ days, items, zone, chip }: { days: string[]; items: Item[]; zone: string; chip: Chip }) {
  const groups = days.map((d) => ({ d, list: itemsOn(items, d, zone) })).filter((g) => g.list.length)
  if (!groups.length) {
    return (
      <Typography variant="body2" color="text.secondary" sx={{ p: 2 }}>
        Nothing in these two weeks.
      </Typography>
    )
  }
  return (
    <Box sx={{ overflowY: 'auto', flex: 1, minHeight: 0 }}>
      {groups.map(({ d, list }) => (
        <Box key={d} sx={{ display: 'flex', gap: 2, py: 1, borderBottom: 1, borderColor: 'divider' }}>
          <Box sx={{ width: 110, flexShrink: 0 }}>
            <Typography variant="body2" sx={{ fontWeight: 600 }}>
              {dayLabel(d, { weekday: 'short', day: 'numeric', month: 'short' })}
            </Typography>
          </Box>
          <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column', gap: 0.5 }}>
            {list.map((it) => {
              const { start, end } = spanOf(it, zone)
              return (
                <Box key={it.id} sx={{ display: 'flex', gap: 1, alignItems: 'center' }}>
                  <Typography variant="caption" color="text.secondary" sx={{ width: 96, flexShrink: 0 }}>
                    {it.allDay || inStrip(it, zone) ? 'All day' : `${clock(start, zone)} – ${clock(end, zone)}`}
                  </Typography>
                  <Box sx={{ flex: 1 }}>{chip(it, { compact: true })}</Box>
                </Box>
              )
            })}
          </Box>
        </Box>
      ))}
    </Box>
  )
}

export default Calendar
