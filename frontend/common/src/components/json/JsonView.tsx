import { useState } from 'react'
import {
  Box,
  IconButton,
  Stack,
  ToggleButton,
  ToggleButtonGroup,
  Tooltip,
} from '@mui/material'
import ArrowDropDownIcon from '@mui/icons-material/ArrowDropDown'
import ArrowRightIcon from '@mui/icons-material/ArrowRight'
import ContentCopyIcon from '@mui/icons-material/ContentCopy'
import WrapTextIcon from '@mui/icons-material/WrapText'

const mono = 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace'

// Value colours borrowed from Chrome DevTools' object viewer
const colors = {
  key: '#881391',
  string: '#c41a16',
  number: '#1c00cf',
  boolean: '#0d22aa',
  null: '#808080',
}

const openDepth = 2
const truncateAt = 200

type Mode = 'pretty' | 'raw'

// Strings that hold JSON are parsed so they can be shown as a tree
function parsed(value: unknown): unknown {
  if (typeof value !== 'string') return value
  try {
    return JSON.parse(value)
  } catch {
    return value
  }
}

function raw(value: unknown): string {
  if (typeof value === 'string') return value
  if (value === undefined) return ''
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

function LongString({ value }: { value: string }) {
  const [expanded, setExpanded] = useState(false)
  if (value.length <= truncateAt || expanded) {
    return <span style={{ color: colors.string }}>"{value}"</span>
  }
  return (
    <span
      style={{ color: colors.string, cursor: 'pointer' }}
      title="Click to expand"
      onClick={() => setExpanded(true)}
    >
      "{value.slice(0, truncateAt)}…"
    </span>
  )
}

function Primitive({ value }: { value: unknown }) {
  if (value === null || value === undefined) {
    return <span style={{ color: colors.null }}>{String(value)}</span>
  }
  switch (typeof value) {
    case 'string':
      return <LongString value={value} />
    case 'number':
    case 'bigint':
      return <span style={{ color: colors.number }}>{String(value)}</span>
    case 'boolean':
      return <span style={{ color: colors.boolean }}>{String(value)}</span>
    default:
      return <span>{String(value)}</span>
  }
}

function summary(value: object): string {
  if (Array.isArray(value)) return `Array(${value.length})`
  const keys = Object.keys(value)
  const shown = keys.slice(0, 4).join(', ')
  return `{${shown}${keys.length > 4 ? ', …' : ''}}`
}

function JsonNode({ name, value, depth }: { name?: string; value: unknown; depth: number }) {
  const [open, setOpen] = useState(depth < openDepth)
  const label = name !== undefined && (
    <>
      <span style={{ color: colors.key }}>{name}</span>:{' '}
    </>
  )

  if (value === null || typeof value !== 'object') {
    return (
      <div style={{ paddingLeft: 18 }}>
        {label}
        <Primitive value={value} />
      </div>
    )
  }

  const entries = Object.entries(value)
  const Arrow = open ? ArrowDropDownIcon : ArrowRightIcon
  return (
    <div>
      <div
        style={{ cursor: 'pointer', display: 'flex', alignItems: 'center' }}
        onClick={() => setOpen((open) => !open)}
      >
        <Arrow sx={{ fontSize: 18 }} />
        <span>
          {label}
          <span style={{ color: colors.null }}>{summary(value)}</span>
        </span>
      </div>
      {open && (
        <div style={{ paddingLeft: 14 }}>
          {entries.length === 0 ? (
            <div style={{ paddingLeft: 18, color: colors.null }}>
              {Array.isArray(value) ? '[]' : '{}'}
            </div>
          ) : (
            entries.map(([key, child]) => (
              <JsonNode key={key} name={key} value={child} depth={depth + 1} />
            ))
          )}
        </div>
      )}
    </div>
  )
}

// A JSON viewer with a collapsible pretty tree and the raw text
function JsonView({ value, defaultMode = 'pretty' }: { value: unknown; defaultMode?: Mode }) {
  const [mode, setMode] = useState<Mode>(defaultMode)
  const [wrap, setWrap] = useState(true)
  const text = raw(value)

  if (value === undefined || text === '') {
    return <Box sx={{ p: 1, color: 'text.secondary', fontSize: 12 }}>No content</Box>
  }

  return (
    <Box sx={{ fontFamily: mono, fontSize: 12 }}>
      <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center', pb: 0.5 }}>
        <ToggleButtonGroup
          size="small"
          exclusive
          value={mode}
          onChange={(_, next: Mode | null) => next && setMode(next)}
        >
          <ToggleButton value="pretty" sx={{ py: 0, fontSize: 11 }}>
            Pretty
          </ToggleButton>
          <ToggleButton value="raw" sx={{ py: 0, fontSize: 11 }}>
            Raw
          </ToggleButton>
        </ToggleButtonGroup>
        {mode === 'raw' && (
          <Tooltip title="Wrap lines">
            <IconButton size="small" color={wrap ? 'primary' : 'default'} onClick={() => setWrap((wrap) => !wrap)}>
              <WrapTextIcon fontSize="inherit" />
            </IconButton>
          </Tooltip>
        )}
        <Tooltip title="Copy">
          <IconButton size="small" onClick={() => void navigator.clipboard?.writeText(text)}>
            <ContentCopyIcon fontSize="inherit" />
          </IconButton>
        </Tooltip>
      </Stack>
      {mode === 'pretty' ? (
        <JsonNode value={parsed(value)} depth={0} />
      ) : (
        <pre
          style={{
            margin: 0,
            fontFamily: mono,
            whiteSpace: wrap ? 'pre-wrap' : 'pre',
            wordBreak: wrap ? 'break-all' : 'normal',
            overflowX: wrap ? 'hidden' : 'auto',
          }}
        >
          {text}
        </pre>
      )}
    </Box>
  )
}

export default JsonView
