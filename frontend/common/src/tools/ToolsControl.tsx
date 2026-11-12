import { FormControlLabel, Stack, Switch, Typography } from '@mui/material'
import type { ContextControlProps } from '../editors/chat/points.ts'

// Whether the model may call tools in this conversation; the server's
// "tools" context provider reads it
function ToolsControl({ value, onChange }: ContextControlProps) {
  return (
    <Stack>
      <FormControlLabel
        control={<Switch size="small" checked={value.enabled !== false} onChange={(_, enabled) => onChange({ ...value, enabled })} />}
        label="Let the model use tools"
      />
      <Typography variant="caption" color="text.secondary">
        Search and read your Volumes and Chronicles, ask you to choose, and open editors. Changes to your files and Chronicles always ask you first.
      </Typography>
    </Stack>
  )
}

export default ToolsControl
