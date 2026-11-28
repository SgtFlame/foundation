// Page view's controls (forge 35b-2): the room's page setup as React state,
// the Page setup dialog, the headers and footers (each a small document in
// the room, edited together), the ruler that sets the selected paragraphs'
// indents, and printing, which copies the page view into a frame so the
// browser's print and Save as PDF match the screen.

import { useEffect, useMemo, useRef, useState, useSyncExternalStore, type PointerEvent as ReactPointerEvent } from 'react'
import { EditorContent, useEditor, type Editor } from '@tiptap/react'
import Collaboration from '@tiptap/extension-collaboration'
import {
  Box, Button, Checkbox, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, IconButton, MenuItem, Stack, Tab, Tabs, TextField,
  Tooltip, Typography,
} from '@mui/material'
import FormatBoldIcon from '@mui/icons-material/FormatBold'
import FormatItalicIcon from '@mui/icons-material/FormatItalic'
import FormatAlignLeftIcon from '@mui/icons-material/FormatAlignLeft'
import FormatAlignCenterIcon from '@mui/icons-material/FormatAlignCenter'
import FormatAlignRightIcon from '@mui/icons-material/FormatAlignRight'
import type * as Y from 'yjs'
import { baseExtensions } from './extensions.ts'
import { headerFooterOf, pageMapOf, pageSetupOf, setPageSetup, type HeaderFooter } from './content.ts'
import { clampIndents, pageBox, pageSizes, pxPerPt, setupOf, type PageSetup, type PageSize } from './pages.ts'
import { indentsAt } from './pageNodes.ts'

// The room's page setup, following everyone's changes
export function usePageSetup(doc: Y.Doc): PageSetup {
  const cache = useRef<{ key: string; value: PageSetup }>(undefined)
  const subscribe = useMemo(() => (listener: () => void) => {
    const map = pageMapOf(doc)
    map.observe(listener)
    return () => map.unobserve(listener)
  }, [doc])
  const snapshot = () => {
    const value = pageSetupOf(doc)
    const key = JSON.stringify(value)
    if (cache.current?.key !== key) cache.current = { key, value }
    return cache.current.value
  }
  return useSyncExternalStore(subscribe, snapshot)
}

const inches = (pt: number) => Math.round((pt / 72) * 100) / 100

export function PageSetupDialog({ doc, open, onClose }: { doc: Y.Doc; open: boolean; onClose(): void }) {
  const current = usePageSetup(doc)
  const [draft, setDraft] = useState(current)
  useEffect(() => {
    if (open) setDraft(current)
  }, [open, current])
  const set = (patch: Partial<PageSetup>) => setDraft((d) => setupOf({ ...d, ...patch }))
  const margin = (side: keyof PageSetup['margins']) => (
    <TextField size="small" type="number" label={side[0].toUpperCase() + side.slice(1)} value={inches(draft.margins[side])}
      slotProps={{ htmlInput: { step: 0.1, min: 0, 'aria-label': `${side} margin (inches)` } }}
      onChange={(e) => set({ margins: { ...draft.margins, [side]: Number(e.target.value) * 72 } })} />
  )
  return (
    <Dialog open={open} onClose={onClose} maxWidth="xs" fullWidth>
      <DialogTitle>Page setup</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ pt: 1 }}>
          <FormControlLabel label="Pageless (no pages, headers or footers)"
            control={<Checkbox checked={draft.pageless} onChange={(_, c) => set({ pageless: c })} />} />
          <TextField select size="small" label="Paper size" value={draft.size}
            onChange={(e) => set({ size: e.target.value as PageSize })}>
            {Object.entries(pageSizes).map(([id, s]) => <MenuItem key={id} value={id}>{s.name}</MenuItem>)}
            <MenuItem value="custom">Custom</MenuItem>
          </TextField>
          {draft.size === 'custom' && (
            <Stack direction="row" spacing={1}>
              <TextField size="small" type="number" label="Width (in)" value={inches(draft.width)}
                onChange={(e) => set({ width: Number(e.target.value) * 72 })} slotProps={{ htmlInput: { step: 0.1 } }} />
              <TextField size="small" type="number" label="Height (in)" value={inches(draft.height)}
                onChange={(e) => set({ height: Number(e.target.value) * 72 })} slotProps={{ htmlInput: { step: 0.1 } }} />
            </Stack>
          )}
          <TextField select size="small" label="Orientation" value={draft.landscape ? 'landscape' : 'portrait'}
            onChange={(e) => set({ landscape: e.target.value === 'landscape' })}>
            <MenuItem value="portrait">Portrait</MenuItem>
            <MenuItem value="landscape">Landscape</MenuItem>
          </TextField>
          <Typography variant="caption" color="text.secondary">Margins (inches)</Typography>
          <Stack direction="row" spacing={1}>{margin('top')}{margin('bottom')}</Stack>
          <Stack direction="row" spacing={1}>{margin('left')}{margin('right')}</Stack>
          <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
            <TextField size="small" type="color" label="Page colour" value={draft.color || '#ffffff'} sx={{ width: 120 }}
              onChange={(e) => set({ color: e.target.value })} />
            <Button size="small" onClick={() => set({ color: '' })} disabled={!draft.color}>Theme paper</Button>
          </Stack>
          <FormControlLabel label="Different first page header and footer"
            control={<Checkbox checked={draft.differentFirst} onChange={(_, c) => set({ differentFirst: c })} />} />
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="contained" onClick={() => (setPageSetup(doc, draft), onClose())}>Apply</Button>
      </DialogActions>
    </Dialog>
  )
}

function MiniButton({ title, active, onClick, children }: { title: string; active?: boolean; onClick(): void; children: React.ReactNode }) {
  return (
    <Tooltip title={title}>
      <IconButton size="small" aria-label={title} aria-pressed={active} color={active ? 'primary' : 'default'}
        onMouseDown={(e) => e.preventDefault()} onClick={onClick}>{children}</IconButton>
    </Tooltip>
  )
}

// One header or footer, edited together in its own fragment of the room
function HeaderFooterEditor({ doc, name, readOnly }: { doc: Y.Doc; name: HeaderFooter; readOnly: boolean }) {
  const editor = useEditor({
    extensions: [...baseExtensions(), Collaboration.configure({ fragment: headerFooterOf(doc, name) })],
    editable: !readOnly,
    editorProps: { attributes: { 'aria-label': name, 'data-header-footer': name } },
  }, [doc, name, readOnly])
  const [, tick] = useState(0)
  useEffect(() => {
    if (!editor) return
    const bump = () => tick((t) => t + 1)
    editor.on('transaction', bump)
    return () => {
      editor.off('transaction', bump)
    }
  }, [editor])
  if (!editor) return null
  const chain = () => editor.chain().focus()
  return (
    <Box>
      {!readOnly && (
        <Stack direction="row" sx={{ alignItems: 'center', mb: 0.5 }}>
          <MiniButton title="Bold" active={editor.isActive('bold')} onClick={() => chain().toggleBold().run()}><FormatBoldIcon fontSize="small" /></MiniButton>
          <MiniButton title="Italic" active={editor.isActive('italic')} onClick={() => chain().toggleItalic().run()}><FormatItalicIcon fontSize="small" /></MiniButton>
          <MiniButton title="Align left" active={editor.isActive({ textAlign: 'left' })} onClick={() => chain().setTextAlign('left').run()}><FormatAlignLeftIcon fontSize="small" /></MiniButton>
          <MiniButton title="Align centre" active={editor.isActive({ textAlign: 'center' })} onClick={() => chain().setTextAlign('center').run()}><FormatAlignCenterIcon fontSize="small" /></MiniButton>
          <MiniButton title="Align right" active={editor.isActive({ textAlign: 'right' })} onClick={() => chain().setTextAlign('right').run()}><FormatAlignRightIcon fontSize="small" /></MiniButton>
          <Button size="small" onMouseDown={(e) => e.preventDefault()} onClick={() => chain().insertPageNumber('page').run()}>Page number</Button>
          <Button size="small" onMouseDown={(e) => e.preventDefault()} onClick={() => chain().insertPageNumber('pages').run()}>Page count</Button>
        </Stack>
      )}
      <Box sx={{
        border: 1, borderColor: 'divider', borderRadius: 1, px: 1.5, py: 0.5, minHeight: 48,
        '& .ProseMirror': { outline: 'none', minHeight: 36 }, '& .ProseMirror p': { my: 0.5 },
        '& .rt-page-number': { bgcolor: 'action.selected', borderRadius: 0.5, px: 0.5 },
      }}>
        <EditorContent editor={editor} />
      </Box>
    </Box>
  )
}

export function HeaderFooterDialog({ doc, open, readOnly, start, onClose }: { doc: Y.Doc; open: boolean; readOnly: boolean; start?: HeaderFooter; onClose(): void }) {
  const setup = usePageSetup(doc)
  const [tab, setTab] = useState<'all' | 'first'>('all')
  useEffect(() => {
    if (open) setTab(start === 'firstHeader' || start === 'firstFooter' ? 'first' : 'all')
  }, [open, start])
  const first = tab === 'first' && setup.differentFirst
  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>Headers and footers</DialogTitle>
      <DialogContent>
        {setup.differentFirst && (
          <Tabs value={tab} onChange={(_, v) => setTab(v)} sx={{ mb: 1 }}>
            <Tab value="all" label="Every page" />
            <Tab value="first" label="First page" />
          </Tabs>
        )}
        <Stack spacing={2}>
          <Typography variant="overline">Header</Typography>
          <HeaderFooterEditor key={`h-${first}`} doc={doc} name={first ? 'firstHeader' : 'header'} readOnly={readOnly} />
          <Typography variant="overline">Footer</Typography>
          <HeaderFooterEditor key={`f-${first}`} doc={doc} name={first ? 'firstFooter' : 'footer'} readOnly={readOnly} />
          <Typography variant="caption" color="text.secondary">
            Everyone with the document open edits these together. Page numbers show as # here and as the page's number on the page.
          </Typography>
        </Stack>
      </DialogContent>
      <DialogActions><Button onClick={onClose}>Done</Button></DialogActions>
    </Dialog>
  )
}

// The ruler above the page: the margins shaded, and the selected
// paragraphs' left, first-line and right indents as handles to drag
export function Ruler({ editor, setup, disabled }: { editor: Editor; setup: PageSetup; disabled: boolean }) {
  const [, tick] = useState(0)
  useEffect(() => {
    const bump = () => tick((t) => t + 1)
    editor.on('selectionUpdate', bump)
    editor.on('update', bump)
    return () => {
      editor.off('selectionUpdate', bump)
      editor.off('update', bump)
    }
  }, [editor])
  const box = pageBox(setup)
  const columnPt = box.contentWidth / pxPerPt
  const indents = indentsAt(editor)
  const [drag, setDrag] = useState<{ which: 'left' | 'first' | 'right'; startX: number; start: typeof indents; now: typeof indents }>()
  const shown = drag?.now ?? indents
  const m = box.margins
  const inchPx = 96
  const ticks: number[] = []
  for (let x = 0; x <= box.width; x += inchPx / 4) ticks.push(x)

  const down = (which: 'left' | 'first' | 'right') => (e: ReactPointerEvent) => {
    if (disabled) return
    e.preventDefault()
    ;(e.target as HTMLElement).setPointerCapture(e.pointerId)
    setDrag({ which, startX: e.clientX, start: indents, now: indents })
  }
  const move = (e: ReactPointerEvent) => {
    if (!drag) return
    const dx = (e.clientX - drag.startX) / pxPerPt
    const s = drag.start
    const step = (v: number) => Math.round(v / 9) * 9 // an eighth of an inch
    // The left indent moves the paragraph and leaves its first line where it was
    const left = step(s.left + dx)
    const next = drag.which === 'left' ? { ...s, left, first: s.first - (left - s.left) }
      : drag.which === 'first' ? { ...s, first: step(s.first + dx) } : { ...s, right: step(s.right - dx) }
    setDrag({ ...drag, now: clampIndents(next, columnPt) })
  }
  const up = () => {
    if (!drag) return
    const { left, right, first } = drag.now
    setDrag(undefined)
    editor.chain().focus().setIndents({ left, right, first }).run()
  }
  const handle = (which: 'left' | 'first' | 'right', x: number, top: boolean, title: string) => (
    <Tooltip title={title} key={which}>
      <Box role="slider" aria-label={title} aria-valuenow={Math.round(which === 'left' ? shown.left : which === 'first' ? shown.first : shown.right)}
        onPointerDown={down(which)} onPointerMove={move} onPointerUp={up}
        sx={{
          position: 'absolute', left: x - 6, top: top ? 0 : 10, width: 12, height: 10, cursor: disabled ? 'default' : 'ew-resize', touchAction: 'none',
          bgcolor: 'primary.main', clipPath: top ? 'polygon(0 0, 100% 0, 50% 100%)' : 'polygon(50% 0, 100% 100%, 0 100%)',
        }} />
    </Tooltip>
  )
  return (
    <Box aria-label="Ruler" data-ruler sx={{ position: 'relative', width: box.width, height: 20, mx: 'auto', bgcolor: 'background.paper', borderBottom: 1, borderColor: 'divider', userSelect: 'none', flexShrink: 0 }}>
      <Box sx={{ position: 'absolute', left: 0, width: m.left, top: 0, bottom: 0, bgcolor: 'action.hover' }} />
      <Box sx={{ position: 'absolute', right: 0, width: m.right, top: 0, bottom: 0, bgcolor: 'action.hover' }} />
      {ticks.map((x) => (
        <Box key={x} sx={{ position: 'absolute', left: x, bottom: 0, width: '1px', height: x % inchPx === 0 ? 8 : 4, bgcolor: 'text.disabled' }} />
      ))}
      {handle('first', m.left + (shown.left + shown.first) * pxPerPt, true, 'First line indent')}
      {handle('left', m.left + shown.left * pxPerPt, false, 'Left indent')}
      {handle('right', box.width - m.right - shown.right * pxPerPt, false, 'Right indent')}
    </Box>
  )
}

// Every rule of the page's style sheets, also those emotion inserted
// through the CSS object model (which leave their style tags empty)
function styleText(): string {
  const out: string[] = []
  for (const sheet of Array.from(document.styleSheets)) {
    try {
      for (const rule of Array.from(sheet.cssRules)) out.push(rule.cssText)
    } catch {
      // a sheet from another origin: its link is copied instead
    }
  }
  return out.join('\n')
}

// Prints the page view: the page box in a frame of its own, sized to the
// paper, with the bands between pages removed and each page's header
// starting a printed page
export function printDocument(page: HTMLElement, setup: PageSetup, title: string) {
  const box = pageBox(setup)
  const frame = document.createElement('iframe')
  frame.setAttribute('aria-hidden', 'true')
  frame.style.cssText = 'position: fixed; right: 0; bottom: 0; width: 0; height: 0; border: 0'
  document.body.append(frame)
  const w = frame.contentWindow
  const d = frame.contentDocument
  if (!w || !d) return frame.remove()
  d.open()
  d.write('<!doctype html><html><head><meta charset="utf-8"></head><body></body></html>')
  d.close()
  d.title = title
  for (const link of Array.from(document.querySelectorAll('link[rel="stylesheet"]'))) d.head.append(link.cloneNode(true))
  const style = d.createElement('style')
  style.textContent = `${styleText()}
@page { size: ${box.width}px ${box.height}px; margin: 0 }
html, body { margin: 0; padding: 0; background: none }
* { -webkit-print-color-adjust: exact; print-color-adjust: exact }`
  d.head.append(style)
  const copy = page.cloneNode(true) as HTMLElement
  copy.setAttribute('data-printing', '')
  d.body.className = document.body.className
  d.body.append(copy)
  const done = () => setTimeout(() => frame.remove(), 1000)
  w.addEventListener('afterprint', done)
  setTimeout(() => {
    w.focus()
    w.print()
    done()
  }, 300)
}
