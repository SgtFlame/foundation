import { Avatar, AvatarGroup, Chip, Stack, Tooltip, Typography } from '@mui/material'
import type { CollabState, CollabStatus } from './collab/CollabSession.ts'

const statusChips: Record<CollabStatus, { label: string; color: 'default' | 'success' | 'warning' | 'error' }> = {
  connecting: { label: 'Connecting', color: 'default' },
  syncing: { label: 'Syncing', color: 'default' },
  ready: { label: 'Live', color: 'success' },
  offline: { label: 'Offline', color: 'warning' },
  closed: { label: 'Closed', color: 'error' },
}

function saveText(state: CollabState): string | undefined {
  if (state.status === 'closed') return state.closedReason && `${state.closedReason}; close and reopen the file`
  if (state.saveError) return `Save failed: ${state.saveError}`
  if (state.saving) return 'Saving…'
  if (state.lastSavedAt) return `Saved ${new Date(state.lastSavedAt).toLocaleTimeString()}`
  return undefined
}

// Connection, save state and the other people editing the file
function MarkdownStatusBar({ state }: { state: CollabState }) {
  const chip = statusChips[state.status]
  return (
    <Stack
      direction="row"
      sx={{ alignItems: 'center', gap: 1, px: 1, py: 0.25, borderTop: 1, borderColor: 'divider' }}
    >
      <Chip size="small" label={chip.label} color={chip.color} variant="outlined" />
      {state.readOnly && (
        <Tooltip title="You may read this Volume but not change it">
          <Chip size="small" label="Read-only" variant="outlined" />
        </Tooltip>
      )}
      <Typography
        variant="caption"
        color={state.saveError || state.status === 'closed' ? 'error' : 'text.secondary'}
        sx={{ flexGrow: 1 }}
      >
        {saveText(state)}
      </Typography>
      <AvatarGroup max={6} sx={{ '& .MuiAvatar-root': { width: 22, height: 22, fontSize: 11 } }}>
        {state.peers.map((peer) => (
          <Tooltip key={peer.clientId} title={peer.name}>
            <Avatar sx={{ bgcolor: peer.color }}>{peer.name.at(0)}</Avatar>
          </Tooltip>
        ))}
      </AvatarGroup>
    </Stack>
  )
}

export default MarkdownStatusBar
