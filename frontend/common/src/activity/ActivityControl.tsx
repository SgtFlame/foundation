import { FormControlLabel, Stack, Switch, Typography } from '@mui/material'
import type { ContextControlProps } from '../editors/chat/points.ts'
import { useSetting } from '../settings/configStore.ts'

// Whether the person's recent activity goes with each message (forge 15d);
// the server's "activity" context provider renders it into the prompt
function ActivityControl({ value, onChange }: ContextControlProps) {
  const recording = useSetting<boolean>('activity', 'record', 'user') !== false
  return (
    <Stack>
      <FormControlLabel
        control={<Switch size="small" checked={value.enabled !== false} onChange={(_, enabled) => onChange({ ...value, enabled })} />}
        label="Send what I did"
      />
      <Typography variant="caption" color="text.secondary">
        {recording
          ? 'What you did by hand since your last message: tabs opened, clicks on app pages, records changed. Dismissed actions are never sent.'
          : 'Your recent activity is not recorded: turn it on in Settings › Chat › Recent activity.'}
      </Typography>
    </Stack>
  )
}

export default ActivityControl
