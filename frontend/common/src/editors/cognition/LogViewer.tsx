import { useState } from 'react'
import {
  Alert,
  Box,
  Button,
  Chip,
  FormControlLabel,
  IconButton,
  List,
  ListItemButton,
  ListItemText,
  Stack,
  Switch,
  Tooltip,
  Typography,
} from '@mui/material'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import ChevronRightIcon from '@mui/icons-material/ChevronRight'
import RefreshIcon from '@mui/icons-material/Refresh'
import JsonView from '../../components/json/JsonView.tsx'
import type { Step } from '../../services/Cognition.ts'
import { formatDuration } from '../../tabs/devtools/network/format.ts'
import { titleOf, useCognition } from './cognitionState.ts'

const statusColor = { ok: 'success', failed: 'error', running: 'info' } as const

function RunList() {
  const state = useCognition()
  const { runs, runId, setRunId, moreRuns, loadMoreRuns, refreshRuns, runsError } = state
  return (
    <Stack sx={{ width: 260, flexShrink: 0, borderRight: 1, borderColor: 'divider', minHeight: 0 }}>
      <Stack direction="row" sx={{ alignItems: 'center', px: 1, py: 0.5, borderBottom: 1, borderColor: 'divider' }}>
        <Typography variant="caption" sx={{ flexGrow: 1, fontWeight: 600 }}>
          Runs
        </Typography>
        <Tooltip title="Refresh">
          <IconButton size="small" onClick={refreshRuns} aria-label="Refresh runs">
            <RefreshIcon fontSize="small" />
          </IconButton>
        </Tooltip>
      </Stack>
      {runsError && <Alert severity="error">{runsError}</Alert>}
      <List dense disablePadding sx={{ overflow: 'auto', flexGrow: 1 }}>
        {runs.length === 0 && !runsError && (
          <Typography variant="body2" sx={{ p: 1.5, color: 'text.secondary' }}>
            No runs yet. Personal runs twice for each chat message: once to choose memory, once to update it.
          </Typography>
        )}
        {runs.map((run) => (
          <ListItemButton key={run.id} selected={run.id === runId} onClick={() => setRunId(run.id)} sx={{ alignItems: 'flex-start' }}>
            <ListItemText
              primary={
                <Stack direction="row" sx={{ alignItems: 'center', gap: 0.75 }}>
                  <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: `${statusColor[run.status]}.main`, flexShrink: 0 }} />
                  <span>{titleOf(state, run.event)}</span>
                </Stack>
              }
              secondary={`${new Date(run.startedAt).toLocaleString()} · ${formatDuration(run.durationMs)} · ${run.steps} steps`}
              slotProps={{ primary: { variant: 'body2', component: 'div' }, secondary: { variant: 'caption' } }}
            />
          </ListItemButton>
        ))}
        {moreRuns && (
          <Button size="small" fullWidth onClick={loadMoreRuns}>
            Older runs
          </Button>
        )}
      </List>
    </Stack>
  )
}

function StepRow({ step }: { step: Step }) {
  const state = useCognition()
  const [open, setOpen] = useState(false)
  const selected = state.selection.seq === step.seq || (state.selection.seq === undefined && state.selection.nodeId === step.nodeId)
  return (
    <Box sx={{ borderBottom: 1, borderColor: 'divider' }}>
      <Stack
        direction="row"
        onClick={() => state.select({ nodeId: step.nodeId, seq: step.seq })}
        sx={{
          alignItems: 'center',
          gap: 1,
          px: 0.5,
          py: 0.25,
          cursor: 'pointer',
          bgcolor: selected ? 'action.selected' : undefined,
          '&:hover': { bgcolor: selected ? 'action.selected' : 'action.hover' },
          fontVariantNumeric: 'tabular-nums',
        }}
      >
        <IconButton
          size="small"
          sx={{ p: 0.25 }}
          onClick={(e) => {
            e.stopPropagation()
            setOpen((o) => !o)
          }}
          aria-label={open ? 'Hide values' : 'Show values'}
        >
          {open ? <ExpandMoreIcon fontSize="small" /> : <ChevronRightIcon fontSize="small" />}
        </IconButton>
        <Typography variant="caption" sx={{ width: 24, color: 'text.secondary' }}>
          {step.seq}
        </Typography>
        <Typography variant="body2" sx={{ flexGrow: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {titleOf(state, step.nodeId)}
          <Typography component="span" variant="caption" sx={{ color: 'text.secondary', ml: 1 }}>
            {step.nodeType}
          </Typography>
        </Typography>
        {step.execPin && step.execPin !== 'then' && <Chip size="small" label={`→ ${step.execPin}`} />}
        {step.error && <Chip size="small" color="error" label="failed" />}
        <Typography variant="caption" sx={{ width: 64, textAlign: 'right', color: 'text.secondary' }}>
          {formatDuration(step.durationMs)}
        </Typography>
      </Stack>
      {open && (
        <Stack sx={{ pl: 5, pr: 1, pb: 1, gap: 1 }}>
          {step.error && <Alert severity="error">{step.error}</Alert>}
          <Typography variant="caption" sx={{ fontWeight: 600 }}>
            Inputs
          </Typography>
          <JsonView value={step.inputs} />
          <Typography variant="caption" sx={{ fontWeight: 600 }}>
            Outputs
          </Typography>
          <JsonView value={step.outputs} />
        </Stack>
      )}
    </Box>
  )
}

function StepList() {
  const state = useCognition()
  const [onlySelected, setOnlySelected] = useState(false)
  const { trace, traceError, runId, selection } = state
  if (!runId) {
    return <Typography sx={{ p: 2, color: 'text.secondary' }}>Choose a run to see its trace.</Typography>
  }
  const steps = (trace?.steps ?? []).filter((s) => !onlySelected || !selection.nodeId || s.nodeId === selection.nodeId)
  return (
    <Stack sx={{ flexGrow: 1, minWidth: 0, minHeight: 0 }}>
      <Stack direction="row" sx={{ alignItems: 'center', gap: 1, px: 1, py: 0.25, borderBottom: 1, borderColor: 'divider' }}>
        <Typography variant="caption" sx={{ flexGrow: 1 }}>
          {trace?.run ? `${trace.run.status === 'failed' ? 'Failed' : 'Ran'} in ${formatDuration(trace.run.durationMs)}, graph version ${trace.run.graphVersion}` : ' '}
        </Typography>
        <FormControlLabel
          control={<Switch size="small" checked={onlySelected} onChange={(_, v) => setOnlySelected(v)} />}
          label={<Typography variant="caption">Only the selected node</Typography>}
        />
      </Stack>
      {traceError && <Alert severity="error">{traceError}</Alert>}
      {trace?.run?.error && (
        <Alert severity="error" sx={{ borderRadius: 0 }}>
          {trace.run.failedNode ? `${titleOf(state, trace.run.failedNode)}: ` : ''}
          {trace.run.error}
        </Alert>
      )}
      <Box sx={{ overflow: 'auto', flexGrow: 1 }}>
        {steps.map((s) => (
          <StepRow key={s.seq} step={s} />
        ))}
        {trace?.run?.outputs && (
          <Box sx={{ p: 1 }}>
            <Typography variant="caption" sx={{ fontWeight: 600 }}>
              The run's outputs
            </Typography>
            <JsonView value={trace.run.outputs} />
          </Box>
        )}
      </Box>
    </Stack>
  )
}

// The Log Viewer: the graph's runs, and the selected run's steps in the
// order they ran, with their inputs and outputs. Selecting a step selects
// its node on the canvas.
function LogViewer() {
  return (
    <Stack direction="row" sx={{ height: '100%', minHeight: 0 }}>
      <RunList />
      <StepList />
    </Stack>
  )
}

export default LogViewer
