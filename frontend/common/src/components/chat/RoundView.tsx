import { Alert, Box, Chip, CircularProgress, IconButton, Paper, Stack, Tooltip, Typography } from '@mui/material'
import ContentCopyIcon from '@mui/icons-material/ContentCopy'
import { roundStopped, type RoundFeedback, type RoundSegment } from '../../services/Conversation.ts'
import MarkdownText from '../markdown/MarkdownText.tsx'
import FeedbackButtons from './FeedbackButtons.tsx'
import { formatCost, formatTokens, type ChatLevel } from './levels.ts'
import RoundSources from './RoundSources.tsx'
import ToolCallCard, { type ToolAnswer } from './ToolCallCard.tsx'
import type { ChatRound } from './useChatStream.ts'
import type { ChatClientToolContribution } from '../../editors/chat/points.ts'

// The footer shown on hover: level, model, tokens, cost and latency
function footer(round: ChatRound, levels: readonly ChatLevel[]): string {
  const level = levels.find((l) => l.id === round.level)
  const parts = [level ? `${level.icon ?? ''} ${level.label}`.trim() : round.level, round.model]
  if (round.status === 200 || round.inputTokens > 0) {
    let tokens = `${formatTokens(round.inputTokens)} in`
    if (round.cachedTokens) tokens += ` (${formatTokens(round.cachedTokens)} cached)`
    tokens += ` · ${formatTokens(round.outputTokens)} out`
    if (round.thinkingTokens) tokens += ` · ${formatTokens(round.thinkingTokens)} thinking`
    parts.push(tokens)
    if (round.currency) parts.push(formatCost(round.costMicros, round.currency))
  }
  if (round.latencyMs) parts.push(`${(round.latencyMs / 1000).toFixed(1)} s`)
  return parts.filter(Boolean).join(' · ')
}

export interface RoundActions {
  onOpenSegment?: (segment: RoundSegment) => void
  onFeedback?: (round: ChatRound, feedback: RoundFeedback | undefined) => void
  onAnswerTool?: (round: ChatRound, callId: string, answer: ToolAnswer) => Promise<void>
  clientTools?: readonly ChatClientToolContribution[] // for their titles and cards
}

// One round: the user's message, the reply as it streams with its
// citations, its sources and Context inspector, feedback, and its footer
function RoundView({ round, levels, onOpenSegment, onFeedback, onAnswerTool, clientTools }: { round: ChatRound; levels: readonly ChatLevel[] } & RoundActions) {
  if (round.note) {
    return (
      <Box sx={{ px: 2, py: 1, color: 'text.secondary', borderLeft: 3, borderColor: 'divider', mx: 2, my: 1 }}>
        <MarkdownText text={round.reply} />
      </Box>
    )
  }
  const streaming = round.status === 0
  const failed = round.status >= 300 && round.status !== roundStopped
  const failure = round.error || `The reply failed (${round.status}).`
  const segments = round.segments ?? []
  const citations = segments.length > 0 ? { titles: segments.map((s) => s.title), onCite: (n: number) => onOpenSegment?.(segments[n - 1]) } : undefined
  const ignored = round.feedback === 'ignore'
  const judgeable = !streaming && !failed && !!round.reply && !round.id.startsWith('local-')
  return (
    <Stack
      sx={{ px: 2, py: 1, opacity: ignored ? 0.5 : 1, '&:hover .round-footer, &:hover .round-feedback': { opacity: 1 } }}
      spacing={1}
      title={ignored ? 'Ignored: this exchange is left out of the rest of the conversation' : undefined}
    >
      <Paper
        elevation={0}
        sx={{ alignSelf: 'flex-end', maxWidth: '80%', px: 1.5, py: 1, bgcolor: 'action.selected', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}
      >
        <Typography variant="body2">{round.text}</Typography>
      </Paper>
      {round.toolCalls && round.toolCalls.length > 0 && (
        <Stack spacing={0.5} aria-label="Tool calls">
          {round.toolCalls.map((call) => (
            <ToolCallCard
              key={call.callId}
              call={call}
              clientTool={clientTools?.find((t) => t.id === call.name)}
              onAnswer={streaming && onAnswerTool ? (answer) => onAnswerTool(round, call.callId, answer) : undefined}
            />
          ))}
        </Stack>
      )}
      {round.reply && <MarkdownText text={round.reply} citations={citations} />}
      {segments.some((s) => s.sourceId === 'chronicle' && s.pinned) && (
        <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.5, alignItems: 'center' }} aria-label="Chronicle entries used">
          <Typography variant="caption" color="text.secondary">
            Chronicles:
          </Typography>
          {segments
            .filter((s) => s.sourceId === 'chronicle' && s.pinned)
            .map((s) => (
              <Tooltip key={s.number} title={`[${s.number}] ${s.reason ?? ''}`}>
                <Chip size="small" variant="outlined" label={s.title} onClick={onOpenSegment ? () => onOpenSegment(s) : undefined} />
              </Tooltip>
            ))}
        </Stack>
      )}
      {segments.length > 0 && <RoundSources segments={segments} onOpen={onOpenSegment} />}
      {round.followed?.map((f, i) => (
        <Box key={i} sx={{ typography: 'caption', color: 'text.secondary', '& p': { m: 0 } }}>
          <MarkdownText text={f.summary} />
        </Box>
      ))}
      {streaming && !round.reply && !round.toolCalls?.some((c) => c.status === 'awaiting' || c.status === 'client') && (
        <CircularProgress size={16} sx={{ ml: 0.5 }} aria-label="Waiting for the reply" />
      )}
      {round.status === roundStopped && (
        <Typography variant="caption" color="text.secondary">
          Stopped
        </Typography>
      )}
      {round.finishReason === 'length' && (
        <Typography variant="caption" color="warning.main">
          The reply reached the level's output limit.
        </Typography>
      )}
      {failed && (
        <Alert
          severity="error"
          action={
            <Tooltip title="Copy the error">
              <IconButton size="small" color="inherit" aria-label="Copy the error" onClick={() => void navigator.clipboard?.writeText(failure)}>
                <ContentCopyIcon sx={{ fontSize: 16 }} />
              </IconButton>
            </Tooltip>
          }
        >
          {failure}
        </Alert>
      )}
      {!streaming && (
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
          {round.reply && (
            <Stack
              direction="row"
              className="round-feedback"
              sx={{ alignItems: 'center', opacity: round.feedback ? 1 : 0, transition: 'opacity 120ms', '&:focus-within': { opacity: 1 } }}
            >
              <Tooltip title="Copy the reply">
                <IconButton size="small" aria-label="Copy the reply" onClick={() => void navigator.clipboard?.writeText(round.reply)} sx={{ opacity: 0.6 }}>
                  <ContentCopyIcon sx={{ fontSize: 16 }} />
                </IconButton>
              </Tooltip>
              {judgeable && onFeedback && <FeedbackButtons value={round.feedback} onChange={(f) => onFeedback(round, f)} />}
            </Stack>
          )}
          <Typography className="round-footer" variant="caption" color="text.secondary" sx={{ opacity: 0, transition: 'opacity 120ms' }}>
            {footer(round, levels)}
          </Typography>
        </Stack>
      )}
    </Stack>
  )
}

export default RoundView
