import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Box,
  Chip,
  Collapse,
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
import BugReportOutlinedIcon from '@mui/icons-material/BugReportOutlined'
import ErrorIcon from '@mui/icons-material/Error'
import InfoOutlinedIcon from '@mui/icons-material/InfoOutlined'
import WarningIcon from '@mui/icons-material/Warning'
import { logLevels, logStore, type LogLevel, type LogRecord } from '../../../logging/index.ts'
import JsonView from '../json/JsonView.tsx'
import { useStore } from '../useStore.ts'

const levelLabels: Record<LogLevel, string> = {
  debug: 'Verbose',
  info: 'Info',
  warn: 'Warnings',
  error: 'Errors',
}

const levelStyles: Record<LogLevel, { icon: typeof InfoOutlinedIcon; color?: string; bg?: string }> = {
  debug: { icon: BugReportOutlinedIcon, color: 'text.secondary' },
  info: { icon: InfoOutlinedIcon },
  warn: { icon: WarningIcon, color: '#5c3c00', bg: '#fffbe5' },
  error: { icon: ErrorIcon, color: '#e00', bg: '#fff0f0' },
}

function time(date: Date): string {
  return date.toLocaleTimeString(undefined, { hour12: false }) + `.${String(date.getMilliseconds()).padStart(3, '0')}`
}

// The oops context of a record, shown when it is expanded
function details(record: LogRecord): Record<string, unknown> {
  const fields: Record<string, unknown> = {
    domain: record.domain,
    tags: record.tags.length ? record.tags : undefined,
    code: record.code,
    trace: record.trace,
    span: record.span,
    hint: record.hint,
    public: record.public,
    user: record.user,
    attrs: Object.keys(record.attrs).length ? record.attrs : undefined,
    error: record.error,
  }
  return Object.fromEntries(Object.entries(fields).filter(([, value]) => value !== undefined))
}

function LogRow({ record }: { record: LogRecord }) {
  const [open, setOpen] = useState(false)
  const style = levelStyles[record.level]
  const Icon = style.icon
  const extra = details(record)
  const expandable = Object.keys(extra).some((key) => key !== 'domain' && key !== 'tags')

  return (
    <Box sx={{ borderBottom: 1, borderColor: 'divider', bgcolor: style.bg, color: style.color, fontSize: 12 }}>
      <Stack
        direction="row"
        spacing={0.75}
        onClick={() => expandable && setOpen((open) => !open)}
        sx={{ alignItems: 'flex-start', px: 1, py: 0.25, cursor: expandable ? 'pointer' : 'default' }}
      >
        <Icon sx={{ fontSize: 14, mt: '1px' }} />
        <Box component="span" sx={{ color: 'text.secondary', fontFamily: 'monospace', flexShrink: 0 }}>
          {time(record.time)}
        </Box>
        {record.domain && (
          <Box component="span" sx={{ fontWeight: 600, flexShrink: 0 }}>
            [{record.domain}]
          </Box>
        )}
        {record.tags.map((tag) => (
          <Chip key={tag} label={tag} size="small" sx={{ height: 16, fontSize: 10 }} />
        ))}
        <Box component="span" sx={{ fontFamily: 'monospace', wordBreak: 'break-word', flexGrow: 1 }}>
          {record.message}
        </Box>
        {record.code && (
          <Box component="span" sx={{ color: 'text.secondary', flexShrink: 0 }}>
            {record.code}
          </Box>
        )}
      </Stack>
      {expandable && (
        <Collapse in={open} unmountOnExit>
          <Box sx={{ pl: 4, pr: 1, pb: 0.5, color: 'text.primary' }}>
            <JsonView value={extra} />
          </Box>
        </Collapse>
      )}
    </Box>
  )
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
          <LogRow key={record.id} record={record} />
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
