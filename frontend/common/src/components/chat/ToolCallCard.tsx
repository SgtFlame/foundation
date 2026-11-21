import { Suspense, useState } from 'react'
import { alpha, Box, Button, ButtonGroup, CircularProgress, Collapse, IconButton, Paper, Stack, Tooltip, Typography } from '@mui/material'
import BlockIcon from '@mui/icons-material/Block'
import BuildIcon from '@mui/icons-material/Build'
import CheckCircleOutlineIcon from '@mui/icons-material/CheckCircleOutlineOutlined'
import EditNoteIcon from '@mui/icons-material/EditNote'
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutlineOutlined'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import ManageSearchIcon from '@mui/icons-material/ManageSearch'
import ScheduleIcon from '@mui/icons-material/Schedule'
import TouchAppIcon from '@mui/icons-material/TouchApp'
import type { ChatClientToolContribution } from '../../editors/chat/points.ts'
import type { AnswerRequest, ToolCall } from '../../services/Conversation.ts'
import JsonView from '../json/JsonView.tsx'
import MarkdownText from '../markdown/MarkdownText.tsx'

export type ToolAnswer = Omit<AnswerRequest, 'roundId' | 'callId'>

const ended = (call: ToolCall) => !['running', 'awaiting', 'client'].includes(call.status)

function EffectIcon({ call }: { call: ToolCall }) {
  const props = { fontSize: 'small' as const, color: 'action' as const }
  switch (call.effect) {
    case 'read':
      return <ManageSearchIcon {...props} />
    case 'write':
    case 'external':
      return <EditNoteIcon {...props} />
    case 'ui':
      return <TouchAppIcon {...props} />
    case 'none':
      return <ScheduleIcon {...props} />
  }
  return <BuildIcon {...props} />
}

function StatusIcon({ call }: { call: ToolCall }) {
  switch (call.status) {
    case 'running':
    case 'client':
      return <CircularProgress size={14} aria-label="Running" />
    case 'awaiting':
      return null
    case 'done':
      return <CheckCircleOutlineIcon fontSize="small" color="success" aria-label="Done" />
    case 'denied':
      return <BlockIcon fontSize="small" color="disabled" aria-label="Denied" />
    case 'failed':
    case 'timeout':
      return <ErrorOutlineIcon fontSize="small" color="error" aria-label="Failed" />
  }
  return null
}

function statusText(call: ToolCall, asks: boolean): string {
  switch (call.status) {
    case 'running':
      return 'Running…'
    case 'awaiting':
      return 'Waiting for your approval'
    case 'client':
      return asks ? 'Waiting for your answer' : 'Running in your browser…'
    case 'denied':
      return call.approval === 'denied' ? 'Denied' : 'Not run'
    case 'timeout':
      return 'Timed out'
    case 'failed':
      return call.error ?? 'Failed'
  }
  return ''
}

// A write's arguments as the user approves them: text fields in full, so
// what will be written is read before it is
function ArgsPreview({ args }: { args: Record<string, unknown> }) {
  const edits = Array.isArray(args.edits) ? (args.edits as { find?: string; replace?: string }[]) : undefined
  const block = (text: string, tone?: 'add' | 'remove') => (
    <Box
      component="pre"
      sx={{
        m: 0, p: 1, borderRadius: 1, fontSize: 12, whiteSpace: 'pre-wrap', wordBreak: 'break-word', maxHeight: 240, overflow: 'auto',
        bgcolor: (theme) =>
          tone === 'remove' ? alpha(theme.palette.error.main, 0.14) : tone === 'add' ? alpha(theme.palette.success.main, 0.14) : theme.palette.action.hover,
        borderLeft: 3,
        borderColor: tone === 'remove' ? 'error.main' : tone === 'add' ? 'success.main' : 'transparent',
        color: 'text.primary',
      }}
    >
      {text || '(empty)'}
    </Box>
  )
  return (
    <Stack spacing={1}>
      {Object.entries(args)
        .filter(([key]) => key !== 'edits')
        .map(([key, value]) => (
          <Stack key={key} spacing={0.25}>
            <Typography variant="caption" color="text.secondary">
              {key}
            </Typography>
            {typeof value === 'string' && (value.length > 60 || value.includes('\n')) ? (
              block(value)
            ) : (
              <Typography variant="body2" sx={{ wordBreak: 'break-word' }}>
                {typeof value === 'string' ? value : JSON.stringify(value)}
              </Typography>
            )}
          </Stack>
        ))}
      {edits?.map((e, i) => (
        <Stack key={i} spacing={0.5}>
          <Typography variant="caption" color="text.secondary">
            Change {i + 1} of {edits.length}
          </Typography>
          <Box aria-label="Replaces">{block(e.find ?? '', 'remove')}</Box>
          <Box aria-label="With">{block(e.replace ?? '', 'add')}</Box>
        </Stack>
      ))}
    </Stack>
  )
}

// One tool call in the transcript: what was called and how it went, with
// its arguments and result behind the chevron. A write waiting for approval
// shows what it will do and asks; a client tool with a card shows the card.
function ToolCallCard({
  call,
  clientTool,
  onAnswer,
}: {
  call: ToolCall
  clientTool?: ChatClientToolContribution
  onAnswer?: (answer: ToolAnswer) => Promise<void>
}) {
  const [open, setOpen] = useState(false)
  const [sending, setSending] = useState(false)
  const title = call.title || clientTool?.title || call.name
  const answer = (a: ToolAnswer) => {
    if (!onAnswer || sending) return
    setSending(true)
    void onAnswer(a).finally(() => setSending(false))
  }
  const awaiting = call.status === 'awaiting'
  const failed = call.status === 'failed' || call.status === 'timeout'
  const status = statusText(call, !!clientTool?.Card)
  const Card = call.status === 'client' ? clientTool?.Card : undefined

  return (
    <Paper variant="outlined" sx={{ px: 1, py: 0.5, borderColor: awaiting ? 'warning.main' : 'divider' }} aria-label={`Tool call: ${title}`}>
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', minHeight: 28 }}>
        <EffectIcon call={call} />
        <Typography variant="body2" sx={{ fontWeight: 500, flexShrink: 0 }}>
          {title}
        </Typography>
        <Box sx={{ flex: 1, minWidth: 0, typography: 'body2', color: 'text.secondary', '& p': { m: 0 }, overflow: 'hidden' }}>
          {call.status === 'done' && call.summary ? (
            <MarkdownText text={call.summary} />
          ) : (
            <Typography variant="body2" color={failed ? 'error' : 'text.secondary'} noWrap title={status}>
              {status}
            </Typography>
          )}
        </Box>
        {call.cited && call.cited.length > 0 && (
          <Typography variant="caption" color="text.secondary" sx={{ flexShrink: 0 }}>
            {call.cited.map((n) => `[${n}]`).join('')}
          </Typography>
        )}
        <StatusIcon call={call} />
        <Tooltip title={open ? 'Hide the details' : 'Show the arguments and result'}>
          <IconButton size="small" onClick={() => setOpen((o) => !o)} aria-label="Details" aria-expanded={open}>
            <ExpandMoreIcon fontSize="small" sx={{ transform: open ? 'rotate(180deg)' : 'none', transition: 'transform 120ms' }} />
          </IconButton>
        </Tooltip>
      </Stack>
      {awaiting && (
        <Stack spacing={1} sx={{ py: 1 }}>
          <ArgsPreview args={call.args} />
          <Stack direction="row" spacing={1} sx={{ alignItems: 'center', flexWrap: 'wrap', rowGap: 1 }}>
            <ButtonGroup size="small" variant="contained" disabled={sending || !onAnswer}>
              <Button onClick={() => answer({ approval: 'approved' })}>Approve</Button>
              <Tooltip describeChild title={`Run ${title} without asking for the rest of this conversation`}>
                <Button onClick={() => answer({ approval: 'always' })}>Always allow here</Button>
              </Tooltip>
            </ButtonGroup>
            <Button size="small" color="inherit" disabled={sending || !onAnswer} onClick={() => answer({ approval: 'denied' })}>
              Deny
            </Button>
          </Stack>
        </Stack>
      )}
      {Card && onAnswer && (
        <Box sx={{ py: 1 }}>
          <Suspense fallback={<CircularProgress size={14} />}>
            <Card call={call} answer={(result) => answer({ result })} />
          </Suspense>
        </Box>
      )}
      <Collapse in={open} unmountOnExit>
        <Stack spacing={1} sx={{ py: 1 }}>
          <Typography variant="caption" color="text.secondary">
            {call.name} · {call.effect ?? 'unknown'} · {call.location ?? 'server'}
            {call.approval && call.approval !== 'auto' ? ` · ${call.approval}` : ''}
            {call.durationMs ? ` · ${(call.durationMs / 1000).toFixed(1)} s` : ''}
          </Typography>
          <Typography variant="caption">Arguments</Typography>
          <JsonView value={call.args} />
          {ended(call) && call.result !== undefined && (
            <>
              <Typography variant="caption">Result, as the model saw it</Typography>
              <JsonView value={call.result} />
            </>
          )}
        </Stack>
      </Collapse>
    </Paper>
  )
}

export default ToolCallCard
