import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Box,
  IconButton,
  MenuItem,
  Select,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material'
import BlockIcon from '@mui/icons-material/Block'
import CloseIcon from '@mui/icons-material/Close'
import FiberManualRecordIcon from '@mui/icons-material/FiberManualRecord'
import { networkStore, type NetworkEntry } from '../../../services/http/NetworkStore.ts'
import { useStore } from '../useStore.ts'
import { formatDuration, formatSize, shortName } from './format.ts'
import RequestDetails from './RequestDetails.tsx'

type StatusFilter = 'all' | '2xx' | '3xx' | '4xx' | '5xx' | 'failed'

function matchesStatus(entry: NetworkEntry, filter: StatusFilter): boolean {
  if (filter === 'all') return true
  if (filter === 'failed') return entry.state === 'failed'
  return entry.status !== undefined && `${Math.floor(entry.status / 100)}xx` === filter
}

function statusColor(entry: NetworkEntry): string | undefined {
  if (entry.state === 'failed' || (entry.status ?? 0) >= 400) return 'error.main'
  if (entry.state === 'pending') return 'text.secondary'
  return undefined
}

function statusLabel(entry: NetworkEntry): string {
  if (entry.state === 'pending') return '(pending)'
  if (entry.state === 'failed') return '(failed)'
  return String(entry.status)
}

const cell = { py: 0.25, px: 1, fontSize: 12, whiteSpace: 'nowrap' } as const

// A subset of Chrome DevTools' Network panel over networkStore
function NetworkViewer() {
  const entries = useStore(networkStore)
  const [recording, setRecording] = useState(!networkStore.paused)
  const [text, setText] = useState('')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [selectedId, setSelectedId] = useState<number>()
  const listRef = useRef<HTMLDivElement>(null)

  const visible = useMemo(() => {
    const needle = text.toLowerCase()
    return entries.filter(
      (entry) =>
        matchesStatus(entry, status) &&
        (!needle || `${entry.method} ${entry.url}`.toLowerCase().includes(needle)),
    )
  }, [entries, text, status])

  const selected = entries.find((entry) => entry.id === selectedId)

  // Keep following new requests while scrolled to the bottom
  useEffect(() => {
    const list = listRef.current
    if (list && list.scrollHeight - list.scrollTop - list.clientHeight < 40) {
      list.scrollTop = list.scrollHeight
    }
  }, [visible.length])

  const toggleRecording = () => {
    networkStore.setPaused(recording)
    setRecording(!recording)
  }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center', p: 0.5, borderBottom: 1, borderColor: 'divider' }}>
        <Tooltip title={recording ? 'Stop recording' : 'Record'}>
          <IconButton size="small" onClick={toggleRecording}>
            <FiberManualRecordIcon fontSize="small" color={recording ? 'error' : 'disabled'} />
          </IconButton>
        </Tooltip>
        <Tooltip title="Clear">
          <IconButton
            size="small"
            onClick={() => {
              networkStore.clear()
              setSelectedId(undefined)
            }}
          >
            <BlockIcon fontSize="small" />
          </IconButton>
        </Tooltip>
        <TextField
          size="small"
          placeholder="Filter"
          value={text}
          onChange={(event) => setText(event.target.value)}
          sx={{ flexGrow: 1, '& input': { py: 0.5, fontSize: 12 } }}
        />
        <Select
          size="small"
          value={status}
          onChange={(event) => setStatus(event.target.value as StatusFilter)}
          sx={{ fontSize: 12, '& .MuiSelect-select': { py: 0.5 } }}
        >
          <MenuItem value="all">All</MenuItem>
          <MenuItem value="2xx">2xx</MenuItem>
          <MenuItem value="3xx">3xx</MenuItem>
          <MenuItem value="4xx">4xx</MenuItem>
          <MenuItem value="5xx">5xx</MenuItem>
          <MenuItem value="failed">Failed</MenuItem>
        </Select>
      </Stack>

      <Box ref={listRef} sx={{ flex: selected ? '0 0 40%' : 1, overflow: 'auto', minHeight: 0 }}>
        <Table size="small" stickyHeader>
          <TableHead>
            <TableRow>
              <TableCell sx={cell}>Name</TableCell>
              <TableCell sx={cell}>Method</TableCell>
              <TableCell sx={cell}>Status</TableCell>
              <TableCell sx={cell} align="right">Size</TableCell>
              <TableCell sx={cell} align="right">Time</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {visible.map((entry) => (
              <TableRow
                key={entry.id}
                hover
                selected={entry.id === selectedId}
                onClick={() => setSelectedId(entry.id)}
                sx={{ cursor: 'pointer', '& td': { color: statusColor(entry) } }}
              >
                <TableCell sx={{ ...cell, maxWidth: 220, overflow: 'hidden', textOverflow: 'ellipsis' }} title={entry.url}>
                  {shortName(entry.url)}
                </TableCell>
                <TableCell sx={cell}>{entry.method}</TableCell>
                <TableCell sx={cell}>{statusLabel(entry)}</TableCell>
                <TableCell sx={cell} align="right">{formatSize(entry.sizeBytes)}</TableCell>
                <TableCell sx={cell} align="right">{formatDuration(entry.durationMs)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {visible.length === 0 && (
          <Typography sx={{ p: 2, fontSize: 12, color: 'text.secondary', textAlign: 'center' }}>
            {recording ? 'Recording network activity…' : 'Recording is paused'}
          </Typography>
        )}
      </Box>

      {selected && (
        <Box sx={{ flex: 1, minHeight: 0, borderTop: 1, borderColor: 'divider', position: 'relative' }}>
          <Tooltip title="Close">
            <IconButton
              size="small"
              onClick={() => setSelectedId(undefined)}
              sx={{ position: 'absolute', right: 2, top: 2, zIndex: 1 }}
            >
              <CloseIcon fontSize="inherit" />
            </IconButton>
          </Tooltip>
          <RequestDetails entry={selected} />
        </Box>
      )}
    </Box>
  )
}

export default NetworkViewer
