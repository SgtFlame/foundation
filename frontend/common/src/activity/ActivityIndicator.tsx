import { Box, Tooltip } from '@mui/material'
import HistoryIcon from '@mui/icons-material/History'
import { useSetting } from '../settings/configStore.ts'

// The composer's sign that the person's recent activity goes with each
// message (forge 15c question 11, built in 15d): shown while it is
// recorded and the conversation's switch is on. A small icon, its words in
// the tooltip, so a narrow docked chat keeps room for the mode and level.
function ActivityIndicator() {
  const recording = useSetting<boolean>('activity', 'record', 'user') !== false
  if (!recording) return null
  return (
    <Tooltip title="Recent activity is shared: what you did by hand since your last message goes with it, without the actions you dismiss. Turn it off for this conversation in ⚙ or with /activity off, or stop recording in Settings › Chat › Recent activity.">
      <Box component="span" role="img" aria-label="Recent activity is shared" sx={{ display: 'inline-flex', color: 'text.secondary', flexShrink: 0 }}>
        <HistoryIcon fontSize="small" />
      </Box>
    </Tooltip>
  )
}

export default ActivityIndicator
