// Block mode's chrome (forge 35c-2): the slash menu, a drag handle beside
// the block under the pointer, and keys that move blocks. Every move is one
// transaction (blockOps.moveBlocks), so one Yjs transaction and one undo
// step, whether it moves a block or a selection of blocks.

import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import type { Editor } from '@tiptap/react'
import { TextSelection } from '@tiptap/pm/state'
import { Box, IconButton, List, ListItemButton, ListItemIcon, ListItemText, Paper, Tooltip, Typography } from '@mui/material'
import DragIndicatorIcon from '@mui/icons-material/DragIndicator'
import AddIcon from '@mui/icons-material/Add'
import { NamedIcon } from '../../ui/renderers/mui/icons.tsx'
import { filterItems, moveBlocks, posOfBlock, selectedBlocks, slashQuery, type SlashItem } from './blockOps.ts'

export interface BlockCommand {
  id: string
  block: number // the top-level block the menu was opened in
}

interface Query {
  query: string
  from: number
  to: number
}

// Inserts an item where the slash menu was opened, replacing the typed /query
export function applySlashItem(editor: Editor, item: SlashItem, q: Query, onCommand?: (c: BlockCommand) => void) {
  const block = editor.state.doc.resolve(q.from).index(0)
  const chain = editor.chain().focus().deleteRange({ from: q.from, to: q.to })
  if (item.command || !item.content) {
    chain.run()
    if (item.command) onCommand?.({ id: item.id, block })
    return
  }
  const single = Array.isArray(item.content) ? undefined : item.content
  const type = single && editor.schema.nodes[String(single.type)]
  if (single && type?.isTextblock && !single.content) {
    chain.setNode(type.name, (single.attrs as Record<string, unknown>) ?? {}).run()
    return
  }
  chain.insertContent(item.content).run()
  // The cursor goes into the block it made, when it can hold one
  if (single) {
    const { state } = editor
    const at = Math.min(block, state.doc.childCount - 1)
    const node = state.doc.child(at)
    if (node.type.name === single.type && !node.isAtom) {
      const pos = posOfBlock(state, at) + 1
      editor.view.dispatch(state.tr.setSelection(TextSelection.near(state.doc.resolve(pos))).scrollIntoView())
    }
  }
}

// The top-level index of the block a DOM element belongs to, if any
function blockIndexOf(editor: Editor, el: Element | null): number | undefined {
  const root = editor.view.dom
  let node: Element | null = el
  while (node && node.parentElement !== root) node = node.parentElement
  if (!node || node.parentElement !== root) return undefined
  try {
    const pos = editor.view.posAtDOM(node, 0)
    return editor.state.doc.resolve(Math.min(pos, editor.state.doc.content.size)).index(0)
  } catch {
    return undefined
  }
}

function blockRect(editor: Editor, i: number): DOMRect | undefined {
  const dom = editor.view.nodeDOM(posOfBlock(editor.state, i))
  return dom instanceof HTMLElement ? dom.getBoundingClientRect() : undefined
}

// Moves the selected blocks up or down one place
export function moveSelected(editor: Editor, dir: -1 | 1): boolean {
  const { from, to } = selectedBlocks(editor.state)
  const target = dir < 0 ? from - 1 : to + 1
  if (target < 0 || target > editor.state.doc.childCount) return false
  const tr = moveBlocks(editor.state, from, to, target)
  if (!tr) return false
  // Keeps the selection on the moved blocks
  const shift = dir < 0 ? -editor.state.doc.child(from - 1).nodeSize : editor.state.doc.child(to).nodeSize
  const sel = editor.state.selection
  try {
    tr.setSelection(TextSelection.create(tr.doc, sel.from + shift, sel.to + shift))
  } catch {
    // a selection that cannot be kept stays where the transaction maps it
  }
  editor.view.dispatch(tr)
  return true
}

export function BlockChrome({ editor, items, locked, onCommand, children }: {
  editor: Editor
  items: SlashItem[]
  locked: boolean
  onCommand?(c: BlockCommand): void
  children: ReactNode
}) {
  const wrap = useRef<HTMLDivElement>(null)
  const [query, setQuery] = useState<Query>()
  const [dismissed, setDismissed] = useState<number>()
  const [active, setActive] = useState(0)
  const [hover, setHover] = useState<{ index: number; top: number; height: number }>()
  const drag = useRef<{ from: number; to: number }>(undefined)
  const [drop, setDrop] = useState<{ target: number; top: number }>()

  // The slash menu follows every transaction
  useEffect(() => {
    const update = () => {
      const q = editor.isEditable ? slashQuery(editor.state) : null
      setQuery((old) => (q && old && q.from === old.from && q.query === old.query ? old : q ?? undefined))
    }
    update()
    editor.on('transaction', update)
    return () => {
      editor.off('transaction', update)
    }
  }, [editor])
  const shown = useMemo(() => (query ? filterItems(items, query.query) : []), [items, query])
  const open = !!query && query.from !== dismissed && shown.length > 0 && !locked
  useEffect(() => setActive(0), [query?.query, query?.from])
  useEffect(() => {
    if (query?.from !== dismissed) setDismissed(undefined)
  }, [query?.from, dismissed])

  const choose = (item: SlashItem) => {
    if (!query) return
    applySlashItem(editor, item, query, onCommand)
    setQuery(undefined)
  }

  // Keys reach the menu before the editor
  const onKeyDownCapture = (e: React.KeyboardEvent) => {
    if (open && query) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        setActive((a) => (a + (e.key === 'ArrowDown' ? 1 : shown.length - 1)) % shown.length)
      } else if (e.key === 'Enter' || e.key === 'Tab') {
        choose(shown[Math.min(active, shown.length - 1)])
      } else if (e.key === 'Escape') {
        setDismissed(query.from)
      } else return
      e.preventDefault()
      e.stopPropagation()
      return
    }
    if (!locked && (e.ctrlKey || e.metaKey) && e.shiftKey && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) {
      if (moveSelected(editor, e.key === 'ArrowUp' ? -1 : 1)) {
        e.preventDefault()
        e.stopPropagation()
      }
    }
  }

  const onMouseMove = (e: React.MouseEvent) => {
    if (drag.current || locked) return
    const box = wrap.current?.getBoundingClientRect()
    if (!box) return
    // The block level with the pointer, even over the gutter left of the text
    const x = Math.max(e.clientX, editor.view.dom.getBoundingClientRect().left + 8)
    const el = document.elementFromPoint(x, e.clientY)
    const index = el && editor.view.dom.contains(el) ? blockIndexOf(editor, el) : undefined
    if (index === undefined) return
    const r = blockRect(editor, index)
    if (r) setHover({ index, top: r.top - box.top, height: r.height })
  }

  const targetAt = (y: number): number => {
    const n = editor.state.doc.childCount
    for (let i = 0; i < n; i++) {
      const r = blockRect(editor, i)
      if (r && y <= r.top + r.height / 2) return i
    }
    return n
  }

  const onDragStart = (e: React.DragEvent) => {
    if (!hover) return
    const sel = selectedBlocks(editor.state)
    const multi = !editor.state.selection.empty && hover.index >= sel.from && hover.index < sel.to
    drag.current = multi ? sel : { from: hover.index, to: hover.index + 1 }
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('application/x-foundation-blocks', JSON.stringify(drag.current))
    const dom = editor.view.nodeDOM(posOfBlock(editor.state, hover.index))
    if (dom instanceof HTMLElement) e.dataTransfer.setDragImage(dom, 0, 0)
  }
  const onDragOverCapture = (e: React.DragEvent) => {
    if (!drag.current) return
    e.preventDefault()
    e.stopPropagation()
    e.dataTransfer.dropEffect = 'move'
    const box = wrap.current?.getBoundingClientRect()
    if (!box) return
    const target = targetAt(e.clientY)
    const n = editor.state.doc.childCount
    const r = target < n ? blockRect(editor, target) : blockRect(editor, n - 1)
    if (r) setDrop({ target, top: (target < n ? r.top : r.bottom) - box.top })
  }
  const endDrag = () => {
    drag.current = undefined
    setDrop(undefined)
  }
  const onDropCapture = (e: React.DragEvent) => {
    const range = drag.current
    if (!range) return
    e.preventDefault()
    e.stopPropagation()
    const tr = moveBlocks(editor.state, range.from, range.to, targetAt(e.clientY))
    if (tr) editor.view.dispatch(tr)
    endDrag()
    editor.commands.focus()
  }

  // + adds an empty block below with the slash menu open
  const addBelow = () => {
    if (!hover) return
    const at = posOfBlock(editor.state, hover.index + 1)
    const para = editor.schema.nodes.paragraph.create(null, editor.schema.text('/'))
    const tr = editor.state.tr.insert(at, para)
    tr.setSelection(TextSelection.create(tr.doc, at + 2))
    editor.view.dispatch(tr.scrollIntoView())
    editor.commands.focus()
  }

  const coords = open && query ? safeCoords(editor, query.from) : undefined

  return (
    <Box ref={wrap} sx={{ position: 'relative' }} onKeyDownCapture={onKeyDownCapture} onMouseMove={onMouseMove}
      onMouseLeave={() => !drag.current && setHover(undefined)} onDragOverCapture={onDragOverCapture} onDropCapture={onDropCapture}
      data-block-chrome>
      {children}
      {hover && !locked && (
        <Box sx={{ position: 'absolute', left: -56, top: hover.top, height: Math.min(hover.height, 28), display: 'flex', alignItems: 'center', zIndex: 1 }}
          contentEditable={false}>
          <Tooltip title="Add a block below">
            <IconButton size="small" aria-label="Add a block below" onMouseDown={(e) => e.preventDefault()} onClick={addBelow} sx={{ p: 0.25, color: 'text.disabled' }}>
              <AddIcon fontSize="small" />
            </IconButton>
          </Tooltip>
          <Tooltip title="Drag to move (Ctrl+Shift+↑/↓)">
            <Box component="span" draggable role="button" aria-label="Drag to move the block" data-block-handle={hover.index}
              onDragStart={onDragStart} onDragEnd={endDrag}
              sx={{ cursor: 'grab', color: 'text.disabled', display: 'flex', '&:hover': { color: 'text.secondary' } }}>
              <DragIndicatorIcon fontSize="small" />
            </Box>
          </Tooltip>
        </Box>
      )}
      {drop && <Box sx={{ position: 'absolute', left: 0, right: 0, top: drop.top - 1, height: 3, bgcolor: 'primary.main', borderRadius: 1, pointerEvents: 'none' }} data-block-drop />}
      {open && coords && (
        <Paper elevation={6} sx={{ position: 'fixed', left: coords.left, top: coords.bottom + 4, zIndex: 1400, width: 280, maxHeight: 320, overflow: 'auto' }}
          onMouseDown={(e) => e.preventDefault()} role="listbox" aria-label="Insert a block" data-slash-menu>
          <List dense disablePadding>
            {shown.map((item, i) => (
              <ListItemButton key={item.id} selected={i === active} onClick={() => choose(item)} onMouseEnter={() => setActive(i)} role="option" aria-selected={i === active}
                data-slash-item={item.id}>
                <ListItemIcon sx={{ minWidth: 32 }}>
                  <NamedIcon name={item.icon ?? 'add'} fontSize="small" />
                </ListItemIcon>
                <ListItemText primary={item.title} secondary={item.description} slotProps={{ secondary: { noWrap: true } }} />
              </ListItemButton>
            ))}
          </List>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', px: 2, py: 0.5, borderTop: 1, borderColor: 'divider' }}>
            ↑↓ to choose, Enter to insert, Esc to close
          </Typography>
        </Paper>
      )}
    </Box>
  )
}

// Block mode's nodes, in every mode: a toggle with its chevron (shown, and
// closing it, in block mode only), a callout's box, columns side by side
// and the cards that link out
export const blockCss = {
  '& .rt-toggle': { position: 'relative', my: 0.5 },
  '& .rt-toggle-chevron': { display: 'none' },
  '& .rt-toggle[data-level] > .rt-toggle-content > .rt-toggle-title': { fontWeight: 600, lineHeight: 1.3 },
  '& .rt-toggle[data-level="1"] > .rt-toggle-content > .rt-toggle-title': { fontSize: '2em' },
  '& .rt-toggle[data-level="2"] > .rt-toggle-content > .rt-toggle-title': { fontSize: '1.5em' },
  '& .rt-toggle[data-level="3"] > .rt-toggle-content > .rt-toggle-title': { fontSize: '1.17em' },
  '& .rt-toggle-title': { minHeight: '1.6em' },
  '& .rt-callout': { display: 'flex', gap: 1.5, p: 1.5, my: 1, borderRadius: 1, bgcolor: 'action.hover', borderLeft: 4, borderColor: 'info.main' },
  '& .rt-callout[data-color="success"]': { borderColor: 'success.main' },
  '& .rt-callout[data-color="warning"]': { borderColor: 'warning.main' },
  '& .rt-callout[data-color="error"]': { borderColor: 'error.main' },
  '& .rt-callout[data-color="neutral"]': { borderColor: 'divider' },
  '& .rt-callout-icon': { border: 0, bgcolor: 'transparent', fontSize: 20, lineHeight: 1.4, p: 0, cursor: 'pointer', alignSelf: 'flex-start' },
  '& .rt-callout-content': { flex: 1, minWidth: 0, '& > *:first-of-type': { mt: 0 }, '& > *:last-child': { mb: 0 } },
  '& .rt-columns': { display: 'flex', gap: 3, my: 1 },
  '& .rt-column': { flex: 1, minWidth: 0, '& > *:first-of-type': { mt: 0 } },
  '& .rt-card': {
    display: 'flex', alignItems: 'center', gap: 1, px: 1.5, py: 1, my: 0.5, border: 1, borderColor: 'divider', borderRadius: 1, cursor: 'pointer',
    '&:hover': { bgcolor: 'action.hover' }, '&.ProseMirror-selectednode': { outline: 2, outlineColor: 'primary.main' },
  },
  '& .rt-pageLink .rt-card-text': { textDecoration: 'underline', textDecorationColor: 'rgba(127,127,127,.5)', fontWeight: 500 },
  '& .rt-card-text': { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  '&[data-block-mode] .rt-toggle-chevron': {
    display: 'block', position: 'absolute', left: -26, top: 2, width: 22, height: 22, border: 0, bgcolor: 'transparent', color: 'text.secondary', cursor: 'pointer',
    p: 0, borderRadius: 0.5, '&:hover': { bgcolor: 'action.hover' },
    '&::before': {
      content: '""', display: 'inline-block', width: 0, height: 0, borderTop: '5px solid transparent', borderBottom: '5px solid transparent',
      borderLeft: '7px solid currentColor', transition: 'transform .15s', verticalAlign: 'middle',
    },
  },
  '&[data-block-mode] .rt-toggle[data-open="true"] > .rt-toggle-chevron::before': { transform: 'rotate(90deg)' },
  '&[data-block-mode] .rt-toggle[data-level] > .rt-toggle-chevron': { top: '0.35em' },
  '&[data-block-mode] .rt-toggle[data-open="false"] > .rt-toggle-content > :not(.rt-toggle-title)': { display: 'none' },
  '&[data-block-mode] .rt-toggle > .rt-toggle-content > :not(.rt-toggle-title)': { pl: 0 },
  '&[data-block-mode] .ProseMirror > *': { position: 'relative' },
} as const

function safeCoords(editor: Editor, pos: number) {
  try {
    return editor.view.coordsAtPos(pos)
  } catch {
    return undefined
  }
}
