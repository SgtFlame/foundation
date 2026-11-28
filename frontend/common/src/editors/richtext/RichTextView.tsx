// The rich-text editor (forge 35b-1): a TipTap editor bound to a document
// room's Y.XmlFragment, everyone's cursor and selection in their presence
// colour, with a toolbar, find and replace, the outline and a word count.
// The workbench's document editor and the doc.richText primitive both
// draw it over a CollabSession made with richTextContent. In page mode
// (forge 35b-2) it lays the document out on pages of the room's page setup
// (pagination.ts), with a ruler, headers and footers, footnotes at the foot
// of their page, page and section breaks, a table of contents, and print.

import { useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from 'react'
import { EditorContent, useEditor, type Editor } from '@tiptap/react'
import Collaboration from '@tiptap/extension-collaboration'
import CollaborationCursor from '@tiptap/extension-collaboration-cursor'
import {
  Box, Button, Checkbox, Divider, FormControlLabel, IconButton, InputBase, List, ListItemButton, ListItemText, MenuItem, Popover, Select,
  Stack, TextField, Tooltip, Typography, useTheme,
} from '@mui/material'
import FormatBoldIcon from '@mui/icons-material/FormatBold'
import FormatItalicIcon from '@mui/icons-material/FormatItalic'
import FormatUnderlinedIcon from '@mui/icons-material/FormatUnderlined'
import StrikethroughSIcon from '@mui/icons-material/StrikethroughS'
import SuperscriptIcon from '@mui/icons-material/Superscript'
import SubscriptIcon from '@mui/icons-material/Subscript'
import FormatColorTextIcon from '@mui/icons-material/FormatColorText'
import BorderColorIcon from '@mui/icons-material/BorderColor'
import FormatAlignLeftIcon from '@mui/icons-material/FormatAlignLeft'
import FormatAlignCenterIcon from '@mui/icons-material/FormatAlignCenter'
import FormatAlignRightIcon from '@mui/icons-material/FormatAlignRight'
import FormatAlignJustifyIcon from '@mui/icons-material/FormatAlignJustify'
import FormatListBulletedIcon from '@mui/icons-material/FormatListBulleted'
import FormatListNumberedIcon from '@mui/icons-material/FormatListNumbered'
import ChecklistIcon from '@mui/icons-material/Checklist'
import LinkIcon from '@mui/icons-material/Link'
import ImageIcon from '@mui/icons-material/Image'
import TableChartIcon from '@mui/icons-material/TableChart'
import DataObjectIcon from '@mui/icons-material/DataObject'
import HorizontalRuleIcon from '@mui/icons-material/HorizontalRule'
import FormatClearIcon from '@mui/icons-material/FormatClear'
import UndoIcon from '@mui/icons-material/Undo'
import RedoIcon from '@mui/icons-material/Redo'
import SearchIcon from '@mui/icons-material/Search'
import TocIcon from '@mui/icons-material/Toc'
import CloseIcon from '@mui/icons-material/Close'
import KeyboardArrowUpIcon from '@mui/icons-material/KeyboardArrowUp'
import KeyboardArrowDownIcon from '@mui/icons-material/KeyboardArrowDown'
import InsertPageBreakIcon from '@mui/icons-material/InsertPageBreak'
import PrintIcon from '@mui/icons-material/Print'
import ArticleOutlinedIcon from '@mui/icons-material/ArticleOutlined'
import ListAltIcon from '@mui/icons-material/ListAlt'
import WebAssetIcon from '@mui/icons-material/WebAsset'
import ViewAgendaOutlinedIcon from '@mui/icons-material/ViewAgendaOutlined'
import { yXmlFragmentToProsemirrorJSON } from 'y-prosemirror'
import type { CollabSession, CollabState } from '../markdown/collab/CollabSession.ts'
import { localUser } from '../markdown/collab/localUser.ts'
import { baseExtensions } from './extensions.ts'
import { fragmentOf, headerFooterOf, headerFooters, type HeaderFooter } from './content.ts'
import { Pagination, type PaginationStorage } from './pagination.ts'
import { pageBox, type Layout } from './pages.ts'
import { BlockChrome, blockCss, type BlockCommand } from './BlockChrome.tsx'
import { slashItems, type SlashItem } from './blockOps.ts'
import { hrefOfBlock } from './blockNodes.ts'
import { HeaderFooterDialog, PageSetupDialog, Ruler, printDocument, usePageSetup } from './PageView.tsx'
import { FindHighlight, findKey } from './findHighlight.ts'
import { findInBlocks, nextMatch, replacements, type FindOptions, type Range } from './find.ts'
import { describeSelection, headings, textBlocks, type Heading } from './blocks.ts'
import { paragraphStyles, styleOf, stylesCss, type ParagraphStyle } from './styles.ts'

// blocks (forge 35c-2) is block mode: the slash menu, drag handles and
// collapsible toggles, without page view
export type RichTextMode = 'pageless' | 'page' | 'compact' | 'blocks'

export interface RichTextSelection {
  text: string
  blocks: number[]
  heading: string
}

export interface RichTextViewProps {
  session: CollabSession
  readOnly: boolean
  mode?: RichTextMode
  toolbar?: boolean
  outline?: boolean
  placeholder?: string
  minHeight?: number
  maxHeight?: number
  label?: string
  // A link was opened (Ctrl or ⌘ and click, or any click while read-only):
  // true when it was handled, else it opens in a new browser tab
  onLink?(href: string, text: string): boolean | Promise<boolean>
  // The selection settled
  onSelection?(s: RichTextSelection): void
  // Block mode: an app's slash menu items, after the built-in ones (an item
  // with a built-in id replaces it), and what a command item raises
  items?: SlashItem[]
  onCommand?(c: BlockCommand): void
  // Shows the page/block mode switch in the toolbar
  onMode?(mode: 'page' | 'blocks'): void
}

function useCollabState(session: CollabSession): CollabState {
  return useSyncExternalStore(session.subscribe, session.snapshot)
}

export function RichTextView(p: RichTextViewProps) {
  const { session } = p
  const state = useCollabState(session)
  const locked = p.readOnly || !state.loaded || state.status === 'closed'
  const onLink = useRef(p.onLink)
  onLink.current = p.onLink
  const onSelection = useRef(p.onSelection)
  onSelection.current = p.onSelection
  const styles = useMemo(() => paragraphStyles(), [])
  const setup = usePageSetup(session.doc)
  const theme = useTheme()
  // Page mode lays the document out on pages unless its setup is pageless
  const paged = p.mode === 'page' && !setup.pageless
  const pagedRef = useRef(paged)
  pagedRef.current = paged
  const setupRef = useRef(setup)
  setupRef.current = setup
  const [layout, setLayout] = useState<Layout>()
  const box = pageBox(setup)

  const editor = useEditor({
    extensions: [
      ...baseExtensions(),
      FindHighlight,
      Pagination.configure({
        enabled: () => pagedRef.current,
        setup: () => setupRef.current,
        headerFooter: (name) => {
          const f = headerFooterOf(session.doc, name)
          return f.length > 0 ? (yXmlFragmentToProsemirrorJSON(f) as Record<string, unknown>) : undefined
        },
        bandHeight: 24,
        onLayout: setLayout,
      }),
      Collaboration.configure({ fragment: fragmentOf(session.doc) }),
      CollaborationCursor.configure({ provider: { awareness: session.awareness }, user: localUser() }),
    ],
    editable: false,
    editorProps: {
      attributes: { spellcheck: 'true', 'aria-label': p.label ?? 'Document' },
      // A page link, file or embed opens what it names
      handleClickOn(view, _pos, node, _nodePos, event, direct) {
        const href = direct ? hrefOfBlock(node.type.name, node.attrs) : undefined
        if (!href || (view.editable && event.detail > 1)) return false
        void Promise.resolve(onLink.current?.(href, String(node.attrs.title ?? node.attrs.name ?? '')) ?? false).then((handled) => {
          if (!handled && /^https?:/i.test(href)) window.open(href, '_blank', 'noopener,noreferrer')
        })
        return true
      },
      handleClick(view, pos, event) {
        const link = view.state.schema.marks.link
        const mark = view.state.doc.resolve(pos).marks().find((m) => m.type === link)
        if (!mark || !(event.ctrlKey || event.metaKey || !view.editable)) return false
        const href = String(mark.attrs.href ?? '')
        void Promise.resolve(onLink.current?.(href, '') ?? false).then((handled) => {
          if (!handled && /^https?:|^mailto:/i.test(href)) window.open(href, '_blank', 'noopener,noreferrer')
        })
        return true
      },
    },
  }, [session])

  useEffect(() => {
    editor?.setEditable(!locked)
  }, [editor, locked])

  // Lays out again when the page setup, page mode or a header or footer changes
  const relayout = () => (editor?.storage.pagination as PaginationStorage | undefined)?.relayout()
  useEffect(relayout, [editor, paged, setup])
  useEffect(() => {
    if (!editor) return
    const fragments = headerFooters.map((name) => headerFooterOf(session.doc, name))
    const changed = () => (editor.storage.pagination as PaginationStorage | undefined)?.relayout()
    fragments.forEach((f) => f.observeDeep(changed))
    return () => fragments.forEach((f) => f.unobserveDeep(changed))
  }, [editor, session])
  const [setupOpen, setSetupOpen] = useState(false)
  const [headersOpen, setHeadersOpen] = useState<HeaderFooter | false>(false)
  const pageRef = useRef<HTMLDivElement>(null)
  const print = () => pageRef.current && printDocument(pageRef.current, setup, p.label ?? 'Document')
  // A double click on a page's header or footer opens them
  const onDoubleClick = (e: React.MouseEvent) => {
    const t = e.target as HTMLElement
    const header = t.closest('.rt-page-header')
    const footer = t.closest('.rt-page-footer')
    if (!header && !footer) return
    const gap = t.closest('.rt-page-gap') as HTMLElement | null
    const first = !!gap?.classList.contains('rt-page-top')
    setHeadersOpen(header ? (first && setup.differentFirst ? 'firstHeader' : 'header') : 'footer')
  }

  // The selection, once it has settled for a moment
  useEffect(() => {
    if (!editor) return
    let timer: ReturnType<typeof setTimeout> | undefined
    const changed = () => {
      clearTimeout(timer)
      timer = setTimeout(() => {
        const { from, to } = editor.state.selection
        onSelection.current?.(describeSelection(editor.state.doc, from, to))
      }, 300)
    }
    editor.on('selectionUpdate', changed)
    return () => {
      clearTimeout(timer)
      editor.off('selectionUpdate', changed)
    }
  }, [editor])

  const [finding, setFinding] = useState(false)
  const [showOutline, setShowOutline] = useState(!!p.outline)
  const compact = p.mode === 'compact'
  const blockMode = p.mode === 'blocks'
  const items = useMemo(() => slashItems(p.items), [p.items])
  const toolbar = p.toolbar !== false && !p.readOnly
  const minHeight = p.minHeight ?? (compact ? 120 : 160)

  const onKeyDown = (e: React.KeyboardEvent) => {
    if ((e.ctrlKey || e.metaKey) && (e.key === 'f' || e.key === 'h')) {
      e.preventDefault()
      setFinding(true)
    }
  }

  return (
    <Box onKeyDown={onKeyDown} sx={{ display: 'flex', flexDirection: 'column', minHeight: 0, height: p.mode === 'page' || p.mode === 'pageless' || blockMode ? '100%' : undefined }}>
      {p.label && <Typography variant="caption" color="text.secondary">{p.label}</Typography>}
      <Box sx={{ border: compact ? 1 : 0, borderColor: 'divider', borderRadius: compact ? 1 : 0, bgcolor: 'background.paper', color: 'text.primary', display: 'flex', flexDirection: 'column', minHeight: 0, flex: 1 }}
        data-doc-richtext={session.fileId}>
        {toolbar && editor && (
          <Toolbar editor={editor} disabled={locked} compact={compact} styles={styles}
            outline={showOutline} onOutline={() => setShowOutline((o) => !o)} onFind={() => setFinding(true)}
            page={p.mode === 'page' ? { paged, onSetup: () => setSetupOpen(true), onHeaders: () => setHeadersOpen('header'), onPrint: print } : undefined}
            mode={p.onMode && (p.mode === 'page' || blockMode) ? { blocks: blockMode, onToggle: () => p.onMode?.(blockMode ? 'page' : 'blocks') } : undefined} />
        )}
        {finding && editor && <FindPanel editor={editor} readOnly={locked} onClose={() => setFinding(false)} />}
        <Box sx={{ display: 'flex', flex: 1, minHeight: 0 }}>
          {showOutline && editor && !compact && <Outline editor={editor} />}
          <Box sx={{
            flex: 1, minHeight, maxHeight: p.maxHeight, overflow: 'auto', bgcolor: paged ? 'background.default' : undefined,
          }} onDoubleClick={paged ? onDoubleClick : undefined}>
            {paged && editor && toolbar && (
              <Box sx={{ position: 'sticky', top: 0, zIndex: 2, pt: 1, bgcolor: 'background.default' }}>
                <Ruler editor={editor} setup={setup} disabled={locked} />
              </Box>
            )}
            <Box ref={pageRef} data-page-view={paged ? '' : undefined} data-pages={paged ? layout?.pages : undefined} data-block-mode={blockMode ? '' : undefined} sx={{
              mx: 'auto', my: paged ? 3 : 0, width: paged ? box.width : undefined, maxWidth: !paged && (p.mode === 'pageless' || blockMode) ? 900 : undefined,
              minHeight: paged ? (layout ? undefined : box.height) : '100%', boxSizing: 'border-box',
              pt: paged ? `${box.margins.top}px` : 1, pb: paged ? `${box.margins.bottom}px` : 1,
              px: paged ? undefined : compact ? 1.5 : 3, pl: paged ? `${box.margins.left}px` : blockMode ? 8 : undefined, pr: paged ? `${box.margins.right}px` : undefined,
              bgcolor: paged && setup.color ? setup.color : 'background.paper', color: paged && setup.color ? '#202124' : undefined,
              boxShadow: paged ? 2 : undefined, '--rt-page-bg': paged && setup.color ? setup.color : theme.palette.background.paper,
              ...pageCss,
              ...blockCss,
              '& .ProseMirror': { outline: 'none', minHeight: minHeight - 16, lineHeight: 1.6, ...stylesCss(styles) },
              '& .ProseMirror > *:first-of-type': { mt: 0 },
              '& .ProseMirror a': { color: 'primary.main' },
              '& .ProseMirror blockquote': { borderLeft: 3, borderColor: 'divider', pl: 2, ml: 0, color: 'text.secondary' },
              '& .ProseMirror pre': { bgcolor: 'action.hover', p: 1.5, borderRadius: 1, fontFamily: 'monospace', fontSize: '0.9em', overflowX: 'auto' },
              '& .ProseMirror code': { bgcolor: 'action.hover', borderRadius: 0.5, px: 0.5, fontFamily: 'monospace', fontSize: '0.9em' },
              '& .ProseMirror img': { maxWidth: '100%', height: 'auto' },
              '& .ProseMirror img.ProseMirror-selectednode': { outline: 2, outlineColor: 'primary.main' },
              '& .ProseMirror table': { borderCollapse: 'collapse', tableLayout: 'fixed', width: '100%', my: 1, overflow: 'hidden' },
              '& .ProseMirror td, & .ProseMirror th': { border: 1, borderColor: 'divider', p: 0.75, verticalAlign: 'top', position: 'relative', minWidth: 40 },
              '& .ProseMirror th': { bgcolor: 'action.hover', fontWeight: 600, textAlign: 'left' },
              '& .ProseMirror .selectedCell::after': { content: '""', position: 'absolute', inset: 0, bgcolor: 'primary.main', opacity: 0.15, pointerEvents: 'none' },
              '& .ProseMirror .column-resize-handle': { position: 'absolute', right: -2, top: 0, bottom: 0, width: 4, bgcolor: 'primary.main', pointerEvents: 'none' },
              '& .ProseMirror.resize-cursor': { cursor: 'col-resize' },
              '& .ProseMirror ul[data-type="taskList"]': { listStyle: 'none', pl: 0.5 },
              '& .ProseMirror ul[data-type="taskList"] li': { display: 'flex', gap: 1, alignItems: 'flex-start' },
              '& .ProseMirror ul[data-type="taskList"] li > div': { flex: 1 },
              '& .ProseMirror hr': { border: 0, borderTop: 1, borderColor: 'divider', my: 2 },
              '& .ProseMirror mark': { borderRadius: 0.5, px: 0.25 },
              '& .rt-footnote': { fontSize: 0, verticalAlign: 'super', color: 'text.secondary', cursor: 'pointer', '&::before': { content: 'attr(data-n)', fontSize: '0.75rem', color: 'primary.main', fontWeight: 600 } },
              '& .rt-footnote.rt-footnote-open': {
                fontSize: '0.8rem', bgcolor: 'action.selected', borderRadius: 0.5, px: 0.5, cursor: 'text',
                '&::before': { content: 'attr(data-n) ". "' },
              },
              '& .rt-math': { fontFamily: 'monospace', bgcolor: 'action.hover', borderRadius: 0.5, px: 0.5 },
              '& .rt-mention': { color: 'primary.main', fontWeight: 500 },
              '& .rt-find': { bgcolor: 'warning.light', color: 'common.black' },
              '& .rt-find-current': { bgcolor: 'warning.main' },
              // Remote cursors and selections (CollaborationCursor), in each person's colour
              '& .collaboration-cursor__caret': { position: 'relative', mx: '-1px', borderLeft: '1px solid', borderRight: '1px solid', wordBreak: 'normal', pointerEvents: 'none' },
              '& .collaboration-cursor__label': {
                position: 'absolute', top: '-1.4em', left: -1, fontSize: 11, fontWeight: 600, lineHeight: 'normal', whiteSpace: 'nowrap',
                color: '#fff', px: 0.5, borderRadius: '3px 3px 3px 0', userSelect: 'none',
              },
            }}>
              {blockMode && editor ? (
                <BlockChrome editor={editor} items={items} locked={locked} onCommand={p.onCommand}><EditorContent editor={editor} /></BlockChrome>
              ) : <EditorContent editor={editor} />}
              {p.placeholder && editor?.isEmpty && !locked && (
                <Typography color="text.disabled" sx={{ mt: -3.5, pointerEvents: 'none' }}>{p.placeholder}</Typography>
              )}
            </Box>
          </Box>
        </Box>
        <StatusBar editor={editor} state={state} readOnly={p.readOnly} pages={paged ? layout?.pages : undefined} />
      </Box>
      {p.mode === 'page' && <PageSetupDialog doc={session.doc} open={setupOpen} onClose={() => setSetupOpen(false)} />}
      {p.mode === 'page' && (
        <HeaderFooterDialog doc={session.doc} open={!!headersOpen} start={headersOpen || undefined} readOnly={locked} onClose={() => setHeadersOpen(false)} />
      )}
    </Box>
  )
}

// Page view's parts: breaks, the table of contents, the page gaps the
// pagination plugin draws, and print, which hides the bands between pages
// and starts each page's header on a printed page
const pageCss = {
  '& .rt-page-break, & .rt-section-break': {
    position: 'relative', height: 0, my: 1.5, borderTop: '1px dashed', borderColor: 'divider',
    '&::after': { position: 'absolute', top: -9, left: '50%', transform: 'translateX(-50%)', px: 1, fontSize: 11, color: 'text.secondary', bgcolor: 'background.paper' },
  },
  '& .rt-page-break::after': { content: '"Page break"' },
  '& .rt-section-break::after': { content: '"Section break"' },
  '& .rt-page-break.ProseMirror-selectednode, & .rt-section-break.ProseMirror-selectednode, & .rt-toc.ProseMirror-selectednode': { outline: 2, outlineColor: 'primary.main' },
  '& .rt-toc': { border: 1, borderColor: 'divider', borderRadius: 1, p: 1.5, my: 1 },
  '& .rt-toc-title': { fontWeight: 600, mb: 0.5 },
  '& .rt-toc-empty': { color: 'text.secondary', fontSize: 13 },
  '& .rt-toc-entry': { display: 'flex', gap: 1, color: 'primary.main', textDecoration: 'none', py: 0.25, '&:hover': { textDecoration: 'underline' } },
  '& .rt-toc-entry > span:first-of-type': { flex: 1 },
  '& .rt-toc-page': { color: 'text.secondary' },
  '& .rt-page-number': { bgcolor: 'action.selected', borderRadius: 0.5, px: 0.5 },
  '& .rt-page-gap': { boxSizing: 'border-box', userSelect: 'none', cursor: 'default', position: 'relative' },
  '& .rt-page-gap > div': { boxSizing: 'border-box', overflow: 'hidden' },
  '& .rt-page-band': { bgcolor: 'background.default', boxShadow: 'inset 0 6px 6px -6px rgba(0,0,0,.35), inset 0 -6px 6px -6px rgba(0,0,0,.35)', mx: '-1px' },
  '& .rt-page-header, & .rt-page-footer': { color: 'text.secondary', fontSize: 13, display: 'flex', flexDirection: 'column' },
  '& .rt-page-header': { justifyContent: 'flex-end', pb: 2 },
  '& .rt-page-footer': { justifyContent: 'flex-start', pt: 2 },
  '& .rt-page-header p, & .rt-page-footer p': { m: 0 },
  '& .rt-page-notes': { fontSize: 12, lineHeight: '17px', color: 'text.secondary' },
  '& .rt-page-notes:not(:empty)': { borderTop: 1, borderColor: 'divider', pt: '8px', backgroundClip: 'content-box' },
  '& .rt-endnotes': { borderTop: 1, borderColor: 'divider', mt: 3, pt: 1, fontSize: 13, color: 'text.secondary' },
  '& .rt-split > td, & .rt-split > th': { paddingTop: 'calc(var(--rt-pad) + 6px)' },
  '& .rt-page-split': { position: 'absolute', top: 0, zIndex: 1 },
  '& .rt-page-split > .rt-page-gap': { bgcolor: 'var(--rt-page-bg)' },
  '& .rt-repeat-header': { borderCollapse: 'collapse', tableLayout: 'fixed', pointerEvents: 'none' },
  '& .rt-repeat-header td, & .rt-repeat-header th': { border: 1, borderColor: 'divider', p: 0.75, textAlign: 'left', bgcolor: 'action.hover', fontWeight: 600 },
  '@media print': {
    boxShadow: 'none !important', m: '0 !important',
    '& .rt-page-band': { display: 'none' },
    '& .rt-page-gap:not(.rt-page-top) .rt-page-header': { breakBefore: 'page' },
    '& .rt-page-break, & .rt-section-break': { borderTop: 0, '&::after': { display: 'none' } },
    '& .collaboration-cursor__caret, & .rt-find, & .rt-find-current': { all: 'unset' },
    '& .ProseMirror-selectednode': { outline: 'none !important' },
  },
} as const

function statusText(state: CollabState, readOnly: boolean): string {
  if (state.status === 'offline') return 'Offline: changes are kept and sent when the connection returns'
  if (state.status === 'connecting' || state.status === 'syncing') return 'Connecting…'
  if (state.saveError) return state.saveError
  if (readOnly) return 'Read-only'
  if (state.saving) return 'Saving…'
  if (state.lastSavedAt) return 'Saved'
  return ''
}

// Re-renders a component on every transaction of the editor
function useEditorTick(editor: Editor | null) {
  const [, setTick] = useState(0)
  useEffect(() => {
    if (!editor) return
    const bump = () => setTick((t) => t + 1)
    editor.on('transaction', bump)
    return () => {
      editor.off('transaction', bump)
    }
  }, [editor])
}

function StatusBar({ editor, state, readOnly, pages }: { editor: Editor | null; state: CollabState; readOnly: boolean; pages?: number }) {
  useEditorTick(editor)
  const words = (editor?.storage.characterCount?.words?.() as number | undefined) ?? 0
  return (
    <Stack direction="row" spacing={1} sx={{ px: 1, py: 0.25, borderTop: 1, borderColor: 'divider', alignItems: 'center' }}>
      <Typography variant="caption" color={state.saveError ? 'error' : 'text.secondary'} sx={{ flex: 1 }} data-doc-status>
        {statusText(state, readOnly)}
      </Typography>
      {pages !== undefined && <Typography variant="caption" color="text.secondary" data-page-count>{pages === 1 ? '1 page' : `${pages} pages`}</Typography>}
      <Typography variant="caption" color="text.secondary" data-word-count>{words === 1 ? '1 word' : `${words} words`}</Typography>
      {state.peers.map((peer) => (
        <Tooltip key={peer.clientId} title={peer.name}>
          <Box sx={{ width: 10, height: 10, borderRadius: '50%', bgcolor: peer.color }} />
        </Tooltip>
      ))}
    </Stack>
  )
}

function ToolButton({ title, active, disabled, onClick, children }: { title: string; active?: boolean; disabled?: boolean; onClick(): void; children: ReactNode }) {
  return (
    <Tooltip title={title}>
      <span>
        <IconButton size="small" aria-label={title} aria-pressed={active} disabled={disabled} onClick={onClick} onMouseDown={(e) => e.preventDefault()}
          color={active ? 'primary' : 'default'} sx={{ borderRadius: 1, bgcolor: active ? 'action.selected' : undefined }}>
          {children}
        </IconButton>
      </span>
    </Tooltip>
  )
}

const colours = ['#000000', '#5f6368', '#d93025', '#e37400', '#f9ab00', '#188038', '#1a73e8', '#9334e6']
const highlights = ['#fef08a', '#bbf7d0', '#bfdbfe', '#fbcfe8', '#fed7aa']

interface PageActions {
  paged: boolean
  onSetup(): void
  onHeaders(): void
  onPrint(): void
}

function Toolbar({ editor, disabled, compact, styles, outline, onOutline, onFind, page, mode }: {
  editor: Editor; disabled: boolean; compact: boolean; styles: ParagraphStyle[]; outline: boolean; onOutline(): void; onFind(): void; page?: PageActions
  mode?: { blocks: boolean; onToggle(): void }
}) {
  useEditorTick(editor)
  const chain = () => editor.chain().focus()
  const [ask, setAsk] = useState<{ what: 'link' | 'image'; anchor: HTMLElement }>()
  const [palette, setPalette] = useState<{ what: 'color' | 'highlight'; anchor: HTMLElement }>()
  const block = editor.state.selection.$from.parent
  const current = styleOf(styles, block.type.name, Number(block.attrs.level) || undefined)
  const inTable = editor.isActive('table')
  const sep = <Divider orientation="vertical" flexItem sx={{ mx: 0.5 }} />

  const setStyle = (id: string) => {
    const s = styles.find((x) => x.id === id)
    if (!s) return
    if (s.node === 'heading') chain().setHeading({ level: (s.level ?? 1) as 1 | 2 | 3 | 4 | 5 | 6 }).run()
    else chain().setParagraph().run()
  }

  return (
    <Stack direction="row" sx={{ px: 0.5, py: 0.25, borderBottom: 1, borderColor: 'divider', alignItems: 'center', flexWrap: 'wrap', rowGap: 0.25 }} role="toolbar" aria-label="Formatting">
      <ToolButton title="Undo (Ctrl+Z)" disabled={disabled} onClick={() => chain().undo().run()}><UndoIcon fontSize="small" /></ToolButton>
      <ToolButton title="Redo (Ctrl+Shift+Z)" disabled={disabled} onClick={() => chain().redo().run()}><RedoIcon fontSize="small" /></ToolButton>
      {sep}
      <Select size="small" variant="standard" disableUnderline value={current?.id ?? ''} disabled={disabled} displayEmpty
        onChange={(e) => setStyle(String(e.target.value))} inputProps={{ 'aria-label': 'Paragraph style' }} sx={{ minWidth: 120, fontSize: 14, mx: 0.5 }}>
        {!current && <MenuItem value="" disabled>—</MenuItem>}
        {styles.map((s) => <MenuItem key={s.id} value={s.id}>{s.name}</MenuItem>)}
      </Select>
      {sep}
      <ToolButton title="Bold (Ctrl+B)" active={editor.isActive('bold')} disabled={disabled} onClick={() => chain().toggleBold().run()}><FormatBoldIcon fontSize="small" /></ToolButton>
      <ToolButton title="Italic (Ctrl+I)" active={editor.isActive('italic')} disabled={disabled} onClick={() => chain().toggleItalic().run()}><FormatItalicIcon fontSize="small" /></ToolButton>
      <ToolButton title="Underline (Ctrl+U)" active={editor.isActive('underline')} disabled={disabled} onClick={() => chain().toggleUnderline().run()}><FormatUnderlinedIcon fontSize="small" /></ToolButton>
      {!compact && <ToolButton title="Strikethrough" active={editor.isActive('strike')} disabled={disabled} onClick={() => chain().toggleStrike().run()}><StrikethroughSIcon fontSize="small" /></ToolButton>}
      {!compact && <ToolButton title="Superscript" active={editor.isActive('superscript')} disabled={disabled} onClick={() => chain().toggleSuperscript().run()}><SuperscriptIcon fontSize="small" /></ToolButton>}
      {!compact && <ToolButton title="Subscript" active={editor.isActive('subscript')} disabled={disabled} onClick={() => chain().toggleSubscript().run()}><SubscriptIcon fontSize="small" /></ToolButton>}
      <ToolButton title="Text colour" disabled={disabled} onClick={() => setPalette({ what: 'color', anchor: document.activeElement as HTMLElement })}>
        <FormatColorTextIcon fontSize="small" sx={{ color: (editor.getAttributes('textStyle').color as string | undefined) ?? undefined }} />
      </ToolButton>
      <ToolButton title="Highlight" active={editor.isActive('highlight')} disabled={disabled} onClick={() => setPalette({ what: 'highlight', anchor: document.activeElement as HTMLElement })}>
        <BorderColorIcon fontSize="small" />
      </ToolButton>
      {sep}
      {!compact && (['left', 'center', 'right', 'justify'] as const).map((a) => (
        <ToolButton key={a} title={`Align ${a}`} active={editor.isActive({ textAlign: a })} disabled={disabled} onClick={() => chain().setTextAlign(a).run()}>
          {a === 'left' ? <FormatAlignLeftIcon fontSize="small" /> : a === 'center' ? <FormatAlignCenterIcon fontSize="small" /> : a === 'right' ? <FormatAlignRightIcon fontSize="small" /> : <FormatAlignJustifyIcon fontSize="small" />}
        </ToolButton>
      ))}
      {!compact && sep}
      <ToolButton title="Bulleted list" active={editor.isActive('bulletList')} disabled={disabled} onClick={() => chain().toggleBulletList().run()}><FormatListBulletedIcon fontSize="small" /></ToolButton>
      <ToolButton title="Numbered list" active={editor.isActive('orderedList')} disabled={disabled} onClick={() => chain().toggleOrderedList().run()}><FormatListNumberedIcon fontSize="small" /></ToolButton>
      <ToolButton title="Check list" active={editor.isActive('taskList')} disabled={disabled} onClick={() => chain().toggleTaskList().run()}><ChecklistIcon fontSize="small" /></ToolButton>
      {sep}
      <ToolButton title="Link" active={editor.isActive('link')} disabled={disabled} onClick={() => setAsk({ what: 'link', anchor: document.activeElement as HTMLElement })}><LinkIcon fontSize="small" /></ToolButton>
      {!compact && <ToolButton title="Image" disabled={disabled} onClick={() => setAsk({ what: 'image', anchor: document.activeElement as HTMLElement })}><ImageIcon fontSize="small" /></ToolButton>}
      {!compact && <ToolButton title={inTable ? 'Add a row below' : 'Table'} disabled={disabled}
        onClick={() => (inTable ? chain().addRowAfter().run() : chain().insertTable({ rows: 3, cols: 3, withHeaderRow: true }).run())}><TableChartIcon fontSize="small" /></ToolButton>}
      {inTable && !compact && (
        <>
          <Button size="small" disabled={disabled} onMouseDown={(e) => e.preventDefault()} onClick={() => chain().addColumnAfter().run()}>+ Column</Button>
          <Button size="small" disabled={disabled || !editor.can().mergeOrSplit()} onMouseDown={(e) => e.preventDefault()} onClick={() => chain().mergeOrSplit().run()}>Merge</Button>
          <Button size="small" disabled={disabled} onMouseDown={(e) => e.preventDefault()} onClick={() => chain().toggleHeaderRow().run()}>Header</Button>
          <Button size="small" color="error" disabled={disabled} onMouseDown={(e) => e.preventDefault()} onClick={() => chain().deleteRow().run()}>− Row</Button>
          <Button size="small" color="error" disabled={disabled} onMouseDown={(e) => e.preventDefault()} onClick={() => chain().deleteColumn().run()}>− Column</Button>
          <Button size="small" color="error" disabled={disabled} onMouseDown={(e) => e.preventDefault()} onClick={() => chain().deleteTable().run()}>Delete table</Button>
        </>
      )}
      <ToolButton title="Code block" active={editor.isActive('codeBlock')} disabled={disabled} onClick={() => chain().toggleCodeBlock().run()}><DataObjectIcon fontSize="small" /></ToolButton>
      {!compact && <ToolButton title="Horizontal rule" disabled={disabled} onClick={() => chain().setHorizontalRule().run()}><HorizontalRuleIcon fontSize="small" /></ToolButton>}
      {!compact && <ToolButton title="Footnote" disabled={disabled}
        onClick={() => chain().insertContent({ type: 'footnote', content: [{ type: 'text', text: 'Footnote' }] }).run()}><sup style={{ fontWeight: 700, fontSize: 12 }}>1</sup></ToolButton>}
      {page && (
        <>
          {sep}
          <ToolButton title="Page break (Ctrl+Enter)" disabled={disabled} onClick={() => chain().setPageBreak().run()}><InsertPageBreakIcon fontSize="small" /></ToolButton>
          <Button size="small" disabled={disabled} onMouseDown={(e) => e.preventDefault()} onClick={() => chain().setSectionBreak().run()}>Section break</Button>
          <ToolButton title="Table of contents" disabled={disabled} onClick={() => chain().insertTableOfContents().run()}><ListAltIcon fontSize="small" /></ToolButton>
          <ToolButton title="Headers and footers" disabled={!page.paged} onClick={page.onHeaders}><WebAssetIcon fontSize="small" /></ToolButton>
          <ToolButton title="Page setup" onClick={page.onSetup}><ArticleOutlinedIcon fontSize="small" /></ToolButton>
        </>
      )}
      <ToolButton title="Clear formatting" disabled={disabled} onClick={() => chain().unsetAllMarks().clearNodes().run()}><FormatClearIcon fontSize="small" /></ToolButton>
      <Box sx={{ flex: 1 }} />
      {mode && (
        <ToolButton title={mode.blocks ? 'Block mode: switch to page mode' : 'Switch to block mode'} active={mode.blocks} disabled={disabled} onClick={mode.onToggle}>
          <ViewAgendaOutlinedIcon fontSize="small" />
        </ToolButton>
      )}
      {page && <ToolButton title="Print" disabled={!page.paged} onClick={page.onPrint}><PrintIcon fontSize="small" /></ToolButton>}
      <ToolButton title="Find and replace (Ctrl+F)" onClick={onFind}><SearchIcon fontSize="small" /></ToolButton>
      {!compact && <ToolButton title="Outline" active={outline} onClick={onOutline}><TocIcon fontSize="small" /></ToolButton>}

      <Popover open={!!palette} anchorEl={palette?.anchor} onClose={() => setPalette(undefined)} anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}>
        <Stack direction="row" spacing={0.5} sx={{ p: 1 }}>
          {(palette?.what === 'color' ? colours : highlights).map((c) => (
            <Box key={c} component="button" aria-label={c} onClick={() => {
              if (palette?.what === 'color') chain().setColor(c).run()
              else chain().toggleHighlight({ color: c }).run()
              setPalette(undefined)
            }} sx={{ width: 22, height: 22, borderRadius: '50%', border: 1, borderColor: 'divider', bgcolor: c, cursor: 'pointer', p: 0 }} />
          ))}
          <Button size="small" onClick={() => {
            if (palette?.what === 'color') chain().unsetColor().run()
            else chain().unsetHighlight().run()
            setPalette(undefined)
          }}>None</Button>
        </Stack>
      </Popover>
      {ask && <AskUrl editor={editor} what={ask.what} anchor={ask.anchor} onClose={() => setAsk(undefined)} />}
    </Stack>
  )
}

// Asks for a link's or an image's address
function AskUrl({ editor, what, anchor, onClose }: { editor: Editor; what: 'link' | 'image'; anchor: HTMLElement; onClose(): void }) {
  const [url, setUrl] = useState(what === 'link' ? String(editor.getAttributes('link').href ?? '') : '')
  const [alt, setAlt] = useState('')
  const apply = () => {
    const chain = editor.chain().focus()
    if (what === 'link') {
      if (url) chain.extendMarkRange('link').setLink({ href: url }).run()
      else chain.extendMarkRange('link').unsetLink().run()
    } else if (url) {
      chain.setImage({ src: url, alt }).run()
    }
    onClose()
  }
  return (
    <Popover open anchorEl={anchor} onClose={onClose} anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}>
      <Stack spacing={1} sx={{ p: 1.5, width: 340 }} component="form" onSubmit={(e) => (e.preventDefault(), apply())}>
        <TextField size="small" autoFocus label={what === 'link' ? 'Link (https: or workbench:)' : 'Image address'} value={url} onChange={(e) => setUrl(e.target.value)} />
        {what === 'image' && <TextField size="small" label="Alternative text" value={alt} onChange={(e) => setAlt(e.target.value)} />}
        <Stack direction="row" spacing={1} sx={{ justifyContent: 'flex-end' }}>
          {what === 'link' && editor.isActive('link') && <Button size="small" color="error" onClick={() => (setUrl(''), editor.chain().focus().extendMarkRange('link').unsetLink().run(), onClose())}>Remove</Button>}
          <Button size="small" onClick={onClose}>Cancel</Button>
          <Button size="small" variant="contained" type="submit">Apply</Button>
        </Stack>
      </Stack>
    </Popover>
  )
}

function FindPanel({ editor, readOnly, onClose }: { editor: Editor; readOnly: boolean; onClose(): void }) {
  const [query, setQuery] = useState('')
  const [replace, setReplace] = useState('')
  const [options, setOptions] = useState<FindOptions>({})
  const [matches, setMatches] = useState<Range[]>([])
  const [current, setCurrent] = useState(-1)

  // Finds again as the query, the options or the document change
  useEffect(() => {
    const find = () => {
      const found = findInBlocks(textBlocks(editor.state.doc), query, options)
      setMatches(found)
      setCurrent((c) => (found.length === 0 ? -1 : Math.min(Math.max(c, 0), found.length - 1)))
    }
    find()
    editor.on('update', find)
    return () => {
      editor.off('update', find)
    }
  }, [editor, query, options])

  useEffect(() => {
    editor.view.dispatch(editor.state.tr.setMeta(findKey, { matches, current }).setMeta('addToHistory', false))
  }, [editor, matches, current])
  useEffect(() => () => {
    if (!editor.isDestroyed) editor.view.dispatch(editor.state.tr.setMeta(findKey, { matches: [], current: -1 }))
  }, [editor])

  const go = (backwards: boolean) => {
    const pos = current >= 0 ? (backwards ? matches[current].from : matches[current].to) : editor.state.selection.from
    const i = nextMatch(matches, pos, backwards)
    if (i < 0) return
    setCurrent(i)
    editor.commands.setTextSelection(matches[i])
    editor.commands.scrollIntoView()
  }
  const replaceOne = () => {
    const m = matches[current]
    if (!m) return
    editor.chain().insertContentAt(m, replace).run()
  }
  const replaceAll = () => {
    if (matches.length === 0) return
    let tr = editor.state.tr
    for (const m of replacements(matches)) tr = replace ? tr.insertText(replace, m.from, m.to) : tr.delete(m.from, m.to)
    editor.view.dispatch(tr)
  }

  return (
    <Stack direction="row" spacing={1} sx={{ px: 1, py: 0.5, borderBottom: 1, borderColor: 'divider', alignItems: 'center', flexWrap: 'wrap' }} role="search">
      <InputBase autoFocus placeholder="Find" value={query} onChange={(e) => setQuery(e.target.value)} inputProps={{ 'aria-label': 'Find' }}
        onKeyDown={(e) => {
          if (e.key === 'Enter') (e.preventDefault(), go(e.shiftKey))
          if (e.key === 'Escape') onClose()
        }} sx={{ border: 1, borderColor: 'divider', borderRadius: 1, px: 1, fontSize: 14, width: 180 }} />
      <Typography variant="caption" color="text.secondary" sx={{ minWidth: 56 }} data-find-count>
        {query ? (matches.length ? `${current + 1} of ${matches.length}` : 'No results') : ''}
      </Typography>
      <IconButton size="small" aria-label="Previous match" onClick={() => go(true)} disabled={!matches.length}><KeyboardArrowUpIcon fontSize="small" /></IconButton>
      <IconButton size="small" aria-label="Next match" onClick={() => go(false)} disabled={!matches.length}><KeyboardArrowDownIcon fontSize="small" /></IconButton>
      {!readOnly && (
        <>
          <InputBase placeholder="Replace with" value={replace} onChange={(e) => setReplace(e.target.value)} inputProps={{ 'aria-label': 'Replace with' }}
            sx={{ border: 1, borderColor: 'divider', borderRadius: 1, px: 1, fontSize: 14, width: 160 }} />
          <Button size="small" onClick={replaceOne} disabled={current < 0}>Replace</Button>
          <Button size="small" onClick={replaceAll} disabled={!matches.length}>Replace all</Button>
        </>
      )}
      <FormControlLabel label={<Typography variant="caption">Match case</Typography>}
        control={<Checkbox size="small" checked={!!options.caseSensitive} onChange={(_, c) => setOptions((o) => ({ ...o, caseSensitive: c }))} />} />
      <FormControlLabel label={<Typography variant="caption">Whole words</Typography>}
        control={<Checkbox size="small" checked={!!options.wholeWord} onChange={(_, c) => setOptions((o) => ({ ...o, wholeWord: c }))} />} />
      <Box sx={{ flex: 1 }} />
      <IconButton size="small" aria-label="Close find" onClick={onClose}><CloseIcon fontSize="small" /></IconButton>
    </Stack>
  )
}

function Outline({ editor }: { editor: Editor }) {
  useEditorTick(editor)
  const list: Heading[] = headings(editor.state.doc)
  return (
    <Box sx={{ width: 220, flexShrink: 0, borderRight: 1, borderColor: 'divider', overflow: 'auto' }} aria-label="Outline">
      <Typography variant="overline" sx={{ px: 1.5, display: 'block' }}>Outline</Typography>
      {list.length === 0 && <Typography variant="caption" color="text.secondary" sx={{ px: 1.5 }}>Headings appear here.</Typography>}
      <List dense disablePadding>
        {list.map((h) => (
          <ListItemButton key={`${h.pos}`} sx={{ pl: 1.5 + (h.level - 1) * 1.5, py: 0.25 }} onClick={() => {
            editor.chain().focus().setTextSelection(h.pos + 1).scrollIntoView().run()
          }}>
            <ListItemText primary={h.text || 'Untitled'} slotProps={{ primary: { noWrap: true, sx: { fontSize: 13, fontWeight: h.level === 1 ? 600 : 400 } } }} />
          </ListItemButton>
        ))}
      </List>
    </Box>
  )
}
