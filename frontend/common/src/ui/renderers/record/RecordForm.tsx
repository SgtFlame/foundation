// record.form: edits one record, keeping what this user typed while the
// record follows everyone's changes (formMerge.ts). save sends only the
// fields this user changed; the prefab around it sends record.update.

import { useState } from 'react'
import { Alert, Box, Button, Stack, Typography } from '@mui/material'
import { live } from '../../../live/LiveConnection.ts'
import type { PresentUser } from '../../../live/usePresence.ts'
import type { RendererProps } from '../../points.ts'
import { isComputed, visibleFields, type Field, type RecordValues } from './fields.ts'
import { FieldInput } from './FieldEditor.tsx'
import { valueText } from './FieldValue.tsx'
import { changes, conflicts, edit, follow, keepMine, takeTheirs, type Edits } from './formMerge.ts'

const str = (v: unknown) => (v === undefined || v === null ? '' : String(v))

export function RecordForm(p: RendererProps) {
  const fields = visibleFields(p.props.fields as Field[] | undefined)
  const record = (p.props.record as RecordValues | null | undefined) ?? undefined
  const problems = (p.props.problems as Record<string, string> | undefined) ?? {}
  const users = (p.props.users as PresentUser[] | undefined) ?? []
  const entity = str(p.props.entity) || undefined
  const disabled = !!p.props.disabled
  const cancelLabel = p.props.cancelLabel === undefined ? 'Cancel' : str(p.props.cancelLabel)

  const [edits, setEdits] = useState<Edits>({})
  const [seen, setSeen] = useState(record)
  if (record !== seen) {
    // The record changed: a field someone else moved under this user's
    // edit is a conflict, named by who changed it now
    // A record's user ids are JSON numbers, which lose digits past 2^53
    // in the browser; parsed the same way, a presence entry's string id
    // rounds to the same number
    const sameUser = (a: unknown, b: unknown) => a !== undefined && a !== null && a !== '' && Number(a) === Number(b)
    const me = live.getHello()?.userId
    const nameOf = (id: string) =>
      sameUser(id, me) ? 'You, in another window' : (users.find((u) => sameUser(u.userId, id))?.name ?? (id ? 'Someone' : 'Someone else'))
    if (str(record?._seq) !== str(seen?._seq)) setEdits(follow(edits, seen, record, nameOf))
    setSeen(record)
  }

  if (!record) {
    return <Typography variant="body2" color="text.secondary">Loading…</Typography>
  }
  const open = conflicts(edits)
  const dirty = Object.keys(edits).length > 0
  return (
    <Stack spacing={2} sx={{ minWidth: 0 }}>
      {fields.map((f) => {
        const e = edits[f.id]
        return (
          <Box key={f.id}>
            <FieldInput field={f} value={e ? e.value : record[f.id]} problem={problems[f.id]} disabled={disabled || isComputed(f)}
              entity={entity} record={str(record._id)} onChange={(v) => setEdits((old) => edit(old, record, f.id, v))} />
            {e?.conflict && (
              <Alert severity="warning" sx={{ mt: 0.5 }} action={
                <Stack direction="row" spacing={1}>
                  <Button size="small" color="inherit" onClick={() => setEdits((old) => takeTheirs(old, f.id))}>Use theirs</Button>
                  <Button size="small" color="inherit" onClick={() => setEdits((old) => keepMine(old, f.id))}>Keep mine</Button>
                </Stack>
              }>
                {e.conflict.by} changed it to “{valueText(f, e.conflict.value) || 'nothing'}”
              </Alert>
            )}
          </Box>
        )
      })}
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
        <Button variant="contained" disabled={disabled || !dirty || open.length > 0}
          onClick={() => p.emit('save', { set: changes(edits), seq: record._seq })}>
          {str(p.props.saveLabel) || 'Save'}
        </Button>
        {cancelLabel && (
          <Button disabled={disabled} onClick={() => {
            setEdits({})
            p.emit('cancel')
          }}>
            {cancelLabel}
          </Button>
        )}
        {open.length > 0 && <Typography variant="body2" color="warning.main">Choose a value for each field changed by someone else first</Typography>}
      </Stack>
    </Stack>
  )
}
