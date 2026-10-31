import { useState } from 'react'
import { Divider, IconButton, Popover, Stack, Tooltip, Typography } from '@mui/material'
import SettingsIcon from '@mui/icons-material/Settings'
import type { ChatContextSettingsContribution } from '../../editors/chat/points.ts'

// The composer's ⚙: every chat.contextSettings control for this
// conversation. Their defaults are edited in Settings › AI › Chat › Context.
function ChatSettingsPopover({
  contributions,
  values,
  onChange,
}: {
  contributions: readonly ChatContextSettingsContribution[]
  values: Record<string, Record<string, unknown>>
  onChange: (id: string, value: Record<string, unknown>) => void
}) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null)
  if (contributions.length === 0) return null
  return (
    <>
      <Tooltip title="Settings for this conversation">
        <IconButton size="small" onClick={(e) => setAnchor(e.currentTarget)} aria-label="Conversation settings">
          <SettingsIcon fontSize="small" />
        </IconButton>
      </Tooltip>
      <Popover
        open={!!anchor}
        anchorEl={anchor}
        onClose={() => setAnchor(null)}
        anchorOrigin={{ vertical: 'top', horizontal: 'left' }}
        transformOrigin={{ vertical: 'bottom', horizontal: 'left' }}
      >
        <Stack sx={{ p: 2, width: 320 }} spacing={1.5} divider={<Divider flexItem />}>
          {contributions.map((c) => {
            const Icon = c.icon
            return (
              <Stack key={c.id} spacing={0.5}>
                <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                  {Icon && <Icon fontSize="small" />}
                  <Typography variant="subtitle2">{c.title}</Typography>
                </Stack>
                <c.Control value={values[c.id] ?? {}} onChange={(v) => onChange(c.id, v)} />
              </Stack>
            )
          })}
        </Stack>
      </Popover>
    </>
  )
}

export default ChatSettingsPopover
