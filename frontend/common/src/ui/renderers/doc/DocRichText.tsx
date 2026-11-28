// doc.richText (forge 35b-1): a record's richText field edited together.
// The field's collaboration room (Collab.openDoc, kind record) holds the
// document as a document file does, {format: 1, doc}; the room's writer
// saves it with record.setRichText a second after typing pauses. The view
// is the workbench's rich-text editor (editors/richtext).

import { useEffect, useRef, useState } from 'react'
import { Alert, Box, Typography } from '@mui/material'
import { EditorContent, useEditor } from '@tiptap/react'
import { Collab } from '../../../services/Collab.ts'
import { useCollabSession, useCollabState } from '../../../editors/markdown/collab/useCollabSession.ts'
import { richTextContent } from '../../../editors/richtext/content.ts'
import { baseExtensions } from '../../../editors/richtext/extensions.ts'
import { RichTextView, type RichTextMode, type RichTextSelection } from '../../../editors/richtext/RichTextView.tsx'
import type { SlashItem } from '../../../editors/richtext/blockOps.ts'
import type { BlockCommand } from '../../../editors/richtext/BlockChrome.tsx'
import { openWorkbenchLink } from '../../../workbench/links.ts'
import { useUiHost } from '../../UiHostContext.ts'
import type { RendererProps } from '../../points.ts'

const str = (v: unknown) => (v === undefined || v === null ? '' : String(v))

type Room = { fileId: string; readOnly: boolean } | { error: string }

// What a richText field's editor takes, from doc.richText's props or a
// record form's field
export interface RichTextFieldProps {
  entity: string
  id: string
  field: string
  mode?: RichTextMode
  toolbar?: boolean
  outline?: boolean
  placeholder?: string
  minHeight?: number
  maxHeight?: number
  disabled?: boolean
  label?: string
  onSaved?(at: number): void
  onLink?(href: string, text: string): boolean | Promise<boolean>
  onSelection?(s: RichTextSelection): void
  items?: SlashItem[]
  onCommand?(c: BlockCommand): void
}

export default function DocRichText(p: RendererProps) {
  const host = useUiHost()
  const emit = useRef(p.emit)
  emit.current = p.emit
  const handles = useRef(p.handles)
  handles.current = p.handles
  const mode = (['page', 'pageless', 'compact', 'blocks'].includes(str(p.props.mode)) ? str(p.props.mode) : 'pageless') as RichTextMode
  const minHeight = p.props.minHeight as number | undefined
  const maxHeight = p.props.maxHeight as number | undefined
  if (!str(p.props.entity) && !str(p.props.id) && p.props.value && typeof p.props.value === 'object') {
    return <StaticRichText value={p.props.value as Record<string, unknown>} minHeight={minHeight} maxHeight={maxHeight} />
  }
  return (
    <RichTextField entity={str(p.props.entity)} id={str(p.props.id)} field={str(p.props.field)} mode={mode}
      toolbar={p.props.toolbar !== false} outline={!!p.props.outline} placeholder={str(p.props.placeholder) || undefined}
      minHeight={minHeight} maxHeight={maxHeight} disabled={!!p.props.disabled}
      onSaved={(at) => handles.current('saved') && emit.current('saved', { at })}
      onSelection={(s) => handles.current('selection') && emit.current('selection', { ...s })}
      items={Array.isArray(p.props.items) ? (p.props.items as SlashItem[]) : undefined}
      onCommand={(c) => emit.current('command', { ...c })}
      onLink={async (href, text) => {
        if (href.startsWith('workbench:') && host.registry) return openWorkbenchLink(host.registry, href)
        if (!handles.current('linkClicked')) return false
        emit.current('linkClicked', { href, text })
        return true
      }} />
  )
}

export function RichTextField(p: RichTextFieldProps) {
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
  if (!room) return <Box sx={{ minHeight: p.minHeight ?? 160 }} />
  if ('error' in room) return <Alert severity="error" variant="outlined">{room.error}</Alert>
  return (
    <RoomEditor key={room.fileId} fileId={room.fileId} readOnly={room.readOnly || !!p.disabled} p={p}
      // A dropped room (the server restarted, or the room emptied) is opened again
      onClosed={() => setTimeout(() => setAttempt((a) => a + 1), 500)} />
  )
}

function RoomEditor({ fileId, readOnly, p, onClosed }: { fileId: string; readOnly: boolean; p: RichTextFieldProps; onClosed(): void }) {
  const session = useCollabSession(fileId, readOnly, richTextContent)
  const state = useCollabState(session)
  const closed = useRef(onClosed)
  closed.current = onClosed
  useEffect(() => {
    if (state.status === 'closed') closed.current()
  }, [state.status])
  const saved = useRef(p.onSaved)
  saved.current = p.onSaved
  useEffect(() => {
    if (state.lastSavedAt) saved.current?.(state.lastSavedAt)
  }, [state.lastSavedAt])
  return (
    <RichTextView session={session} readOnly={readOnly} mode={p.mode ?? 'compact'} toolbar={p.toolbar} outline={p.outline} placeholder={p.placeholder}
      minHeight={p.minHeight} maxHeight={p.maxHeight} label={p.label} onLink={p.onLink} onSelection={p.onSelection}
      items={p.items} onCommand={p.onCommand} />
  )
}

// A document shown read-only, such as a committed version
function StaticRichText({ value, minHeight, maxHeight }: { value: Record<string, unknown>; minHeight?: number; maxHeight?: number }) {
  const editor = useEditor({ extensions: baseExtensions(), editable: false, content: value }, [])
  const key = JSON.stringify(value)
  useEffect(() => {
    if (editor && !editor.isDestroyed) editor.commands.setContent(JSON.parse(key) as Record<string, unknown>, false)
  }, [editor, key])
  return (
    <Box sx={{ minHeight, maxHeight, overflow: 'auto', '& .ProseMirror': { outline: 'none' } }} data-doc-richtext-static>
      <EditorContent editor={editor} />
    </Box>
  )
}
