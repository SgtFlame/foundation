import { IconButton, Stack, Tooltip } from '@mui/material'
import BlockIcon from '@mui/icons-material/Block'
import ThumbDownIcon from '@mui/icons-material/ThumbDown'
import ThumbDownOutlinedIcon from '@mui/icons-material/ThumbDownOutlined'
import ThumbUpIcon from '@mui/icons-material/ThumbUp'
import ThumbUpOutlinedIcon from '@mui/icons-material/ThumbUpOutlined'
import type { RoundFeedback } from '../../services/Conversation.ts'

const choices: { value: RoundFeedback; label: string; hint: string; on: typeof ThumbUpIcon; off: typeof ThumbUpIcon }[] = [
  { value: 'up', label: 'Correct', hint: 'The answer was grounded: its sources rank higher next time', on: ThumbUpIcon, off: ThumbUpOutlinedIcon },
  {
    value: 'down',
    label: 'Incorrect',
    hint: 'Retrieval or the answer failed: its sources rank lower next time. The round stays in the conversation, so you can correct it.',
    on: ThumbDownIcon,
    off: ThumbDownOutlinedIcon,
  },
  { value: 'ignore', label: 'Ignore', hint: 'Leave this exchange out of the rest of the conversation', on: BlockIcon, off: BlockIcon },
]

// 👍 Correct / 👎 Incorrect / 🚫 Ignore; clicking the chosen one returns to neutral
function FeedbackButtons({ value, onChange }: { value?: RoundFeedback; onChange: (value: RoundFeedback | undefined) => void }) {
  return (
    <Stack direction="row" role="group" aria-label="Feedback on this reply">
      {choices.map((c) => {
        const chosen = value === c.value
        const Icon = chosen ? c.on : c.off
        return (
          <Tooltip key={c.value} title={`${c.label}: ${c.hint}`}>
            <IconButton
              size="small"
              aria-label={c.label}
              aria-pressed={chosen}
              color={chosen ? (c.value === 'down' ? 'error' : c.value === 'up' ? 'success' : 'default') : 'default'}
              onClick={() => onChange(chosen ? undefined : c.value)}
              sx={{ opacity: chosen ? 1 : 0.6 }}
            >
              <Icon sx={{ fontSize: 16 }} />
            </IconButton>
          </Tooltip>
        )
      })}
    </Stack>
  )
}

export default FeedbackButtons
