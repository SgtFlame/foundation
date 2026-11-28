// mui.virtualList and mui.virtualGrid (forge 07l): the browser half of the
// server's windowed collections (backend/features/ui/Window.go). The
// server sends count (every record), offset and the window's rows: a
// virtualList's as rendered children (its row template, one per id), a
// virtualGrid's as records whose cells are drawn here. The browser
// virtualizes the scrolling over count rows of a fixed height, shows the
// window's rows and a placeholder elsewhere, and sends window {first,
// last} only when the rows shown near the window's edge, so scrolling
// within it costs no round trip.
//
// reverse (forge 26d-2) is a chat's message list: the first record (the
// newest, with a descending sort) is drawn at the bottom, rows take their
// own heights (measured), the list opens at the bottom and follows new
// records only while it is there (else a New messages button shows), and
// endReached is sent when the oldest record scrolls into view at the top.
// window and selection still speak in record indexes.

import { useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type MouseEvent, type ReactNode } from 'react'
import { Box, Button, Skeleton, TableSortLabel, Typography } from '@mui/material'
import { useVirtualizer } from '@tanstack/react-virtual'
import type { RendererProps } from '../../points.ts'
import { TableCellValue, type Column } from '../mui/components.tsx'

const num = (v: unknown, d: number) => (typeof v === 'number' && Number.isFinite(v) ? v : d)
const str = (v: unknown) => (v === undefined || v === null ? '' : String(v))

interface Windowed {
  scroller: React.RefObject<HTMLDivElement | null>
  items: { index: number; display: number; key: string | number; start: number; size: number }[]
  reverse: boolean
  measure(el: Element | null): void
  unseen: boolean
  toBottom(): void
  total: number
  offset: number
  ids: string[]
  loaded(index: number): boolean
  selected: Set<string>
  focus: number
  rowProps(index: number): { onClick(e: MouseEvent): void; onDoubleClick(): void; 'aria-selected': boolean; 'data-row': number; 'data-id'?: string }
  onKeyDown(e: KeyboardEvent): void
}

function useWindowed(p: RendererProps, rowHeight: number, reverse = false): Windowed {
  const count = num(p.props.count, 0)
  const offset = num(p.props.offset, 0)
  const ids = useMemo(() => ((p.props.ids as unknown[]) ?? []).map(str), [p.props.ids])
  const selection = str(p.props.selection) || 'none'
  const scroller = useRef<HTMLDivElement>(null)
  // A record's index i is drawn at display d: the same, or count-1-i reversed
  const disp = (i: number) => (reverse ? count - 1 - i : i)
  const virtual = useVirtualizer({
    count,
    getScrollElement: () => scroller.current,
    estimateSize: () => rowHeight,
    overscan: 4,
    // Reversed rows are measured, so a row keeps its height by its record
    getItemKey: reverse ? (d) => { const i = count - 1 - d; return i >= offset && i < offset + ids.length ? ids[i - offset] : `#${d}` } : undefined,
  })
  const items = virtual.getVirtualItems().map((v) => ({ index: disp(v.index), display: v.index, key: v.key as string | number, start: v.start, size: v.size, end: v.end }))
  const emit = useRef(p.emit)
  emit.current = p.emit

  // The rows shown, from the scroll position (rows are of one height);
  // reversed, from the measured rows on screen, in record indexes
  const [fixedView, setView] = useState({ first: 0, last: 0 })
  const top = virtual.scrollOffset ?? 0
  const screen = virtual.scrollRect?.height ?? 0
  const onScreen = items.filter((v) => v.end > top && v.start < top + screen)
  const view = reverse
    ? { first: Math.max(0, onScreen.length ? onScreen[onScreen.length - 1].index : 0), last: Math.max(0, onScreen.length ? onScreen[0].index : 0) }
    : fixedView
  useEffect(() => {
    const el = scroller.current
    if (!el || reverse) return
    const read = () => {
      const first = Math.floor(el.scrollTop / rowHeight)
      const last = Math.max(first, Math.ceil((el.scrollTop + el.clientHeight) / rowHeight) - 1)
      setView((v) => (v.first === first && v.last === last ? v : { first, last }))
    }
    read()
    el.addEventListener('scroll', read, { passive: true })
    const ro = new ResizeObserver(read)
    ro.observe(el)
    return () => {
      el.removeEventListener('scroll', read)
      ro.disconnect()
    }
  }, [rowHeight, reverse])

  // Reversed: open at the bottom, and follow new records while there
  const atBottom = useRef(true)
  const [unseen, setUnseen] = useState(false)
  const seen = useRef(0)
  useEffect(() => {
    const el = scroller.current
    if (!el || !reverse) return
    const read = () => {
      atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 32
      if (atBottom.current) setUnseen(false)
    }
    el.addEventListener('scroll', read, { passive: true })
    return () => el.removeEventListener('scroll', read)
  }, [reverse])
  const toBottom = () => {
    if (count > 0) virtual.scrollToIndex(count - 1, { align: 'end' })
    atBottom.current = true
    setUnseen(false)
  }
  useLayoutEffect(() => {
    if (!reverse || count === 0) return
    if (seen.current === 0 || atBottom.current) toBottom()
    else if (count > seen.current) setUnseen(true)
    seen.current = count
    // toBottom reads the virtualizer, which is stable
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [count, reverse])

  // window, when the rows shown near the window's edge (or on first sight,
  // so the server sizes it to the viewport); at most every 100 ms
  const sent = useRef<string>('')
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
  const lastSent = useRef(0)
  useEffect(() => {
    if (!p.handles('window')) return
    const { first, last } = view
    const span = last - first + 1
    const end = offset + ids.length
    const margin = Math.ceil(span / 2)
    const near = (first < offset + margin && offset > 0) || (last > end - 1 - margin && end < count) || first < offset || last >= end
    const key = `${first}:${last}`
    if ((!near && sent.current !== '') || sent.current === key) return
    const send = () => {
      sent.current = key
      lastSent.current = Date.now()
      emit.current('window', { first, last })
    }
    clearTimeout(timer.current)
    const wait = 100 - (Date.now() - lastSent.current)
    if (wait <= 0) send()
    else timer.current = setTimeout(send, wait)
    // p.handles is stable for the node
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view.first, view.last, offset, ids.length, count])
  useEffect(() => () => clearTimeout(timer.current), [])

  // endReached, once per count
  const ended = useRef(-1)
  useEffect(() => {
    if (count > 0 && view.last >= count - 1 && ended.current !== count) {
      ended.current = count
      emit.current('endReached', { count })
    }
  }, [view.last, count])

  // Selection: kept here, sent as select; the server's prop replaces it
  const server = useMemo(() => new Set(((p.props.selected as unknown[]) ?? []).map(str)), [p.props.selected])
  const [selected, setSelected] = useState(server)
  useEffect(() => setSelected(server), [server])
  const [focus, setFocus] = useState(-1)
  const anchor = useRef(-1)
  const loaded = (i: number) => i >= offset && i < offset + ids.length
  const idAt = (i: number) => (loaded(i) ? ids[i - offset] : undefined)

  function choose(next: Set<string>, id: string) {
    setSelected(next)
    emit.current('select', { selected: [...next], id })
  }

  function select(i: number, how: { toggle?: boolean; range?: boolean }) {
    const id = idAt(i)
    if (id === undefined || selection === 'none') return
    setFocus(i)
    if (selection === 'single') {
      anchor.current = i
      choose(new Set([id]), id)
    } else if (how.range && anchor.current >= 0) {
      // A range covers the rows the window holds
      const next = new Set<string>()
      for (let k = Math.min(anchor.current, i); k <= Math.max(anchor.current, i); k++) {
        const x = idAt(k)
        if (x !== undefined) next.add(x)
      }
      choose(next, id)
    } else if (how.toggle) {
      anchor.current = i
      const next = new Set(selected)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      choose(next, id)
    } else {
      anchor.current = i
      choose(new Set([id]), id)
    }
  }

  function move(to: number, range: boolean) {
    const i = Math.max(0, Math.min(count - 1, to))
    virtual.scrollToIndex(disp(i), { align: 'auto' })
    setFocus(i)
    if (idAt(i) !== undefined) select(i, { range })
  }

  function onKeyDown(e: KeyboardEvent) {
    const page = Math.max(1, view.last - view.first)
    // Reversed, down is toward the first record
    const dir = reverse ? -1 : 1
    const at = focus < 0 ? (reverse ? view.last + 1 : view.first - 1) : focus
    switch (e.key) {
      case 'ArrowDown': e.preventDefault(); move(at + dir, e.shiftKey); break
      case 'ArrowUp': e.preventDefault(); move(at - dir, e.shiftKey); break
      case 'PageDown': e.preventDefault(); move(at + dir * page, e.shiftKey); break
      case 'PageUp': e.preventDefault(); move(at - dir * page, e.shiftKey); break
      case 'Home': e.preventDefault(); move(reverse ? count - 1 : 0, e.shiftKey); break
      case 'End': e.preventDefault(); move(reverse ? 0 : count - 1, e.shiftKey); break
      case 'Enter': {
        const id = idAt(focus)
        if (id !== undefined) emit.current('open', { id })
        break
      }
      case ' ':
        if (focus >= 0 && selection === 'multiple') {
          e.preventDefault()
          select(focus, { toggle: true })
        }
        break
      case 'a':
        if ((e.ctrlKey || e.metaKey) && selection === 'multiple') {
          e.preventDefault()
          choose(new Set(ids), ids[0] ?? '')
        }
    }
  }

  return {
    scroller,
    items,
    reverse,
    measure: virtual.measureElement,
    unseen,
    toBottom,
    total: virtual.getTotalSize(),
    offset,
    ids,
    loaded,
    selected,
    focus,
    onKeyDown,
    rowProps: (i) => ({
      onClick: (e: MouseEvent) => select(i, { toggle: e.ctrlKey || e.metaKey, range: e.shiftKey }),
      onDoubleClick: () => {
        const id = idAt(i)
        if (id !== undefined) emit.current('open', { id })
      },
      'aria-selected': selected.has(idAt(i) ?? '\0'),
      'data-row': i,
      'data-id': idAt(i),
    }),
  }
}

function Frame({ w, height, width, children, label, header }: { w: Windowed; height?: number; width?: number; children: ReactNode; label: string; header?: ReactNode }) {
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: height ?? '100%', width: width ?? '100%', minHeight: 120, flexShrink: 0, bgcolor: 'background.paper', color: 'text.primary' }}>
      {header}
      <Box ref={w.scroller} tabIndex={0} role="listbox" aria-label={label} aria-multiselectable onKeyDown={w.onKeyDown} data-virtual-scroller
        sx={{ flex: 1, overflow: 'auto', position: 'relative', outline: 'none', contain: 'strict', '&:focus-visible': { boxShadow: (t) => `inset 0 0 0 2px ${t.palette.primary.main}` } }}>
        <Box sx={{ height: w.total, position: 'relative', width: '100%' }}>{children}</Box>
      </Box>
      {w.unseen && (
        <Box sx={{ position: 'relative', height: 0 }}>
          <Button size="small" variant="contained" onClick={w.toBottom} sx={{ position: 'absolute', bottom: 8, left: '50%', transform: 'translateX(-50%)', borderRadius: 4 }}>
            New messages
          </Button>
        </Box>
      )}
    </Box>
  )
}

// A row's look is a few fixed classes; its place is an inline style, so
// scrolling makes no new CSS (a transform in sx would, per row per frame)
const rowSx = {
  position: 'absolute', top: 0, left: 0, width: '100%', display: 'flex', alignItems: 'center', overflow: 'hidden', cursor: 'pointer',
  borderBottom: 1, borderColor: 'divider', outlineOffset: -1,
  '&:hover': { bgcolor: 'action.hover' },
  '&[aria-selected="true"]': { bgcolor: 'action.selected' },
  '&[data-focused="true"]': { outline: '1px dotted' },
} as const

function rowStyle(start: number, size: number, measured = false) {
  return measured ? { transform: `translateY(${start}px)` } : { height: size, transform: `translateY(${start}px)` }
}

const placeholder = (size: number) => <Skeleton variant="text" sx={{ mx: 1.5, width: '60%', height: size * 0.5 }} />

export function MuiVirtualList(p: RendererProps) {
  const rowHeight = num(p.props.rowHeight, 48)
  const reverse = p.props.reverse === true
  const w = useWindowed(p, rowHeight, reverse)
  const count = num(p.props.count, 0)
  if (p.props.count === undefined) return <Typography variant="body2" color="error">mui.virtualList is drawn only inside a prefab</Typography>
  return (
    <Frame w={w} height={p.props.height as number | undefined} width={p.props.width as number | undefined} label="list">
      {count === 0 && !p.props.loading && <Typography variant="body2" color="text.secondary" sx={{ p: 2 }}>Nothing here</Typography>}
      {w.items.map((v) => {
        return (
          <Box key={v.key} ref={reverse ? w.measure : undefined} data-index={v.display} role="option" {...w.rowProps(v.index)} data-focused={w.focus === v.index}
            sx={reverse ? { ...rowSx, alignItems: 'stretch', borderBottom: 0, cursor: 'default', minHeight: rowHeight } : rowSx} style={rowStyle(v.start, v.size, reverse)}>
            <Box sx={{ flex: 1, minWidth: 0, px: 1 }}>{w.loaded(v.index) ? p.children[v.index - w.offset] : placeholder(reverse ? rowHeight : v.size)}</Box>
          </Box>
        )
      })}
    </Frame>
  )
}

export function MuiVirtualGrid(p: RendererProps) {
  const rowHeight = num(p.props.rowHeight, 36)
  const w = useWindowed(p, rowHeight)
  const columns = (p.props.columns as Column[]) ?? []
  const rows = (p.props.rows as Record<string, unknown>[]) ?? []
  const sort = ((p.props.sort as { field: string; desc?: boolean }[]) ?? [])[0]
  const template = columns.map((c) => (c.width ? `${c.width}px` : 'minmax(80px, 1fr)')).join(' ')
  if (p.props.count === undefined) return <Typography variant="body2" color="error">mui.virtualGrid is drawn only inside a prefab</Typography>
  const header = (
    <Box role="row" sx={{ display: 'grid', gridTemplateColumns: template, borderBottom: 2, borderColor: 'divider', typography: 'subtitle2', py: 0.75, pr: 1.5 }}>
      {columns.map((c) => {
        const field = c.sortField ?? c.field
        const active = sort?.field === field
        return (
          <Box key={c.field} role="columnheader" sx={{ px: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {c.sortable ? (
              <TableSortLabel active={active} direction={active && sort?.desc ? 'desc' : 'asc'} onClick={() => p.emit('sort', { field, desc: active ? !sort?.desc : false })}>
                {c.title ?? c.field}
              </TableSortLabel>
            ) : (c.title ?? c.field)}
          </Box>
        )
      })}
    </Box>
  )
  return (
    <Frame w={w} height={p.props.height as number | undefined} width={p.props.width as number | undefined} label="grid" header={header}>
      {w.items.map((v) => {
        const row = w.loaded(v.index) ? rows[v.index - w.offset] : undefined
        const id = str(row?._id)
        return (
          <Box key={v.index} role="option" {...w.rowProps(v.index)} data-focused={w.focus === v.index} sx={rowSx} style={{ ...rowStyle(v.start, v.size), display: 'grid', gridTemplateColumns: template }}>
            {row
              ? columns.map((c) => (
                  <Box key={c.field} role="cell" sx={{ px: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', typography: 'body2' }}>
                    <TableCellValue column={c} row={row} rowKey={id} emit={p.emit} />
                  </Box>
                ))
              : placeholder(v.size)}
          </Box>
        )
      })}
    </Frame>
  )
}
