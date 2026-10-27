import { Alert, Box, Chip, Divider, MenuItem, Stack, TextField, Typography } from '@mui/material'
import JsonView from '../../components/json/JsonView.tsx'
import type { Pin, Truncated } from '../../services/Cognition.ts'
import { formatDuration } from '../../tabs/devtools/network/format.ts'
import { titleOf, typeColor, useCognition } from './cognitionState.ts'

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <Box sx={{ p: 1.5 }}>
      <Typography variant="overline" sx={{ color: 'text.secondary', lineHeight: 1.5 }}>
        {title}
      </Typography>
      {children}
    </Box>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <Stack direction="row" sx={{ gap: 1, fontSize: 13, py: 0.25 }}>
      <Box sx={{ width: 90, flexShrink: 0, color: 'text.secondary' }}>{label}</Box>
      <Box sx={{ minWidth: 0, overflowWrap: 'anywhere' }}>{children}</Box>
    </Stack>
  )
}

function isTruncated(v: unknown): v is Truncated {
  return typeof v === 'object' && v !== null && '$truncated' in v
}

// A captured value: cut short ones show their start
function Value({ value }: { value: unknown }) {
  if (isTruncated(value)) {
    return (
      <Stack sx={{ gap: 0.5 }}>
        <Alert severity="info">
          {Math.round(value.$truncated / 1024)} KB, larger than the trace keeps; here is its start (Settings › AI › Cognition)
        </Alert>
        <Box component="pre" sx={{ m: 0, fontSize: 12, whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
          {value.preview}
        </Box>
      </Stack>
    )
  }
  if (value === undefined) {
    return <Typography variant="body2" sx={{ color: 'text.secondary' }}>No value</Typography>
  }
  return <JsonView value={value} />
}

function PinChip({ pin }: { pin: Pin }) {
  const { types } = useCognition()
  return (
    <Chip
      size="small"
      variant="outlined"
      label={pin.exec ? 'exec' : pin.type}
      sx={{ borderColor: pin.exec ? 'text.secondary' : typeColor(types, pin.type), height: 20, fontSize: 11 }}
    />
  )
}

// The right details panel: the selected node, the selected pin, and the
// pin's captured value in a chosen run (Handle Data)
function DetailsPanel() {
  const state = useCognition()
  const { graph, pins, problems, nodeTypes, selection, trace, runs, runId, setRunId } = state
  const node = graph.nodes.find((n) => n.id === selection.nodeId)
  if (!node) {
    return (
      <Box sx={{ overflow: 'auto', height: '100%' }}>
        <Section title="Graph">
          <Typography variant="subtitle2">{state.info.name}</Typography>
          <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1 }}>
            {state.info.description}
          </Typography>
          <Field label="Kind">{state.info.kind}</Field>
          <Field label="Version">{state.info.version}</Field>
          <Field label="Nodes">{graph.nodes.length}</Field>
          {state.info.builtIn && <Field label="Built in">read-only</Field>}
          {problems.map((p, i) => (
            <Alert key={i} severity={p.severity} sx={{ mt: 1 }}>
              {p.nodeId ? `${titleOf(state, p.nodeId)}: ` : ''}
              {p.message}
            </Alert>
          ))}
          <Typography variant="body2" sx={{ color: 'text.secondary', mt: 2 }}>
            Select a node to see its settings and pins; select a pin to see the value it carried in a run.
          </Typography>
        </Section>
      </Box>
    )
  }

  const type = nodeTypes.get(node.type)
  const nodePins = pins[node.id] ?? { in: [], out: [] }
  const pin = selection.pin && nodePins[selection.pin.dir].find((p) => p.id === selection.pin?.id)
  const steps = (trace?.steps ?? []).filter((s) => s.nodeId === node.id)
  const step = steps.find((s) => s.seq === selection.seq) ?? steps[0]

  return (
    <Box sx={{ overflow: 'auto', height: '100%' }}>
      <Section title="Node">
        <Typography variant="subtitle2">{titleOf(state, node.id)}</Typography>
        <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1 }}>
          {type?.description}
        </Typography>
        <Field label="ID">{node.id}</Field>
        <Field label="Type">{node.type}</Field>
        <Field label="Category">{type?.category}</Field>
        <Field label="From">{type?.source}</Field>
        {node.config !== undefined && (
          <Box sx={{ mt: 1 }}>
            <Typography variant="caption" sx={{ fontWeight: 600 }}>
              Config
            </Typography>
            <JsonView value={node.config} />
          </Box>
        )}
        {problems
          .filter((p) => p.nodeId === node.id)
          .map((p, i) => (
            <Alert key={i} severity={p.severity} sx={{ mt: 1 }}>
              {p.message}
            </Alert>
          ))}
        <Stack sx={{ mt: 1, gap: 0.25 }}>
          {(['in', 'out'] as const).map((dir) =>
            nodePins[dir].map((p) => (
              <Stack
                key={dir + p.id}
                direction="row"
                onClick={() => state.select({ nodeId: node.id, pin: { id: p.id, dir }, seq: selection.seq })}
                sx={{
                  alignItems: 'center',
                  gap: 1,
                  fontSize: 13,
                  cursor: 'pointer',
                  px: 0.5,
                  borderRadius: 0.5,
                  bgcolor: pin?.id === p.id && selection.pin?.dir === dir ? 'action.selected' : undefined,
                  '&:hover': { bgcolor: 'action.hover' },
                }}
              >
                <Box sx={{ width: 28, color: 'text.secondary', fontSize: 11 }}>{dir}</Box>
                <Box sx={{ flexGrow: 1 }}>{p.name || p.id}</Box>
                <PinChip pin={p} />
              </Stack>
            )),
          )}
        </Stack>
      </Section>
      {pin && selection.pin && (
        <>
          <Divider />
          <Section title="Handle">
            <Field label="Node">{node.id}</Field>
            <Field label="Handle">{pin.id}</Field>
            <Field label="Direction">{selection.pin.dir === 'in' ? 'input' : 'output'}</Field>
            <Field label="Type">{pin.exec ? 'exec' : pin.type}</Field>
            {pin.description && <Field label="About">{pin.description}</Field>}
          </Section>
        </>
      )}
      <Divider />
      <Section title={pin ? 'Handle data' : 'Run data'}>
        <TextField select size="small" fullWidth label="Run" value={runId ?? ''} onChange={(e) => setRunId(e.target.value || undefined)} sx={{ my: 1 }}>
          {runs.length === 0 && <MenuItem value="">No runs yet</MenuItem>}
          {runs.map((r) => (
            <MenuItem key={r.id} value={r.id}>
              {new Date(r.startedAt).toLocaleString()} · {titleOf(state, r.event)}
            </MenuItem>
          ))}
        </TextField>
        {runId && !step && (
          <Typography variant="body2" sx={{ color: 'text.secondary' }}>
            The node did not run in this run.
          </Typography>
        )}
        {step && (
          <Stack sx={{ gap: 1 }}>
            <Typography variant="caption" sx={{ color: 'text.secondary' }}>
              Step {step.seq} · {formatDuration(step.durationMs)}
              {step.execPin ? ` · left by ${step.execPin}` : ''}
            </Typography>
            {step.error && <Alert severity="error">{step.error}</Alert>}
            {pin && selection.pin ? (
              pin.exec ? (
                <Typography variant="body2">
                  {selection.pin.dir === 'out' ? (step.execPin === pin.id ? 'Taken in this run.' : 'Not taken in this run.') : 'Reached in this run.'}
                </Typography>
              ) : (
                <Value value={(selection.pin.dir === 'in' ? step.inputs : step.outputs)?.[pin.id]} />
              )
            ) : (
              <>
                <Typography variant="caption" sx={{ fontWeight: 600 }}>
                  Inputs
                </Typography>
                <Value value={step.inputs} />
                <Typography variant="caption" sx={{ fontWeight: 600 }}>
                  Outputs
                </Typography>
                <Value value={step.outputs} />
              </>
            )}
          </Stack>
        )}
      </Section>
    </Box>
  )
}

export default DetailsPanel
