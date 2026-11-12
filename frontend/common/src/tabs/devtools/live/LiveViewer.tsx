import { useMemo, useState, useSyncExternalStore } from 'react'
import { Box, Chip, IconButton, Stack, Table, TableBody, TableCell, TableHead, TableRow, TextField, Tooltip, Typography } from '@mui/material'
import BlockIcon from '@mui/icons-material/Block'
import PauseIcon from '@mui/icons-material/Pause'
import PlayArrowIcon from '@mui/icons-material/PlayArrow'
import JsonView from '../../../components/json/JsonView.tsx'
import { live } from '../../../live/LiveConnection.ts'
import { liveFrames, setFramesPaused, type LiveFrame } from '../../../live/frames.ts'
import { useStore } from '../useStore.ts'

function parse(frame: LiveFrame): unknown {
  try {
    return frame.text ? JSON.parse(frame.text) : undefined
  } catch {
    return frame.text
  }
}

// The live connection's frames, newest first: what went out, what came in,
// with a filter on type and content and the selected frame as JSON
function LiveViewer() {
  const frames = useStore(liveFrames)
  const status = useSyncExternalStore((l) => live.onStatus(l), () => live.getStatus())
  const [filter, setFilter] = useState('')
  const [paused, setPaused] = useState(false)
  const [selected, setSelected] = useState<number>()
  const shown = useMemo(() => {
    const f = filter.toLowerCase()
    return frames.filter((e) => !f || e.type.toLowerCase().includes(f) || e.text?.toLowerCase().includes(f)).reverse()
  }, [frames, filter])
  const current = frames.find((f) => f.seq === selected)

  return (
    <Stack sx={{ height: '100%' }}>
      <Stack direction="row" spacing={1} sx={{ p: 0.5, borderBottom: 1, borderColor: 'divider', alignItems: 'center' }}>
        <Chip size="small" label={status} color={status === 'open' ? 'success' : status === 'connecting' ? 'warning' : 'error'} />
        <TextField size="small" placeholder="Filter frames" value={filter} onChange={(e) => setFilter(e.target.value)} sx={{ flex: 1 }} />
        <Tooltip title={paused ? 'Record frames' : 'Pause recording'}>
          <IconButton size="small" aria-label={paused ? 'Record frames' : 'Pause recording'} onClick={() => { setFramesPaused(!paused); setPaused(!paused) }}>
            {paused ? <PlayArrowIcon fontSize="small" /> : <PauseIcon fontSize="small" />}
          </IconButton>
        </Tooltip>
        <Tooltip title="Clear">
          <IconButton size="small" aria-label="Clear" onClick={() => liveFrames.clear()}>
            <BlockIcon fontSize="small" />
          </IconButton>
        </Tooltip>
      </Stack>
      <Box sx={{ flex: 1, overflow: 'auto', minHeight: 0 }}>
        <Table size="small" stickyHeader>
          <TableHead>
            <TableRow>
              <TableCell width={28} />
              <TableCell>Type</TableCell>
              <TableCell>Id</TableCell>
              <TableCell align="right">Size</TableCell>
              <TableCell align="right">Time</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {shown.map((f) => {
              const body = parse(f) as Record<string, unknown> | undefined
              const id = body ? String(body.id ?? body.sub ?? body.session ?? body.channel ?? '') : ''
              return (
                <TableRow key={f.seq} hover selected={f.seq === selected} onClick={() => setSelected(f.seq)} sx={{ cursor: 'pointer' }}>
                  <TableCell sx={{ color: f.direction === 'out' ? 'primary.main' : 'success.main' }}>{f.direction === 'out' ? '↑' : '↓'}</TableCell>
                  <TableCell sx={{ fontFamily: 'monospace' }}>{f.type}</TableCell>
                  <TableCell sx={{ fontFamily: 'monospace', color: 'text.secondary' }}>{id}</TableCell>
                  <TableCell align="right">{f.size}</TableCell>
                  <TableCell align="right">{new Date(f.at).toLocaleTimeString()}</TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
        {shown.length === 0 && (
          <Typography variant="body2" color="text.secondary" sx={{ p: 2 }}>
            No frames yet.
          </Typography>
        )}
      </Box>
      {current && (
        <Box sx={{ maxHeight: '40%', overflow: 'auto', borderTop: 1, borderColor: 'divider', p: 1 }}>
          {current.text ? <JsonView value={parse(current)} /> : <Typography variant="body2">Binary frame, {current.size} bytes</Typography>}
        </Box>
      )}
    </Stack>
  )
}

export default LiveViewer
