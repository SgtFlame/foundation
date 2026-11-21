import { useState } from 'react'
import { ListItemText, MenuItem, TextField, Tooltip } from '@mui/material'
import type { ChatMode } from '../../services/Conversation.ts'

// The conversation's chat mode (forge 06i), beside the level: the modes
// offered where the user is, and the conversation's own if it is not one
// of them. Each mode runs its graph for the next round; the history stays.
function ModePicker({
  modes,
  value,
  onChange,
  disabled,
}: {
  modes: readonly ChatMode[]
  value?: ChatMode
  onChange: (id: string) => void
  disabled?: boolean
}) {
  const [open, setOpen] = useState(false)
  if (modes.length < 2 && (!value || modes.some((m) => m.id === value.id))) return null
  const shown = value && !modes.some((m) => m.id === value.id) ? [value, ...modes] : modes
  const label = (m: ChatMode) => `${m.icon ? `${m.icon} ` : ''}${m.label}`
  return (
    <Tooltip title={open ? '' : (value?.description ?? 'The chat mode: how the next message is answered')} placement="top">
      <TextField
        select
        size="small"
        value={value?.id ?? ''}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
        slotProps={{
          htmlInput: { 'aria-label': 'Chat mode' },
          select: { onOpen: () => setOpen(true), onClose: () => setOpen(false), renderValue: (id) => label(shown.find((m) => m.id === id) ?? { id: String(id), label: String(id), graphRef: '' }) },
        }}
        sx={{ minWidth: 120, '& .MuiSelect-select': { py: 0.5 } }}
      >
        {shown.map((m) => (
          <MenuItem key={m.id} value={m.id} disabled={m.missing} sx={{ maxWidth: 420, whiteSpace: 'normal' }}>
            <ListItemText
              primary={label(m)}
              secondary={m.missing ? 'Its graph is missing' : m.description}
              slotProps={{ secondary: { sx: { display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical', overflow: 'hidden' } } }}
            />
          </MenuItem>
        ))}
      </TextField>
    </Tooltip>
  )
}

export default ModePicker
