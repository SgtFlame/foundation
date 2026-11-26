import { useState } from 'react'
import { Divider, IconButton, ListItemText, MenuItem, TextField, Tooltip } from '@mui/material'
import AddIcon from '@mui/icons-material/Add'
import EditOutlinedIcon from '@mui/icons-material/EditOutlined'
import type { ChatMode } from '../../services/Conversation.ts'
import { modePickerShown, modeTitle } from './modeMatch.ts'

// The conversation's chat mode (forge 06i), beside the level: the modes
// offered where the user is, and the conversation's own if it is not one
// of them. Each mode runs its graph for the next round; the history stays.
// open and onOpenChange control the list (forge 15e: a bare /mode opens it,
// above the composer, keyboard first: arrows, Enter, Esc); opened so, it
// leaves the focus to its owner when it closes.
// Forge 15h: onNewMode adds New mode… at the foot of the list (the mode
// that makes modes), and onEditMode a ✎ on each mode the person made,
// opening its graph.
const NEW_MODE = '__newMode'

function ModePicker({
  modes,
  value,
  onChange,
  disabled,
  open: openProp,
  onOpenChange,
  onNewMode,
  onEditMode,
}: {
  modes: readonly ChatMode[]
  value?: ChatMode
  onChange: (id: string) => void
  disabled?: boolean
  open?: boolean
  onOpenChange?: (open: boolean) => void
  onNewMode?: () => void
  onEditMode?: (mode: ChatMode) => void
}) {
  const [openState, setOpenState] = useState(false)
  // The tooltip shows on hover alone, never while the list is open, and
  // never left over from before it opened
  const [hover, setHover] = useState(false)
  const open = openProp ?? openState
  const setOpen = (o: boolean) => {
    setOpenState(o)
    setHover(false)
    onOpenChange?.(o)
  }
  if (!modePickerShown(modes, value)) return null
  const shown = value && !modes.some((m) => m.id === value.id) ? [value, ...modes] : modes
  const label = modeTitle
  return (
    <Tooltip
      title={value?.description ?? 'The chat mode: how the next message is answered'}
      placement="top"
      open={hover && !open}
      onOpen={() => setHover(true)}
      onClose={() => setHover(false)}
      disableFocusListener
    >
      <TextField
        select
        size="small"
        value={value?.id ?? ''}
        onChange={(e) => (e.target.value === NEW_MODE ? onNewMode?.() : onChange(e.target.value))}
        disabled={disabled}
        slotProps={{
          htmlInput: { 'aria-label': 'Chat mode' },
          select: {
            open,
            onOpen: () => setOpen(true),
            onClose: () => setOpen(false),
            MenuProps: { disableRestoreFocus: openProp !== undefined, anchorOrigin: { vertical: 'top', horizontal: 'left' }, transformOrigin: { vertical: 'bottom', horizontal: 'left' } },
            renderValue: (id) => label(shown.find((m) => m.id === id) ?? { id: String(id), label: String(id), graphRef: '' }),
          },
        }}
        sx={{ minWidth: 120, '& .MuiSelect-select': { py: 0.5 } }}
      >
        {shown.map((m) => (
          <MenuItem key={m.id} value={m.id} disabled={m.missing} sx={{ maxWidth: 420, whiteSpace: 'normal', gap: 1 }}>
            <ListItemText
              primary={label(m) + (m.mine ? (m.shared ? ' · yours, shared' : ' · yours') : m.shared ? ' · shared' : '')}
              secondary={m.missing ? 'Its graph is missing' : m.description}
              slotProps={{ secondary: { sx: { display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical', overflow: 'hidden' } } }}
            />
            {m.mine && onEditMode && (
              <Tooltip title="Edit its graph in the Cognition editor">
                <IconButton
                  size="small"
                  aria-label={`Edit ${m.label}`}
                  onMouseDown={(e) => e.stopPropagation()}
                  onClick={(e) => {
                    e.stopPropagation()
                    setOpen(false)
                    onEditMode(m)
                  }}
                >
                  <EditOutlinedIcon fontSize="small" />
                </IconButton>
              </Tooltip>
            )}
          </MenuItem>
        ))}
        {onNewMode && <Divider />}
        {onNewMode && (
          <MenuItem value={NEW_MODE}>
            <AddIcon fontSize="small" sx={{ mr: 1 }} />
            <ListItemText primary="New mode…" secondary="Make a mode of your own with the Mode Builder" />
          </MenuItem>
        )}
      </TextField>
    </Tooltip>
  )
}

export default ModePicker
