// What the Log Explorer shows: a query over a time range, how it travels in
// a deep link, and stored rows turned into LogRecords for LogRow.

import { logLevels, type LogLevel, type LogRecord } from '../../logging/index.ts'
import type { LogRow, TimeRange } from '../../services/Logs.ts'

export type RangeSpec = { relative: string } | { start: string; end: string } // relative: a preset id

export const presets = [
  { id: '15m', label: 'Last 15 minutes', ms: 15 * 60_000 },
  { id: '1h', label: 'Last hour', ms: 60 * 60_000 },
  { id: '24h', label: 'Last 24 hours', ms: 24 * 60 * 60_000 },
  { id: '7d', label: 'Last 7 days', ms: 7 * 24 * 60 * 60_000 },
] as const

export const defaultRange = { relative: '1h' } satisfies RangeSpec

export interface LogsView {
  query: string
  range: RangeSpec
}

// The range as start and end, relative presets ending now
export function resolveRange(range: RangeSpec, now = Date.now()): Required<TimeRange> {
  if ('start' in range) return { start: range.start, end: range.end }
  const preset = presets.find((p) => p.id === range.relative) ?? presets[1]
  return { start: new Date(now - preset.ms).toISOString(), end: new Date(now).toISOString() }
}

export function rangeLabel(range: RangeSpec): string {
  if ('relative' in range) return presets.find((p) => p.id === range.relative)?.label ?? range.relative
  const fmt = (iso: string) => new Date(iso).toLocaleString(undefined, { hour12: false })
  return `${fmt(range.start)} – ${fmt(range.end)}`
}

// Deep links: /workbench?logs=<query>&range=1h, or &start=…&end=… for an absolute range
export function toSearch(view: LogsView): string {
  const params = new URLSearchParams({ logs: view.query })
  if ('relative' in view.range) params.set('range', view.range.relative)
  else {
    params.set('start', view.range.start)
    params.set('end', view.range.end)
  }
  return params.toString()
}

export function fromSearch(search: string): LogsView | undefined {
  const params = new URLSearchParams(search)
  const query = params.get('logs')
  if (query === null) return undefined
  const start = params.get('start')
  const end = params.get('end')
  const range: RangeSpec = start && end ? { start, end } : { relative: params.get('range') ?? defaultRange.relative }
  return { query, range }
}

// Adds a filter to a LogsQL query, before its first pipe
export function appendFilter(query: string, filter: string): string {
  const pipe = query.search(/\|/)
  const filters = (pipe < 0 ? query : query.slice(0, pipe)).trim()
  const pipes = pipe < 0 ? '' : ' ' + query.slice(pipe).trim()
  return `${!filters || filters === '*' ? '' : filters + ' '}${filter}${pipes}`
}

// A filter matching exactly field = value
export function exactFilter(field: string, value: string): string {
  if (field === 'tags') return `tags:${JSON.stringify(value)}` // stored as a JSON array
  return `${fieldName(field)}:=${JSON.stringify(value)}`
}

// A field name as LogsQL writes it: bare when it can be, quoted otherwise
export function fieldName(field: string): string {
  return /^[A-Za-z_][\w.]*$/.test(field) ? field : JSON.stringify(field)
}

// The fields of a stored record shown as LogRecord fields, not as extra columns
const recordFields = new Set([
  '_time', '_msg', '_stream', '_stream_id', 'level', 'domain', 'tags', 'code', 'trace', 'span', 'hint', 'public',
  'user.id', 'error', 'error.stack', 'error.cause', 'error.code', 'error.domain',
])

// A stored row as a LogRecord: attr.<key> fields become attrs, the error
// fields an error, and the stream fields attrs too
export function recordFromRow(row: LogRow, id: number): LogRecord {
  const attrs: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(row)) {
    if (recordFields.has(key)) continue
    attrs[key.startsWith('attr.') ? key.slice(5) : key] = value
  }
  let tags: string[] = []
  if (row.tags) {
    try {
      const parsed: unknown = JSON.parse(row.tags)
      tags = Array.isArray(parsed) ? parsed.map(String) : [row.tags]
    } catch {
      tags = [row.tags]
    }
  }
  const level = logLevels.includes(row.level as LogLevel) ? (row.level as LogLevel) : 'info'
  return {
    id,
    time: new Date(row._time ?? Date.now()),
    level,
    message: row._msg ?? '',
    domain: row.domain || undefined,
    tags,
    code: row.code || row['error.code'] || undefined,
    trace: row.trace || undefined,
    span: row.span || undefined,
    hint: row.hint || undefined,
    public: row.public || undefined,
    user: row['user.id'] ? { id: row['user.id'], attrs: {} } : undefined,
    attrs,
    error: row.error
      ? {
          name: 'Error',
          message: row.error,
          stack: row['error.stack'],
          cause: row['error.cause'] ? { name: 'Error', message: row['error.cause'] } : undefined,
        }
      : undefined,
  }
}

// The query for the records around a row in its stream
export function surroundingQuery(row: LogRow, count = 10): string | undefined {
  if (!row._stream_id || !row._time) return undefined
  return `_stream_id:${row._stream_id} _time:${row._time} _msg:=${JSON.stringify(row._msg ?? '')} | stream_context before ${count} after ${count}`
}
