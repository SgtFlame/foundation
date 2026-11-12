import { Checkbox, FormControlLabel, Stack, Typography } from '@mui/material'
import type { ContextControlProps } from './points.ts'

// Whether the server logs this conversation's AI calls in detail; the
// server's "debug" context provider reads it
function DebugControl({ value, onChange }: ContextControlProps) {
  return (
    <Stack>
      <FormControlLabel
        control={<Checkbox size="small" checked={value.enabled === true} onChange={(_, enabled) => onChange({ ...value, enabled })} />}
        label="Log AI calls in detail"
      />
      <Typography variant="caption" color="text.secondary">
        The server logs each call's model, settings and request shape, and the provider's full error. Prompts and replies are not logged.
      </Typography>
    </Stack>
  )
}

export default DebugControl
