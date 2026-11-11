// The input for one field of an entity type, chosen by its type
// (record.fieldEditor, and each field of record.form). Values go out in
// the record's format (records.CheckValue): decimals as strings, enums as
// option ids, dateTime in UTC, rich text as a ProseMirror document.

import { useState } from 'react'
import {
  Autocomplete,
  Box,
  Button,
  Checkbox,
  Chip,
  FormControl,
  FormControlLabel,
  FormHelperText,
  InputAdornment,
  InputLabel,
  MenuItem,
  Select,
  Stack,
  TextField,
  Typography,
} from '@mui/material'
import { useQuery } from '../../../live/useQuery.ts'
import { docFromText, plainText, Records, type FileRef, type RecordPage, type RichTextDoc } from '../../../services/Records.ts'
import type { RendererProps } from '../../points.ts'
import type { ConnectionList } from '../../../services/Connectors.ts'
import { isComputed, same, type Field } from './fields.ts'
import { FieldDisplay, titleOf } from './FieldValue.tsx'

const str = (v: unknown) => (v === undefined || v === null ? '' : String(v))

export interface FieldInputProps {
  field: Field
  value: unknown
  onChange(value: unknown): void
  // Text-like inputs call it when the user leaves them or presses Enter
  onCommit?(): void
  label?: string
  problem?: string
  disabled?: boolean
  entity?: string
  record?: string
}

// Text typed into a number, decimal or JSON field, kept while it does not
// parse yet, and replaced when the value changes from outside
function useText(value: unknown, show: (v: unknown) => string): [string, (t: string) => void] {
  const [text, setText] = useState(() => show(value))
  const [seen, setSeen] = useState(value)
  if (!same(value, seen)) {
    setSeen(value)
    setText(show(value))
  }
  return [text, setText]
}

function NumberInput({ field, value, onChange, onCommit, label, problem, disabled, decimal }: FieldInputProps & { decimal: boolean }) {
  const [text, setText] = useText(value, str)
  const [bad, setBad] = useState(false)
  return (
    <TextField size="small" fullWidth label={label} value={text} disabled={disabled} error={!!problem || bad}
      helperText={problem || (bad ? 'Not a number' : undefined)}
      slotProps={{ htmlInput: { inputMode: 'decimal' }, input: field.currency ? { endAdornment: <InputAdornment position="end">{field.currency}</InputAdornment> } : undefined }}
      onChange={(e) => {
        const t = e.target.value.trim()
        setText(e.target.value)
        if (t === '') {
          setBad(false)
          onChange(null)
          return
        }
        const ok = /^-?\d+(\.\d*)?$/.test(t)
        setBad(!ok)
        if (ok) onChange(decimal ? t : Number(t))
      }}
      onBlur={onCommit} onKeyDown={(e) => e.key === 'Enter' && onCommit?.()} />
  )
}

function JsonInput({ value, onChange, onCommit, label, problem, disabled }: FieldInputProps) {
  const [text, setText] = useText(value, (v) => (v === undefined || v === null ? '' : JSON.stringify(v, null, 2)))
  const [bad, setBad] = useState<string>()
  return (
    <TextField size="small" fullWidth multiline minRows={2} label={label} value={text} disabled={disabled} error={!!problem || !!bad}
      helperText={problem || bad} slotProps={{ htmlInput: { style: { fontFamily: 'monospace' } } }}
      onChange={(e) => {
        setText(e.target.value)
        if (e.target.value.trim() === '') {
          setBad(undefined)
          onChange(null)
          return
        }
        try {
          onChange(JSON.parse(e.target.value))
          setBad(undefined)
        } catch (err) {
          setBad((err as Error).message)
        }
      }}
      onBlur={onCommit} />
  )
}

// The local time a dateTime input shows for a UTC value, and back
function toLocal(v: unknown): string {
  if (!v) return ''
  const d = new Date(str(v))
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// A relation's choices: the target type's records, named by its title field
function RelationInput({ field, value, onChange, label, problem, disabled }: FieldInputProps) {
  const target = field.relation?.entity ?? ''
  const many = !!field.relation?.many
  const toUsers = target === 'user'
  const { data: type } = useQuery<{ entity?: { titleField?: string } }>(toUsers ? undefined : 'entity.describe', { id: target, published: true })
  const { data: page } = useQuery<RecordPage>(toUsers ? undefined : 'records.find', { entity: target, limit: 200 })
  const titleField = type?.entity?.titleField
  const options = (page?.records ?? []).map((r) => ({ id: str(r._id), label: str(titleField ? r[titleField] : '') || titleOf(r) }))
  const labelOf = (id: string) => options.find((o) => o.id === id)?.label ?? id
  const ids = many ? ((value as string[] | null) ?? []) : value ? [str(value)] : []
  return (
    <Autocomplete size="small" multiple={many} freeSolo={toUsers} disabled={disabled}
      options={options.map((o) => o.id)} getOptionLabel={(id) => labelOf(String(id))}
      value={many ? ids : (ids[0] ?? null)}
      onChange={(_, v) => onChange(many ? ((v as string[]) ?? []) : ((v as string | null) ?? null))}
      renderValue={many ? (vals, getItemProps) => (Array.isArray(vals) ? vals : [vals]).map((id, index) => <Chip size="small" label={labelOf(String(id))} {...getItemProps({ index })} key={String(id)} />) : undefined}
      renderInput={(params) => <TextField {...params} label={label} error={!!problem} helperText={problem || (toUsers ? 'User ids' : undefined)} />} />
  )
}

function FileInput({ field, value, onChange, label, problem, disabled, entity, record }: FieldInputProps) {
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState<string>()
  const file = value as FileRef | null
  return (
    <FormControl error={!!problem || !!failed} fullWidth>
      <Typography variant="caption" color="text.secondary">{label}</Typography>
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
        {file ? <FieldDisplay field={field} value={file} entity={entity} record={record} /> : <Typography variant="body2" color="text.secondary">No file</Typography>}
        <Button size="small" component="label" disabled={disabled || busy || !entity}>
          {file ? 'Replace…' : 'Upload…'}
          <input type="file" hidden onChange={(e) => {
            const f = e.target.files?.[0]
            if (!f || !entity) return
            setBusy(true)
            setFailed(undefined)
            Records.upload(entity, f).then((r) => r.file && onChange(r.file), (err) => setFailed((err as Error).message)).finally(() => setBusy(false))
          }} />
        </Button>
        {file && <Button size="small" disabled={disabled} onClick={() => onChange(null)}>Remove</Button>}
      </Stack>
      {(problem || failed) && <FormHelperText>{problem || failed}</FormHelperText>}
    </FormControl>
  )
}

function MoneyInput(props: FieldInputProps) {
  const { field, value, onChange, label, problem, disabled } = props
  if (field.currency) return <NumberInput {...props} decimal />
  const m = (value as { amount?: string; currency?: string } | null) ?? {}
  const set = (amount: unknown, currency: string) => onChange(amount === null && !currency ? null : { amount, currency })
  return (
    <Stack direction="row" spacing={1}>
      <NumberInput {...props} decimal value={m.amount ?? null} onChange={(a) => set(a, m.currency ?? '')} />
      <TextField size="small" label="Currency" value={m.currency ?? ''} disabled={disabled} error={!!problem} sx={{ width: 110 }}
        onChange={(e) => set(m.amount ?? null, e.target.value.toUpperCase())} slotProps={{ htmlInput: { maxLength: 3, 'aria-label': `${label} currency` } }} />
    </Stack>
  )
}

export function FieldInput(props: FieldInputProps) {
  const { field, value, onChange, onCommit, problem, disabled } = props
  const label = props.label ?? field.name
  const p = { ...props, label }
  if (isComputed(field)) {
    return (
      <Box>
        <Typography variant="caption" color="text.secondary">{label}</Typography>
        <FieldDisplay field={field} value={value} entity={props.entity} record={props.record} />
      </Box>
    )
  }
  const text = (multiline: boolean, shown: string, set: (t: string) => unknown) => (
    <TextField size="small" fullWidth label={label} value={shown} multiline={multiline} minRows={multiline ? 3 : undefined}
      disabled={disabled} error={!!problem} helperText={problem} required={field.required}
      onChange={(e) => onChange(set(e.target.value))} onBlur={onCommit}
      onKeyDown={(e) => e.key === 'Enter' && (!multiline || e.ctrlKey || e.metaKey) && onCommit?.()} />
  )
  switch (field.type) {
    case 'text':
    case 'duration':
      return text(false, str(value), (t) => (t === '' ? null : t))
    case 'longText':
      return text(true, str(value), (t) => (t === '' ? null : t))
    case 'richText':
      return text(true, plainText(value as RichTextDoc), (t) => (t === '' ? null : docFromText(t)))
    case 'number':
      return <NumberInput {...p} decimal={false} />
    case 'decimal':
      return <NumberInput {...p} decimal />
    case 'money':
      return <MoneyInput {...p} />
    case 'bool':
      return (
        <FormControl error={!!problem}>
          <FormControlLabel label={label} control={<Checkbox size="small" checked={value === true} disabled={disabled} onChange={(_, c) => onChange(c)} />} />
          {problem && <FormHelperText>{problem}</FormHelperText>}
        </FormControl>
      )
    case 'date':
      return (
        <TextField size="small" type="date" fullWidth label={label} value={str(value)} disabled={disabled} error={!!problem} helperText={problem}
          slotProps={{ inputLabel: { shrink: true } }} onChange={(e) => onChange(e.target.value || null)} />
      )
    case 'dateTime':
      return (
        <TextField size="small" type="datetime-local" fullWidth label={label} value={toLocal(value)} disabled={disabled} error={!!problem} helperText={problem}
          slotProps={{ inputLabel: { shrink: true } }} onChange={(e) => onChange(e.target.value ? new Date(e.target.value).toISOString() : null)} />
      )
    case 'enum':
    case 'multiEnum': {
      const many = field.type === 'multiEnum'
      const id = `field-${field.id}`
      return (
        <FormControl size="small" fullWidth error={!!problem} disabled={disabled}>
          <InputLabel id={id}>{label}</InputLabel>
          <Select labelId={id} label={label} multiple={many} value={many ? ((value as string[]) ?? []) : str(value)}
            onChange={(e) => onChange(many ? e.target.value : e.target.value || null)}>
            {!many && <MenuItem value=""><em>None</em></MenuItem>}
            {(field.options ?? []).map((o) => <MenuItem key={o.id} value={o.id}>{o.name}</MenuItem>)}
          </Select>
          {problem && <FormHelperText>{problem}</FormHelperText>}
        </FormControl>
      )
    }
    case 'tags':
      return (
        <Autocomplete size="small" multiple freeSolo options={[]} disabled={disabled} value={(value as string[]) ?? []}
          onChange={(_, v) => onChange(v.map(String))}
          renderInput={(params) => <TextField {...params} label={label} error={!!problem} helperText={problem || 'Press Enter after each tag'} />} />
      )
    case 'relation':
      return <RelationInput {...p} />
    case 'user':
      return text(false, str(value), (t) => (t.trim() === '' ? null : t.trim()))
    case 'file':
      return <FileInput {...p} />
    case 'connection':
      return <ConnectionInput {...p} />
  }
  // json, list, object, volumeFile and any type to come
  return <JsonInput {...p} />
}

// record.fieldEditor: on its own, a text-like field sends change when the
// user leaves it or presses Enter, the others at once
export function RecordFieldEditor(p: RendererProps) {
  const field = p.props.field as Field | undefined
  const server = p.props.value
  const [own, setOwn] = useState<unknown>(server)
  const [seen, setSeen] = useState<unknown>(server)
  if (!same(server, seen)) {
    setSeen(server)
    setOwn(server)
  }
  if (!field) return null
  const textual = ['text', 'longText', 'richText', 'number', 'decimal', 'money', 'duration', 'user', 'json', 'list', 'object', 'volumeFile'].includes(field.type)
  return (
    <FieldInput field={field} value={own} label={p.props.label as string | undefined} problem={str(p.props.problem) || undefined}
      disabled={!!p.props.disabled} entity={str(p.props.entity) || undefined}
      onChange={(v) => {
        setOwn(v)
        if (!textual) p.emit('change', { value: v })
      }}
      onCommit={() => !same(own, server) && p.emit('change', { value: own })} />
  )
}

// A connection field picks among the connections the user may read, of
// types implementing the field's contract when it names one (forge 07c);
// the value is only the id.
function ConnectionInput({ field, value, onChange, label, problem, disabled }: FieldInputProps) {
  const { data } = useQuery<ConnectionList>('connections.list', field.contract ? { contract: field.contract } : {})
  const id = `field-${field.id}`
  const list = data?.connections ?? []
  const missing = !!value && !list.some((c) => c.id === value)
  return (
    <FormControl size="small" fullWidth error={!!problem || missing} disabled={disabled}>
      <InputLabel id={id}>{label}</InputLabel>
      <Select labelId={id} label={label} value={str(value)} onChange={(e) => onChange(e.target.value || null)}>
        <MenuItem value=""><em>None</em></MenuItem>
        {list.map((c) => (
          <MenuItem key={c.id} value={c.id}>
            {c.name}
            {c.status !== 'ok' && <Chip size="small" label={c.status} color={c.status === 'error' ? 'error' : 'default'} sx={{ ml: 1 }} />}
          </MenuItem>
        ))}
        {missing && <MenuItem value={str(value)}>Connection {str(value)}</MenuItem>}
      </Select>
      {(problem || missing) && <FormHelperText>{problem || 'This connection is gone, or you may not see it'}</FormHelperText>}
    </FormControl>
  )
}
