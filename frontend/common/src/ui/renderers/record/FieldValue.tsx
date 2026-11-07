// A record field's value shown by its type (record.fieldValue, and the
// cells of a mui.table column with a recordField)

import { Box, Chip, Link, Stack, Typography } from '@mui/material'
import { fileUrl, plainText, type FileRef, type RichTextDoc } from '../../../services/Records.ts'
import type { RendererProps } from '../../points.ts'
import { shownAs, type Field } from './fields.ts'

const str = (v: unknown) => (v === undefined || v === null ? '' : String(v))

// A related record's name: its _title from the query's expand, else its id
export function titleOf(r: unknown): string {
  if (r && typeof r === 'object') {
    const o = r as Record<string, unknown>
    return str(o._title ?? o.displayName ?? o._id)
  }
  return str(r)
}

function dateOnly(s: string): string {
  const [y, m, d] = s.split('-').map(Number)
  return y ? new Date(y, (m ?? 1) - 1, d ?? 1).toLocaleDateString() : s
}

function money(amount: unknown, currency: string | undefined): string {
  const n = Number(amount)
  if (!currency || Number.isNaN(n)) return str(amount)
  try {
    return new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(n)
  } catch {
    return `${str(amount)} ${currency}`
  }
}

function chips(labels: string[]) {
  return (
    <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap', alignSelf: 'flex-start' }}>
      {labels.map((l, i) => <Chip key={`${l}-${i}`} size="small" label={l} />)}
    </Stack>
  )
}

export interface FieldDisplayProps {
  field: Field
  value: unknown
  expanded?: unknown // a relation's related record or records
  entity?: string
  record?: string
}

// The value as text, for places that need a string (a title)
export function valueText(field: Field, value: unknown, expanded?: unknown): string {
  const f = shownAs(field)
  if (value === undefined || value === null) return ''
  switch (f.type) {
    case 'bool':
      return value ? 'Yes' : 'No'
    case 'date':
      return dateOnly(str(value))
    case 'dateTime':
      return new Date(str(value)).toLocaleString()
    case 'money':
      return typeof value === 'object' ? money((value as Record<string, unknown>).amount, str((value as Record<string, unknown>).currency)) : money(value, f.currency)
    case 'number':
      return typeof value === 'number' ? value.toLocaleString() : str(value)
    case 'enum':
      return f.options?.find((o) => o.id === value)?.name ?? str(value)
    case 'multiEnum':
      return (value as string[]).map((v) => f.options?.find((o) => o.id === v)?.name ?? v).join(', ')
    case 'tags':
      return (value as string[]).join(', ')
    case 'relation':
      if (Array.isArray(expanded)) return expanded.map(titleOf).join(', ')
      if (expanded) return titleOf(expanded)
      return Array.isArray(value) ? value.join(', ') : str(value)
    case 'user':
      return expanded ? titleOf(expanded) : `User ${str(value)}`
    case 'richText':
      return plainText(value as RichTextDoc)
    case 'file':
      return (value as FileRef).name ?? ''
    case 'text':
    case 'longText':
    case 'decimal':
    case 'duration':
      return str(value)
  }
  return typeof value === 'object' ? JSON.stringify(value) : str(value)
}

export function FieldDisplay({ field, value, expanded, entity, record }: FieldDisplayProps) {
  const f = shownAs(field)
  if (value === undefined || value === null || (Array.isArray(value) && value.length === 0)) {
    return <Typography variant="body2" color="text.secondary">—</Typography>
  }
  switch (f.type) {
    case 'enum': {
      const o = f.options?.find((x) => x.id === value)
      return <Chip size="small" label={o?.name ?? str(value)} sx={{ alignSelf: 'flex-start' }} />
    }
    case 'multiEnum':
      return chips((value as string[]).map((v) => f.options?.find((o) => o.id === v)?.name ?? v))
    case 'tags':
      return chips(value as string[])
    case 'relation':
      if (f.relation?.many || Array.isArray(value)) {
        const list = Array.isArray(expanded) ? expanded.map(titleOf) : (value as string[])
        return chips(list)
      }
      return <Typography variant="body2">{valueText(field, value, expanded)}</Typography>
    case 'file': {
      const file = value as FileRef
      if (entity && record) return <Link href={fileUrl(entity, record, field.id, file.id)} target="_blank" rel="noopener" variant="body2">{file.name}</Link>
      return <Typography variant="body2">{file.name}</Typography>
    }
    case 'longText':
    case 'richText':
      return <Typography variant="body2" sx={{ whiteSpace: 'pre-wrap' }}>{valueText(field, value)}</Typography>
    case 'json':
    case 'list':
    case 'object':
    case 'volumeFile':
      return (
        <Box component="pre" sx={{ m: 0, typography: 'body2', fontFamily: 'monospace', whiteSpace: 'pre-wrap' }}>
          {JSON.stringify(value, null, 2)}
        </Box>
      )
  }
  return <Typography variant="body2">{valueText(field, value, expanded)}</Typography>
}

export function RecordFieldValue(p: RendererProps) {
  const field = p.props.field as Field | undefined
  if (!field) return null
  return <FieldDisplay field={field} value={p.props.value} expanded={p.props.expanded} entity={str(p.props.entity) || undefined} record={str(p.props.record) || undefined} />
}
