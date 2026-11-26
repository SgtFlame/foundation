import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Box,
  Button,
  IconButton,
  MenuItem,
  Select,
  Stack,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Tooltip,
  Typography,
} from '@mui/material'
import BlockIcon from '@mui/icons-material/Block'
import JsonView from '../../../components/json/JsonView.tsx'
import LogRow, { details } from '../../../components/logs/LogRow.tsx'
import { openLogs } from '../../../editors/logs/openLogs.ts'
import { logLevels, logStore, type LogLevel } from '../../../logging/index.ts'
import { useStore } from '../useStore.ts'

const levelLabels: Record<LogLevel, string> = {
  debug: 'Verbose',
  info: 'Info',
  warn: 'Warnings',
  error: 'Errors',
}

// A subset of Chrome DevTools' Console panel over logStore
function LogViewer() {
  const records = useStore(logStore)
  const [levels, setLevels] = useState<LogLevel[]>(['info', 'warn', 'error'])
  const [text, setText] = useState('')
  const [domain, setDomain] = useState('')
  const listRef = useRef<HTMLDivElement>(null)

  const domains = useMemo(
    () => [...new Set(records.flatMap((record) => (record.domain ? [record.domain] : [])))].sort(),
    [records],
  )

  const visible = useMemo(() => {
    const needle = text.toLowerCase()
    return records.filter(
      (record) =>
        levels.includes(record.level) &&
        (!domain || record.domain === domain) &&
        (!needle ||
          [record.message, record.domain ?? '', record.code ?? '', ...record.tags]
            .join(' ')
            .toLowerCase()
            .includes(needle)),
    )
  }, [records, levels, text, domain])

  // Keep following new records while scrolled to the bottom
  useEffect(() => {
    const list = listRef.current
    if (list && list.scrollHeight - list.scrollTop - list.clientHeight < 40) {
      list.scrollTop = list.scrollHeight
    }
  }, [visible.length])

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <Stack
        direction="row"
        spacing={0.5}
        useFlexGap
        sx={{ alignItems: 'center', flexWrap: 'wrap', p: 0.5, borderBottom: 1, borderColor: 'divider' }}
      >
        <Tooltip title="Clear">
          <IconButton size="small" onClick={() => logStore.clear()}>
            <BlockIcon fontSize="small" />
          </IconButton>
        </Tooltip>
        <TextField
          size="small"
          placeholder="Filter"
          value={text}
          onChange={(event) => setText(event.target.value)}
          sx={{ flexGrow: 1, minWidth: 100, '& input': { py: 0.5, fontSize: 12 } }}
        />
        <Select
          size="small"
          displayEmpty
          value={domain}
          onChange={(event) => setDomain(event.target.value)}
          sx={{ fontSize: 12, '& .MuiSelect-select': { py: 0.5 } }}
        >
          <MenuItem value="">All domains</MenuItem>
          {domains.map((name) => (
            <MenuItem key={name} value={name}>
              {name}
            </MenuItem>
          ))}
        </Select>
        <ToggleButtonGroup size="small" value={levels} onChange={(_, next: LogLevel[]) => setLevels(next)}>
          {logLevels.map((level) => (
            <ToggleButton key={level} value={level} sx={{ py: 0, fontSize: 11, textTransform: 'none' }}>
              {levelLabels[level]}
            </ToggleButton>
          ))}
        </ToggleButtonGroup>
      </Stack>
      <Box ref={listRef} sx={{ flexGrow: 1, overflow: 'auto', minHeight: 0 }}>
        {visible.map((record) => (
          <LogRow
            key={record.id}
            record={record}
            expanded={
              record.trace ? (
                <>
                  <JsonView value={details(record)} />
                  <Button
                    size="small"
                    onClick={() => openLogs({ query: `trace:=${JSON.stringify(record.trace)}`, range: { relative: '24h' } })}
                  >
                    Show in Log Explorer
                  </Button>
                </>
              ) : undefined
            }
          />
        ))}
        {visible.length === 0 && (
          <Typography sx={{ p: 2, fontSize: 12, color: 'text.secondary', textAlign: 'center' }}>
            No log records
          </Typography>
        )}
      </Box>
    </Box>
  )
}

export default LogViewer
