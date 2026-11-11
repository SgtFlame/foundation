// Administration › Jobs (forge 07a, documentation/Jobs.md): the tenant's
// background jobs as the live jobs.list query gives them, filtered by
// status, app or graph. A job opens to its details, its inputs and each
// attempt, whose Cognition trace loads on demand; Retry queues a dead or
// canceled job again and Cancel stops one that has not started, both as
// commands. Everything stays live: a job moves through its statuses as
// the runners run it.

import { Fragment, useMemo, useState } from 'react'
import {
  Alert,
  Box,
  Button,
  Chip,
  IconButton,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Tooltip,
  Typography,
} from '@mui/material'
import KeyboardArrowDownIcon from '@mui/icons-material/KeyboardArrowDown'
import KeyboardArrowRightIcon from '@mui/icons-material/KeyboardArrowRight'
import { useQuery } from '../../live/useQuery.ts'
import { live } from '../../live/LiveConnection.ts'
import JsonView from '../../components/json/JsonView.tsx'
import type { Step } from '../../services/Cognition.ts'
import { jobStatuses, principalText, type Job, type JobRun, type JobStatus, type JobsList } from '../../services/Jobs.ts'

const statusColor: Record<JobStatus, 'default' | 'primary' | 'info' | 'warning' | 'error' | 'success'> = {
  queued: 'default',
  running: 'primary',
  retrying: 'warning',
  dead: 'error',
  done: 'success',
  canceled: 'default',
}

const runColor: Record<JobRun['status'], 'default' | 'primary' | 'error' | 'success' | 'warning'> = {
  running: 'primary',
  ok: 'success',
  failed: 'error',
  lost: 'warning',
}

function when(t?: string): string {
  if (!t || t.startsWith('0001-')) return ''
  return new Date(t).toLocaleString()
}

function duration(from?: string, to?: string): string {
  if (!from || !to || to.startsWith('0001-')) return ''
  const ms = new Date(to).getTime() - new Date(from).getTime()
  return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`
}

// The trace of one run, read when opened through the jobs.trace query
// (which shows a job's runs to whoever may see the job): each node it ran,
// the failed one with its error and inputs, so a dead job says what to fix
function Trace({ id, runId }: { id: string; runId: string }) {
  const [open, setOpen] = useState(false)
  return (
    <Box>
      <Button size="small" onClick={() => setOpen(!open)}>
        {open ? 'Hide trace' : 'Trace'}
      </Button>
      {open && <TraceSteps id={id} runId={runId} />}
    </Box>
  )
}

function TraceSteps({ id, runId }: { id: string; runId: string }) {
  const { data, error } = useQuery<{ steps?: Step[] }>('jobs.trace', { id, runId })
  const steps = data?.steps
  if (error) return <Alert severity="warning">{error.message}</Alert>
  if (!steps) return null
  if (steps.length === 0) {
    return (
      <Typography variant="caption" color="text.secondary">
        No steps were kept for this run.
      </Typography>
    )
  }
  return (
    <Table size="small" aria-label="Trace">
      <TableBody>
        {steps.map((s) => (
          <TableRow key={s.seq}>
            <TableCell sx={{ width: 40, color: 'text.secondary' }}>{s.seq}</TableCell>
            <TableCell sx={{ fontFamily: 'monospace' }}>
              {s.nodeId} <Typography component="span" variant="caption" color="text.secondary">({s.nodeType})</Typography>
            </TableCell>
            <TableCell sx={{ width: 80 }}>{s.durationMs} ms</TableCell>
            <TableCell>
              {s.error ? (
                <Stack spacing={0.5}>
                  <Typography variant="body2" color="error">
                    {s.error}
                  </Typography>
                  {s.inputs && <JsonView value={s.inputs} />}
                </Stack>
              ) : (
                <Typography variant="caption" color="text.secondary">
                  {s.execPin ? `→ ${s.execPin}` : ''}
                </Typography>
              )}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function Details({ job }: { job: Job }) {
  const facts: [string, string][] = [
    ['ID', job.id],
    ['Queue', job.queue],
    ['Key', job.key ?? ''],
    ['Event', job.event ?? 'On call'],
    ['Run at', when(job.runAt)],
    ['Retry at', when(job.retryAt)],
    ['Started', when(job.startedAt)],
    ['Finished', when(job.finishedAt)],
    ['Request', job.request ?? ''],
    ['Last action by', job.by ?? ''],
  ]
  return (
    <Stack spacing={1.5} sx={{ py: 1 }}>
      {job.error && (
        <Alert severity={job.status === 'dead' ? 'error' : 'warning'}>
          <Typography variant="body2" sx={{ fontFamily: 'monospace', whiteSpace: 'pre-wrap' }}>
            {job.error}
          </Typography>
        </Alert>
      )}
      <Box sx={{ display: 'grid', gridTemplateColumns: 'max-content 1fr', columnGap: 2, rowGap: 0.25 }}>
        {facts
          .filter(([, v]) => v)
          .map(([k, v]) => (
            <Fragment key={k}>
              <Typography variant="caption" color="text.secondary">
                {k}
              </Typography>
              <Typography variant="body2" sx={{ wordBreak: 'break-all' }}>
                {v}
              </Typography>
            </Fragment>
          ))}
      </Box>
      {job.inputs != null && (
        <Box>
          <Typography variant="caption" color="text.secondary">
            Inputs
          </Typography>
          <JsonView value={job.inputs} />
        </Box>
      )}
      <Typography variant="subtitle2">Attempts</Typography>
      {job.runs.length === 0 && (
        <Typography variant="caption" color="text.secondary">
          Not run yet.
        </Typography>
      )}
      {[...job.runs].reverse().map((r) => (
        <Box key={`${r.attempt}-${r.startedAt}`} sx={{ borderLeft: 3, borderColor: 'divider', pl: 1.5 }}>
          <Stack direction="row" spacing={1} sx={{ alignItems: 'center', flexWrap: 'wrap' }}>
            <Typography variant="body2">Attempt {r.attempt}</Typography>
            <Chip size="small" label={r.status} color={runColor[r.status]} variant="outlined" />
            <Typography variant="caption" color="text.secondary">
              {when(r.startedAt)} {duration(r.startedAt, r.finishedAt) && `· ${duration(r.startedAt, r.finishedAt)}`}
              {r.runner && ` · on ${r.runner}`}
              {r.version ? ` · graph v${r.version}` : ''}
            </Typography>
          </Stack>
          {r.error && (
            <Typography variant="body2" color="error" sx={{ fontFamily: 'monospace', whiteSpace: 'pre-wrap' }}>
              {r.error}
              {r.failedNode && ` (node ${r.failedNode})`}
            </Typography>
          )}
          {r.runId && <Trace id={job.id} runId={r.runId} />}
        </Box>
      ))}
    </Stack>
  )
}

function JobsEditor() {
  const [status, setStatus] = useState<JobStatus | ''>('')
  const [search, setSearch] = useState('')
  const [open, setOpen] = useState<string>()
  const [error, setError] = useState<string>()
  const params = useMemo(() => (status ? { status } : {}), [status])
  const { data, loading, error: queryError } = useQuery<JobsList>('jobs.list', params)

  const jobs = useMemo(() => {
    const q = search.trim().toLowerCase()
    return Object.values(data ?? {})
      .filter((j) => !q || [j.id, j.graph, j.app, j.trigger, j.key].some((v) => v?.toLowerCase().includes(q)))
      .sort((a, b) => b.enqueuedAt.localeCompare(a.enqueuedAt))
  }, [data, search])

  const act = async (command: 'job.retry' | 'job.cancel', id: string) => {
    setError(undefined)
    try {
      await live.command(command, id, {})
    } catch (err) {
      setError((err as Error).message)
    }
  }

  return (
    <Box sx={{ p: 2, height: '100%', overflow: 'auto', boxSizing: 'border-box' }}>
      <Stack direction="row" spacing={2} sx={{ alignItems: 'center', mb: 2, flexWrap: 'wrap', rowGap: 1 }}>
        <Typography variant="h6" component="h2">
          Jobs
        </Typography>
        <ToggleButtonGroup size="small" exclusive value={status} onChange={(_, v: JobStatus | '' | null) => setStatus(v ?? '')} aria-label="Status">
          <ToggleButton value="">All</ToggleButton>
          {jobStatuses.map((s) => (
            <ToggleButton key={s} value={s}>
              {s}
            </ToggleButton>
          ))}
        </ToggleButtonGroup>
        <TextField size="small" label="Search id, graph, app or key" value={search} onChange={(e) => setSearch(e.target.value)} sx={{ minWidth: 260 }} />
      </Stack>
      {error && (
        <Alert severity="error" onClose={() => setError(undefined)} sx={{ mb: 2 }}>
          {error}
        </Alert>
      )}
      {queryError && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {queryError.message}
        </Alert>
      )}
      {!loading && jobs.length === 0 && (
        <Typography color="text.secondary">
          No jobs{status ? ` ${status}` : ''}. Jobs appear here as apps’ triggers fire and graphs enqueue them; finished ones stay for a day.
        </Typography>
      )}
      {jobs.length > 0 && (
        <Table size="small" aria-label="Jobs">
          <TableHead>
            <TableRow>
              <TableCell sx={{ width: 32 }} />
              <TableCell>Status</TableCell>
              <TableCell>Graph</TableCell>
              <TableCell>App</TableCell>
              <TableCell>Runs as</TableCell>
              <TableCell>Attempts</TableCell>
              <TableCell>Enqueued</TableCell>
              <TableCell>Last error</TableCell>
              <TableCell />
            </TableRow>
          </TableHead>
          <TableBody>
            {jobs.map((j) => {
              const expanded = open === j.id
              return (
                <Fragment key={j.id}>
                  <TableRow hover onClick={() => setOpen(expanded ? undefined : j.id)} sx={{ cursor: 'pointer', '& > td': { borderBottom: expanded ? 'none' : undefined } }}>
                    <TableCell>
                      <IconButton size="small" aria-label={expanded ? 'Close' : 'Open'}>
                        {expanded ? <KeyboardArrowDownIcon fontSize="inherit" /> : <KeyboardArrowRightIcon fontSize="inherit" />}
                      </IconButton>
                    </TableCell>
                    <TableCell>
                      <Chip size="small" label={j.status} color={statusColor[j.status]} variant={j.status === 'queued' ? 'outlined' : 'filled'} />
                    </TableCell>
                    <TableCell sx={{ fontFamily: 'monospace' }}>{j.graph}</TableCell>
                    <TableCell>{j.app ? `${j.app}${j.trigger ? ` › ${j.trigger}` : ''}` : ''}</TableCell>
                    <TableCell>{principalText(j.as)}</TableCell>
                    <TableCell>{j.attempts}</TableCell>
                    <TableCell>{when(j.enqueuedAt)}</TableCell>
                    <TableCell sx={{ maxWidth: 320 }}>
                      <Tooltip title={j.error ?? ''}>
                        <Typography variant="body2" color="error" noWrap>
                          {j.error}
                        </Typography>
                      </Tooltip>
                    </TableCell>
                    <TableCell align="right" onClick={(e) => e.stopPropagation()}>
                      {(j.status === 'dead' || j.status === 'canceled') && (
                        <Button size="small" onClick={() => void act('job.retry', j.id)}>
                          Retry
                        </Button>
                      )}
                      {(j.status === 'queued' || j.status === 'retrying') && (
                        <Button size="small" color="warning" onClick={() => void act('job.cancel', j.id)}>
                          Cancel
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                  {expanded && (
                    <TableRow>
                      <TableCell />
                      <TableCell colSpan={8}>
                        <Details job={j} />
                      </TableCell>
                    </TableRow>
                  )}
                </Fragment>
              )
            })}
          </TableBody>
        </Table>
      )}
    </Box>
  )
}

export default JobsEditor
