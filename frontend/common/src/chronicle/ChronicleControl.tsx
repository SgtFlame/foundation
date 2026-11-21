import { FormControlLabel, Slider, Stack, Switch, Typography } from '@mui/material'
import type { ContextControlProps } from '../editors/chat/points.ts'

// Chronicle memory for the conversation: whether it is used, how many
// entries context matching may choose, whether a model call (stage 1)
// chooses them, and whether replies update memory. The entries checked in
// the Chronicles view are part of the same settings (checked). The server's
// "chronicle" context provider and round follower read them.
function ChronicleControl({ value, onChange }: ContextControlProps) {
  const enabled = value.enabled !== false
  const topK = Number(value.topK ?? 5)
  const checked = Array.isArray(value.checked) ? value.checked.length : 0
  return (
    <Stack spacing={1}>
      <FormControlLabel
        control={<Switch size="small" checked={enabled} onChange={(_, on) => onChange({ ...value, enabled: on })} />}
        label="Use Chronicle memory"
      />
      <Stack>
        <Typography variant="caption" id="chronicle-topk">
          Entries: {topK}
        </Typography>
        <Slider size="small" min={1} max={20} value={topK} disabled={!enabled} onChange={(_, v) => onChange({ ...value, topK: v as number })} aria-labelledby="chronicle-topk" />
      </Stack>
      <FormControlLabel
        control={<Switch size="small" checked={value.match !== false} disabled={!enabled} onChange={(_, on) => onChange({ ...value, match: on })} />}
        label="Context matching"
      />
      <FormControlLabel
        control={<Switch size="small" checked={value.update !== false} disabled={!enabled} onChange={(_, on) => onChange({ ...value, update: on })} />}
        label="Update memory after each reply"
      />
      <Typography variant="caption" color="text.secondary">
        {checked > 0 ? `${checked} checked in the Chronicles view, always used. ` : 'Check entries in the Chronicles view to always use them. '}
        Context matching picks the others each message needs, with a fast model call; off, retrieval's ranking decides.
      </Typography>
    </Stack>
  )
}

export default ChronicleControl
