import { FormControlLabel, Stack, Switch, Typography } from '@mui/material'
import type { ContextControlProps } from '../../editors/chat/points.ts'

// Whether the workbench snapshot goes with each message; the server's
// "workbench" context provider renders it into the prompt
function ContextControl({ value, onChange }: ContextControlProps) {
  return (
    <Stack>
      <FormControlLabel
        control={
          <Switch size="small" checked={value.enabled !== false} onChange={(_, enabled) => onChange({ ...value, enabled })} />
        }
        label="Send what I have open"
      />
      <Typography variant="caption" color="text.secondary">
        Open editors, what each is editing, and what you recently viewed or edited. Never their content.
      </Typography>
    </Stack>
  )
}

export default ContextControl
