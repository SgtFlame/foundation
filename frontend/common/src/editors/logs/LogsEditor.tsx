import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Alert,
  Autocomplete,
  Box,
  Button,
  CircularProgress,
  Divider,
  MenuItem,
  Select,
  Stack,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Tooltip,
  Typography,
} from '@mui/material'
import PauseIcon from '@mui/icons-material/Pause'
import PlayArrowIcon from '@mui/icons-material/PlayArrow'
import SensorsIcon from '@mui/icons-material/Sensors'
import VerticalAlignTopIcon from '@mui/icons-material/VerticalAlignTop'
import { log } from '../../logging/index.ts'
import {
  Logs,
  QueryServiceError,
  type HitsResponse,
  type LogRow,
  type QueryError,
  type StatusResponse,
  type ValueHits,
} from '../../services/Logs.ts'
import BuilderPanel from './BuilderPanel.tsx'
import { emptyBuilder, fromLogsQL, newRow, toLogsQL, type Builder } from './builder.ts'
import Histogram from './Histogram.tsx'
import type { LogsObject } from './LogsObject.ts'
import { useWorkbenchSubject } from '../../workbench/context/WorkbenchContext.ts'
import type { EditorProps } from '../../workbench/points.ts'
import type { CompletionSource } from './logsql.ts'
import QueryEditor from './QueryEditor.tsx'
import { isRecords, RecordList, StatsTable } from './Results.tsx'
import {
  appendFilter,
  defaultRange,
  exactFilter,
  presets,
  rangeLabel,
  resolveRange,
  toSearch,
  type LogsView,
  type RangeSpec,
} from './view.ts'

const logsLog = log.In('logs').Tags('explorer')
const maxTailRows = 2000
const defaultColumns: string[] = []

type Mode = 'builder' | 'logsql'

interface Results {
  rows: LogRow[]
  limited: boolean
  hits?: HitsResponse
  start: string
  end: string
}

// datetime-local inputs take local time without a zone
const toLocalInput = (iso: string) => {
  const d = new Date(iso)
  return new Date(d.getTime() - d.getTimezoneOffset() * 60_000).toISOString().slice(0, 16)
}

function RangePicker({ range, onChange }: { range: RangeSpec; onChange: (range: RangeSpec) => void }) {
  const absolute = 'start' in range
  return (
    <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
      <Select
        size="small"
        value={absolute ? 'absolute' : range.relative}
        onChange={(e) => {
          const v = e.target.value
          onChange(v === 'absolute' ? resolveRange(range) : { relative: v })
        }}
        sx={{ fontSize: 12, '& .MuiSelect-select': { py: '4px' } }}
        aria-label="Time range"
      >
        {presets.map((p) => (
          <MenuItem key={p.id} value={p.id} sx={{ fontSize: 12 }}>
            {p.label}
          </MenuItem>
        ))}
        <MenuItem value="absolute" sx={{ fontSize: 12 }}>
          Absolute range
        </MenuItem>
      </Select>
      {absolute && (
        <>
          <TextField
            type="datetime-local"
            size="small"
            value={toLocalInput(range.start)}
            onChange={(e) => e.target.value && onChange({ ...range, start: new Date(e.target.value).toISOString() })}
            sx={{ '& input': { py: '4px', fontSize: 12 } }}
            aria-label="From"
          />
          <Typography variant="caption">to</Typography>
          <TextField
            type="datetime-local"
            size="small"
            value={toLocalInput(range.end)}
            onChange={(e) => e.target.value && onChange({ ...range, end: new Date(e.target.value).toISOString() })}
            sx={{ '& input': { py: '4px', fontSize: 12 } }}
            aria-label="To"
          />
        </>
      )}
    </Stack>
  )
}

// Searches the stored logs (documentation/plans/Logging.md › The Log Explorer)
function LogsEditor({ object, tabId }: EditorProps<LogsObject>) {
  const initial: LogsView = object.view ?? { query: '*', range: defaultRange }
  const initialBuilder = fromLogsQL(initial.query)
  const [mode, setMode] = useState<Mode>(initialBuilder ? 'builder' : 'logsql')
  const [builder, setBuilder] = useState<Builder>(initialBuilder ?? emptyBuilder)
  const [text, setText] = useState(initial.query)
  const [unreadable, setUnreadable] = useState(false) // LogsQL the builder cannot show
  const [range, setRange] = useState<RangeSpec>(initial.range)
  const [status, setStatus] = useState<StatusResponse>()
  const [results, setResults] = useState<Results>()
  const [error, setError] = useState<{ message: string; queryError?: QueryError }>()
  const [loading, setLoading] = useState(false)
  const [names, setNames] = useState<ValueHits[]>([])
  const [streamValues, setStreamValues] = useState<Record<string, ValueHits[]>>({})
  const [columns, setColumns] = useState<string[]>(defaultColumns)
  const [tailing, setTailing] = useState(false)
  const [paused, setPaused] = useState(false)
  const [follow, setFollow] = useState(true)
  const [buffered, setBuffered] = useState<LogRow[]>([])
  const pausedRef = useRef(paused)
  pausedRef.current = paused

  // The query as it runs: the builder's in builder mode
  const query = mode === 'builder' && !unreadable ? toLogsQL(builder) : text
  useWorkbenchSubject(tabId, { type: 'logQuery', id: object.id, label: query.slice(0, 120), summary: `Log Explorer, ${rangeLabel(range)}` })

  useEffect(() => {
    Logs.status()
      .then(setStatus)
      .catch((err: unknown) => logsLog.fromError(err).warn('cannot read the log storage status'))
  }, [])

  // Completion over the current range, cached until the next run
  const cache = useRef(new Map<string, Promise<ValueHits[]>>())
  const resolvedRef = useRef(resolveRange(range))
  const cached = useCallback((key: string, load: () => Promise<ValueHits[]>) => {
    let p = cache.current.get(key)
    if (!p) {
      p = load().catch(() => [])
      cache.current.set(key, p)
    }
    return p
  }, [])
  const loadNames = useCallback(
    () => cached('names', () => Logs.fieldNames({ query: '*', ...resolvedRef.current }).then((r) => r.names)),
    [cached],
  )
  const completion: CompletionSource = useMemo(
    () => ({
      fieldNames: () => loadNames().then((n) => n.map((x) => x.value)),
      fieldValues: (field) => cached(`values:${field}`, () => Logs.fieldValues({ query: '*', field, ...resolvedRef.current }).then((r) => r.values)),
      streamValues: (field) => cached(`stream:${field}`, () => Logs.streams({ query: '*', field, ...resolvedRef.current }).then((r) => r.values)),
    }),
    [cached, loadNames],
  )

  const run = useCallback(
    async (q: string, r: RangeSpec) => {
      const resolved = resolveRange(r)
      resolvedRef.current = resolved
      cache.current.clear()
      setLoading(true)
      setError(undefined)
      history.replaceState(null, '', `/workbench?${toSearch({ query: q, range: r })}`)
      try {
        const [rows, hits] = await Promise.all([
          Logs.query({ query: q, ...resolved }),
          Logs.hits({ query: q.split('|')[0] || '*', field: 'level', ...resolved }).catch(() => undefined),
        ])
        setResults({ rows: rows.rows, limited: rows.limited ?? false, hits, ...resolved })
      } catch (err) {
        const queryError = err instanceof QueryServiceError ? err.queryError : undefined
        setError({ message: err instanceof Error ? err.message : String(err), queryError })
        setResults(undefined)
        if (!(err instanceof QueryServiceError)) logsLog.fromError(err).warn('log query failed')
      } finally {
        setLoading(false)
      }
      void loadNames().then(setNames)
      const values: Record<string, ValueHits[]> = {}
      await Promise.all(
        emptyBuilder.streams.map(async ({ field }) => {
          values[field] = await completion.streamValues(field)
        }),
      )
      setStreamValues(values)
    },
    [completion, loadNames],
  )

  // Run once on open, when storage is on
  const started = useRef(false)
  useEffect(() => {
    if (status?.configured && !started.current) {
      started.current = true
      void run(query, range)
    }
  }, [status, run, query, range])

  // Live tail: new records go on top until it is turned off
  useEffect(() => {
    if (!tailing) return
    const controller = new AbortController()
    Logs.tail(
      { query },
      {
        signal: controller.signal,
        onRow: (row) => {
          if (pausedRef.current) setBuffered((b) => [row, ...b].slice(0, maxTailRows))
          else setResults((r) => r && { ...r, rows: [row, ...r.rows].slice(0, maxTailRows) })
        },
      },
    ).catch((err: unknown) => {
      const queryError = err instanceof QueryServiceError ? err.queryError : undefined
      setError({ message: `Live tail stopped: ${err instanceof Error ? err.message : String(err)}`, queryError })
      setTailing(false)
    })
    return () => controller.abort()
    // Restarts when the query runs again, not on every keystroke
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tailing, results?.start])

  const resume = () => {
    setPaused(false)
    setResults((r) => r && { ...r, rows: [...buffered, ...r.rows].slice(0, maxTailRows) })
    setBuffered([])
  }

  const switchMode = (next: Mode | null) => {
    if (!next || next === mode) return
    if (next === 'logsql') {
      setText(query)
      setUnreadable(false)
    } else {
      const parsed = fromLogsQL(text)
      if (parsed) setBuilder(parsed)
      setUnreadable(!parsed)
    }
    setMode(next)
  }

  // Replaces the query and runs it, in the builder when it can show it
  const runQuery = (q: string, r = range) => {
    const parsed = fromLogsQL(q)
    setText(q)
    if (mode === 'builder' && parsed) {
      setBuilder(parsed)
      setUnreadable(false)
    } else if (mode === 'builder') setMode('logsql')
    void run(q, r)
  }

  const onFilter = (field: string, value: string, exclude = false) => {
    if (mode === 'builder' && !unreadable) {
      const row = { ...newRow(field, field === 'tags' ? 'contains' : 'equals', value), not: exclude }
      const next = { ...builder, groups: [...builder.groups, { join: 'and' as const, rows: [row] }] }
      setBuilder(next)
      void run(toLogsQL(next), range)
    } else {
      runQuery(appendFilter(text, `${exclude ? 'NOT ' : ''}${exactFilter(field, value)}`))
    }
  }

  const fieldOptions = names.map((n) => n.value).filter((n) => !n.startsWith('_'))
  const records = results ? isRecords(results.rows) : true

  return (
    <Stack sx={{ height: '100%', minHeight: 0 }}>
      <Stack direction="row" spacing={1} useFlexGap sx={{ alignItems: 'center', flexWrap: 'wrap', p: 1, pb: 0 }}>
        <RangePicker range={range} onChange={setRange} />
        <ToggleButtonGroup exclusive size="small" value={mode} onChange={(_, m: Mode | null) => switchMode(m)} aria-label="Query mode">
          <ToggleButton value="builder" sx={{ py: 0.25, fontSize: 11, textTransform: 'none' }}>
            Builder
          </ToggleButton>
          <ToggleButton value="logsql" sx={{ py: 0.25, fontSize: 11, textTransform: 'none' }}>
            LogsQL
          </ToggleButton>
        </ToggleButtonGroup>
        <Button
          size="small"
          variant="contained"
          onClick={() => void run(query, range)}
          disabled={loading || status?.configured === false}
          startIcon={loading ? <CircularProgress size={14} /> : undefined}
        >
          Run
        </Button>
        <Tooltip title={tailing ? 'Stop the live tail' : 'Follow new records as they arrive'}>
          <span>
            <ToggleButton
              value="tail"
              size="small"
              selected={tailing}
              disabled={status?.configured === false}
              onChange={() => {
                setTailing((t) => !t)
                setPaused(false)
                setBuffered([])
              }}
              sx={{ py: 0.25, fontSize: 11, textTransform: 'none', gap: 0.5 }}
            >
              <SensorsIcon sx={{ fontSize: 16 }} /> Live tail
            </ToggleButton>
          </span>
        </Tooltip>
        {tailing && (
          <>
            <Button size="small" startIcon={paused ? <PlayArrowIcon /> : <PauseIcon />} onClick={paused ? resume : () => setPaused(true)}>
              {paused ? `Resume${buffered.length ? ` (${buffered.length} new)` : ''}` : 'Pause'}
            </Button>
            <ToggleButton value="follow" size="small" selected={follow} onChange={() => setFollow((f) => !f)} sx={{ py: 0.25, fontSize: 11, textTransform: 'none', gap: 0.5 }}>
              <VerticalAlignTopIcon sx={{ fontSize: 16 }} /> Auto-scroll
            </ToggleButton>
          </>
        )}
        <Box sx={{ flex: 1 }} />
        {status?.configured && (
          <Typography variant="caption" sx={{ color: 'text.secondary' }}>
            {rangeLabel(range)} · {status.url}
          </Typography>
        )}
      </Stack>

      {mode === 'builder' && !unreadable ? (
        <BuilderPanel builder={builder} onChange={setBuilder} names={names} streamValues={streamValues} completion={completion} />
      ) : (
        <Box sx={{ p: 1 }}>
          {unreadable && (
            <Alert
              severity="info"
              sx={{ mb: 1, py: 0 }}
              action={
                <Button
                  size="small"
                  onClick={() => {
                    setBuilder(emptyBuilder)
                    setText(toLogsQL(emptyBuilder))
                    setUnreadable(false)
                  }}
                >
                  Reset
                </Button>
              }
            >
              Edited as LogsQL: the builder cannot show this query. Reset it to start over in the builder.
            </Alert>
          )}
          <Box sx={{ border: 1, borderColor: 'divider' }}>
            <QueryEditor value={text} onChange={setText} onRun={() => void run(text, range)} error={error?.queryError} completion={completion} />
          </Box>
          <Typography variant="caption" sx={{ color: 'text.secondary' }}>
            Ctrl+Enter runs the query. Time is set by the range, not in the query.
          </Typography>
        </Box>
      )}
      {mode === 'builder' && !unreadable && (
        <Typography variant="caption" sx={{ px: 1, fontFamily: 'monospace', color: 'text.secondary', wordBreak: 'break-all' }}>
          {query}
        </Typography>
      )}

      {status && !status.configured && (
        <Alert severity="info" sx={{ m: 1 }}>
          Log storage is not configured, so logs go only to the server's stderr. Run backend/scripts/victorialogs.sh and turn
          it on in Settings › System › Logging.
        </Alert>
      )}
      {status?.lastError && (
        <Alert severity="warning" sx={{ m: 1 }}>
          Storing logs is failing ({status.lastError}); {status.queued} records are waiting
          {status.dropped ? `, ${status.dropped} dropped` : ''}.
        </Alert>
      )}
      {error && (
        <Alert severity="error" sx={{ m: 1 }} onClose={() => setError(undefined)}>
          {error.message}
        </Alert>
      )}

      {results && (
        <>
          {records && results.hits && results.hits.series.length > 0 && (
            <Histogram
              series={results.hits.series}
              stepMs={results.hits.stepMs}
              start={results.start}
              end={results.end}
              onSelect={(start, end) => {
                const r = { start, end }
                setRange(r)
                void run(query, r)
              }}
            />
          )}
          <Divider />
          <Stack direction="row" spacing={1} sx={{ alignItems: 'center', px: 1, py: 0.5 }}>
            <Typography variant="caption" sx={{ color: 'text.secondary', flex: 1 }}>
              {results.rows.length.toLocaleString()} {records ? 'records, newest first' : 'rows'}
              {results.limited ? ' (limited: narrow the query or the range to see the rest)' : ''}
            </Typography>
            {records && (
              <Autocomplete
                multiple
                size="small"
                options={fieldOptions}
                value={columns}
                onChange={(_, v) => setColumns(v)}
                sx={{ minWidth: 240, '& .MuiInputBase-root': { fontSize: 12 } }}
                renderInput={(params) => <TextField {...params} placeholder="Columns" />}
              />
            )}
          </Stack>
          {records ? (
            <RecordList
              rows={results.rows}
              columns={columns}
              follow={tailing && follow && !paused}
              onFilter={onFilter}
              onQuery={(q) => runQuery(q)}
            />
          ) : (
            <StatsTable rows={results.rows} />
          )}
          {records && results.rows.length === 0 && (
            <Typography sx={{ p: 2, fontSize: 12, color: 'text.secondary', textAlign: 'center' }}>No matching records</Typography>
          )}
        </>
      )}
    </Stack>
  )
}

export default LogsEditor
