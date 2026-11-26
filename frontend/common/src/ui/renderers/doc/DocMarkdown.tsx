// doc.markdown (forge 07l): a record's markdown field edited together.
// The field's collaboration room (Collab.openDoc, kind record) is joined
// like a volume file's: one shared Y.Text, everyone's cursors in their
// colour, and the room's writer saving the field (record.update, as the
// writer) a second after typing pauses. The view is Foundation's Live
// Preview (CodeMirror 6) with a formatting toolbar.

import { live } from '../../../live/LiveConnection.ts'
import { useEffect, useMemo, useRef, useState } from 'react'
import { defaultKeymap, indentWithTab } from '@codemirror/commands'
import { markdown, markdownLanguage } from '@codemirror/lang-markdown'
import { Compartment, EditorSelection, EditorState } from '@codemirror/state'
import { EditorView, keymap, placeholder as cmPlaceholder } from '@codemirror/view'
import { Alert, Box, IconButton, Stack, Tooltip, Typography, useTheme } from '@mui/material'
import FormatBoldIcon from '@mui/icons-material/FormatBold'
import FormatItalicIcon from '@mui/icons-material/FormatItalic'
import TitleIcon from '@mui/icons-material/Title'
import FormatListBulletedIcon from '@mui/icons-material/FormatListBulleted'
import FormatQuoteIcon from '@mui/icons-material/FormatQuote'
import CodeIcon from '@mui/icons-material/Code'
import LinkIcon from '@mui/icons-material/Link'
import { yCollab, yUndoManagerKeymap } from 'y-codemirror.next'
import * as Y from 'yjs'
import { Collab } from '../../../services/Collab.ts'
import type { CollabSession } from '../../../editors/markdown/collab/CollabSession.ts'
import { useCollabSession, useCollabState } from '../../../editors/markdown/collab/useCollabSession.ts'
import { livePreview } from '../../../editors/markdown/live/livePreview.ts'
import { obsidianTheme } from '../../../editors/markdown/live/obsidianTheme.ts'
import type { RendererProps } from '../../points.ts'
import { describeSelection, placeHighlights, type Highlight, type Selected } from './highlights.ts'
import { highlightExtension, highlightRange, setHighlights, type HighlightHandlers } from './highlightField.ts'

const str = (v: unknown) => (v === undefined || v === null ? '' : String(v))

type Room = { fileId: string; readOnly: boolean } | { error: string }

// What a markdown field's editor takes, from doc.markdown's props or a
// record form's field
export interface MarkdownFieldProps {
  entity: string
  id: string
  field: string
  placeholder?: string
  toolbar?: boolean
  minHeight?: number
  maxHeight?: number
  disabled?: boolean
  label?: string
  // The text, a second after it stops changing
  onChange?(value: string): void
  // An object ("<type>.<id>") this window is present in while the editor
  // is shown, its state typingState while this window typed in the last
  // few seconds (forge 07o: "Ana is replying" on a mail thread)
  typingIn?: string
  typingState?: Record<string, unknown>
  // Marks over the text (forge 18c): each placed by its Yjs positions,
  // its range or its quote found again, and kept in place as people type
  highlights?: Highlight[]
  // Scrolls to a highlight whenever it changes: {id, n}
  focus?: { id?: string; n?: number }
  onSelected?(s: Selected): void
  onHighlightClicked?(id: string): void
}

export function DocMarkdown(p: RendererProps) {
  const emit = useRef(p.emit)
  emit.current = p.emit
  const onChange = useMemo(() => (value: string) => emit.current('change', { value }), [])
  const onSelected = useMemo(() => (s: Selected) => emit.current('selected', { ...s }), [])
  const onHighlightClicked = useMemo(() => (id: string) => emit.current('highlightClicked', { id }), [])
  const marks = {
    highlights: Array.isArray(p.props.highlights) ? (p.props.highlights as Highlight[]) : undefined,
    focus: p.props.focus as MarkdownFieldProps['focus'],
    onSelected: p.handles('selected') ? onSelected : undefined,
    onHighlightClicked: p.handles('highlightClicked') ? onHighlightClicked : undefined,
  }
  // Text given as value, with no record: shown read-only, with highlights and selection
  if (!str(p.props.entity) && !str(p.props.id) && p.props.value !== undefined) {
    return <StaticDoc value={str(p.props.value)} minHeight={p.props.minHeight as number | undefined} maxHeight={p.props.maxHeight as number | undefined} {...marks} />
  }
  return (
    <MarkdownField {...marks} entity={str(p.props.entity)} id={str(p.props.id)} field={str(p.props.field)} placeholder={str(p.props.placeholder) || undefined}
      toolbar={p.props.toolbar !== false} minHeight={p.props.minHeight as number | undefined} maxHeight={p.props.maxHeight as number | undefined}
      disabled={!!p.props.disabled} onChange={p.handles('change') ? onChange : undefined}
      typingIn={str(p.props.typingIn) || undefined} typingState={p.props.typingState as Record<string, unknown> | undefined} />
  )
}

export function MarkdownField(p: MarkdownFieldProps) {
  const { entity, id, field } = p
  const [room, setRoom] = useState<Room>()
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    if (!entity || !id || !field) return
    let alive = true
    Collab.openDoc({ kind: 'record', ref: { entity, id, field } })
      .then((r) => alive && setRoom(r.fileId ? { fileId: r.fileId, readOnly: !!r.readOnly } : { error: r.message ?? 'cannot open' }))
      .catch((e: Error) => alive && setRoom({ error: e.message }))
    return () => {
      alive = false
    }
  }, [entity, id, field, attempt])
  if (!entity || !id || !field) return <Typography variant="body2" color="text.secondary">Save the record to edit this together.</Typography>
  if (!room) return <Box sx={{ minHeight: p.minHeight ?? 120 }} />
  if ('error' in room) return <Alert severity="error" variant="outlined">{room.error}</Alert>
  return (
    <DocEditor key={room.fileId} fileId={room.fileId} readOnly={room.readOnly || !!p.disabled} p={p}
      // A dropped room (the server restarted, or the room emptied) is opened again
      onClosed={() => setTimeout(() => setAttempt((a) => a + 1), 500)} />
  )
}

function DocEditor({ fileId, readOnly, p, onClosed }: { fileId: string; readOnly: boolean; p: MarkdownFieldProps; onClosed(): void }) {
  const session = useCollabSession(fileId, readOnly)
  const state = useCollabState(session)
  const theme = useTheme()
  const parent = useRef<HTMLDivElement>(null)
  const [compartments] = useState(() => ({ theme: new Compartment(), readOnly: new Compartment() }))
  const [view, setView] = useState<EditorView>()
  const placeholder = p.placeholder ?? ''
  const handlers = useMarkHandlers(p, session.ytext)

  useEffect(() => {
    if (!parent.current) return
    const created = new EditorView({
      parent: parent.current,
      state: EditorState.create({
        doc: session.ytext.toString(),
        extensions: [
          keymap.of([...yUndoManagerKeymap, ...defaultKeymap, indentWithTab]),
          markdown({ base: markdownLanguage }),
          yCollab(session.ytext, session.awareness, { undoManager: new Y.UndoManager(session.ytext) }),
          livePreview({ onLink: (url) => (window.open(url, '_blank', 'noopener,noreferrer'), true) }),
          EditorView.lineWrapping,
          highlightExtension(handlers),
          placeholder ? cmPlaceholder(placeholder) : [],
          compartments.theme.of([]),
          compartments.readOnly.of([]),
        ],
      }),
    })
    setView(created)
    return () => {
      created.destroy()
      setView(undefined)
    }
    // the placeholder is set once
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session, compartments])

  useEffect(() => {
    view?.dispatch({ effects: compartments.theme.reconfigure(obsidianTheme(theme)) })
  }, [view, theme, compartments])

  const locked = readOnly || !state.loaded || state.status === 'closed'
  useEffect(() => {
    view?.dispatch({ effects: compartments.readOnly.reconfigure(locked ? [EditorState.readOnly.of(true), EditorView.editable.of(false)] : []) })
  }, [view, locked, compartments])

  const closedRef = useRef(onClosed)
  closedRef.current = onClosed
  useEffect(() => {
    if (state.status === 'closed') closedRef.current()
  }, [state.status])

  useChange(session, p)
  useTyping(session, p.typingIn, p.typingState)
  useMarks(view, p.highlights, p.focus, state.loaded, session.ytext.doc ?? undefined)

  const toolbar = p.toolbar !== false && !readOnly
  const minHeight = p.minHeight ?? 120
  const maxHeight = p.maxHeight
  return (
    <Box>
    {p.label && <Typography variant="caption" color="text.secondary">{p.label}</Typography>}
    <Box sx={{ border: 1, borderColor: 'divider', borderRadius: 1, bgcolor: 'background.paper', color: 'text.primary', '&:focus-within': { borderColor: 'primary.main' } }} data-doc-markdown={fileId}>
      {toolbar && <Toolbar view={view} disabled={locked} />}
      <Box ref={parent} sx={{ minHeight, maxHeight, overflow: 'auto', px: 1, '& .cm-editor': { minHeight }, '& .cm-editor.cm-focused': { outline: 'none' } }} />
      <Stack direction="row" spacing={1} sx={{ px: 1, py: 0.25, borderTop: 1, borderColor: 'divider', alignItems: 'center' }}>
        <Typography variant="caption" color={state.saveError ? 'error' : 'text.secondary'} sx={{ flex: 1 }} data-doc-status>
          {statusText(state.status, readOnly, state.saving, state.saveError, state.lastSavedAt)}
        </Typography>
        {state.peers.map((peer) => (
          <Tooltip key={peer.clientId} title={peer.name}>
            <Box sx={{ width: 10, height: 10, borderRadius: '50%', bgcolor: peer.color }} />
          </Tooltip>
        ))}
      </Stack>
    </Box>
    </Box>
  )
}

function statusText(status: string, readOnly: boolean, saving: boolean, error?: string, savedAt?: number): string {
  if (status === 'offline') return 'Offline: changes are kept and sent when the connection returns'
  if (status === 'connecting' || status === 'syncing') return 'Connecting…'
  if (readOnly) return 'Read-only'
  if (error) return error
  if (saving) return 'Saving…'
  if (savedAt) return 'Saved'
  return ''
}

// onChange, a second after the text stops changing
function useChange(session: CollabSession, p: MarkdownFieldProps) {
  const emit = useRef(p.onChange)
  emit.current = p.onChange
  const handles = !!p.onChange
  useEffect(() => {
    if (!handles) return
    let timer: ReturnType<typeof setTimeout> | undefined
    const observer = () => {
      clearTimeout(timer)
      timer = setTimeout(() => emit.current?.(session.ytext.toString()), 1000)
    }
    session.ytext.observe(observer)
    return () => {
      clearTimeout(timer)
      session.ytext.unobserve(observer)
    }
  }, [session, handles])
}

// How long after the last keystroke this window still counts as typing
const typingMs = 4000

// Presence in typingIn while shown, with typingState while this window
// typed (a local change to the text) in the last typingMs
function useTyping(session: CollabSession, object: string | undefined, state: Record<string, unknown> | undefined) {
  const stateKey = JSON.stringify(state ?? { typing: true })
  useEffect(() => {
    if (!object) return
    const typing = JSON.parse(stateKey) as Record<string, unknown>
    const held = live.holdPresence(object, {})
    let timer: ReturnType<typeof setTimeout> | undefined
    const observer = (_: unknown, tx: Y.Transaction) => {
      if (!tx.local) return
      held.update(typing)
      clearTimeout(timer)
      timer = setTimeout(() => held.update({}), typingMs)
    }
    session.ytext.observe(observer)
    return () => {
      clearTimeout(timer)
      session.ytext.unobserve(observer)
      held.release()
    }
  }, [session, object, stateKey])
}

// Wraps the selection in marks, or puts a prefix at the start of its lines
function wrap(view: EditorView, before: string, after = before) {
  const changes = view.state.changeByRange((range) => {
    const text = view.state.sliceDoc(range.from, range.to)
    return {
      changes: [{ from: range.from, insert: before }, { from: range.to, insert: after }],
      range: EditorSelection.range(range.from + before.length, range.from + before.length + text.length),
    }
  })
  view.dispatch(changes)
  view.focus()
}

function prefix(view: EditorView, mark: string) {
  const lines = new Set<number>()
  for (const r of view.state.selection.ranges) {
    for (let pos = r.from; pos <= r.to; ) {
      const line = view.state.doc.lineAt(pos)
      lines.add(line.from)
      pos = line.to + 1
    }
  }
  view.dispatch({ changes: [...lines].map((from) => ({ from, insert: mark })) })
  view.focus()
}

function Toolbar({ view, disabled }: { view?: EditorView; disabled: boolean }) {
  const buttons: [string, React.ReactNode, (v: EditorView) => void][] = [
    ['Bold', <FormatBoldIcon fontSize="small" />, (v) => wrap(v, '**')],
    ['Italic', <FormatItalicIcon fontSize="small" />, (v) => wrap(v, '_')],
    ['Heading', <TitleIcon fontSize="small" />, (v) => prefix(v, '## ')],
    ['List', <FormatListBulletedIcon fontSize="small" />, (v) => prefix(v, '- ')],
    ['Quote', <FormatQuoteIcon fontSize="small" />, (v) => prefix(v, '> ')],
    ['Code', <CodeIcon fontSize="small" />, (v) => wrap(v, '`')],
    ['Link', <LinkIcon fontSize="small" />, (v) => wrap(v, '[', '](https://)')],
  ]
  return (
    <Stack direction="row" sx={{ px: 0.5, borderBottom: 1, borderColor: 'divider' }}>
      {buttons.map(([label, icon, run]) => (
        <Tooltip key={label} title={label}>
          <span>
            <IconButton size="small" aria-label={label} disabled={disabled || !view} onMouseDown={(e) => e.preventDefault()} onClick={() => view && run(view)}>
              {icon}
            </IconButton>
          </span>
        </Tooltip>
      ))}
    </Stack>
  )
}

export default DocMarkdown

// The handlers a view's highlight extension calls, current on every render
function useMarkHandlers(p: Pick<MarkdownFieldProps, 'onSelected' | 'onHighlightClicked'>, ytext?: Y.Text) {
  const handlers = useRef<HighlightHandlers>({ clicked: () => {}, selected: () => {} })
  const view = useRef<{ text(): string }>({ text: () => '' })
  handlers.current = {
    clicked: (id) => p.onHighlightClicked?.(id),
    selected: (from, to) => {
      if (!p.onSelected) return
      const text = ytext ? ytext.toString() : view.current.text()
      p.onSelected(from === to ? { quote: '', headingPath: [], context: '', range: { from, to } } : describeSelection(text, from, to, ytext))
    },
  }
  return handlers
}

// Places the highlights when they change or the text first loads, and
// scrolls to the focused one
function useMarks(view: EditorView | undefined, highlights: Highlight[] | undefined, focus: MarkdownFieldProps['focus'], loaded: boolean, doc?: Y.Doc) {
  const key = JSON.stringify(highlights ?? [])
  useEffect(() => {
    if (!view || !loaded) return
    const list = JSON.parse(key) as Highlight[]
    view.dispatch({ effects: setHighlights.of({ want: list.length, place: (text) => placeHighlights(text, list, doc) }) })
  }, [view, key, loaded, doc])
  const focusKey = focus?.id ? `${focus.id}:${focus.n ?? 0}` : ''
  useEffect(() => {
    if (!view || !loaded || !focusKey) return
    const at = highlightRange(view, focusKey.slice(0, focusKey.lastIndexOf(':')))
    if (at) view.dispatch({ effects: EditorView.scrollIntoView(at.from, { y: 'center' }) })
  }, [view, focusKey, loaded, key])
}

// Text with no room, read-only: a version shown with its threads
function StaticDoc(p: { value: string; minHeight?: number; maxHeight?: number } & Pick<MarkdownFieldProps, 'highlights' | 'focus' | 'onSelected' | 'onHighlightClicked'>) {
  const theme = useTheme()
  const parent = useRef<HTMLDivElement>(null)
  const [view, setView] = useState<EditorView>()
  const [themeC] = useState(() => new Compartment())
  const textRef = useRef(p.value)
  textRef.current = p.value
  const handlers = useRef<HighlightHandlers>({ clicked: () => {}, selected: () => {} })
  handlers.current = {
    clicked: (id) => p.onHighlightClicked?.(id),
    selected: (from, to) => {
      if (!p.onSelected) return
      const text = view?.state.doc.toString() ?? textRef.current
      p.onSelected(from === to ? { quote: '', headingPath: [], context: '', range: { from, to } } : describeSelection(text, from, to))
    },
  }
  useEffect(() => {
    if (!parent.current) return
    const created = new EditorView({
      parent: parent.current,
      state: EditorState.create({
        doc: textRef.current,
        extensions: [markdown({ base: markdownLanguage }), livePreview({ revealSelection: false }), EditorView.lineWrapping,
          EditorState.readOnly.of(true), highlightExtension(handlers), themeC.of([])],
      }),
    })
    setView(created)
    return () => {
      created.destroy()
      setView(undefined)
    }
  }, [themeC])
  useEffect(() => {
    view?.dispatch({ effects: themeC.reconfigure(obsidianTheme(theme)) })
  }, [view, theme, themeC])
  useEffect(() => {
    if (view && view.state.doc.toString() !== p.value) view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: p.value } })
  }, [view, p.value])
  useMarks(view, p.highlights, p.focus, true, undefined)
  const minHeight = p.minHeight ?? 120
  return (
    <Box sx={{ border: 1, borderColor: 'divider', borderRadius: 1, bgcolor: 'background.paper', color: 'text.primary', px: 1 }} data-doc-markdown="static">
      <Box ref={parent} sx={{ minHeight, maxHeight: p.maxHeight, overflow: 'auto', '& .cm-editor': { minHeight }, '& .cm-editor.cm-focused': { outline: 'none' } }} />
    </Box>
  )
}
