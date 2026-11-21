// doc.markdown (forge 07l): a record's markdown field edited together.
// The field's collaboration room (Collab.openDoc, kind record) is joined
// like a volume file's: one shared Y.Text, everyone's cursors in their
// colour, and the room's writer saving the field (record.update, as the
// writer) a second after typing pauses. The view is Foundation's Live
// Preview (CodeMirror 6) with a formatting toolbar.

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
}

export function DocMarkdown(p: RendererProps) {
  const emit = useRef(p.emit)
  emit.current = p.emit
  const onChange = useMemo(() => (value: string) => emit.current('change', { value }), [])
  return (
    <MarkdownField entity={str(p.props.entity)} id={str(p.props.id)} field={str(p.props.field)} placeholder={str(p.props.placeholder) || undefined}
      toolbar={p.props.toolbar !== false} minHeight={p.props.minHeight as number | undefined} maxHeight={p.props.maxHeight as number | undefined}
      disabled={!!p.props.disabled} onChange={p.handles('change') ? onChange : undefined} />
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
