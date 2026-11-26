import { useState, type ReactNode } from 'react'
import { Box, Chip, Collapse, Stack } from '@mui/material'
import BugReportOutlinedIcon from '@mui/icons-material/BugReportOutlined'
import ErrorIcon from '@mui/icons-material/Error'
import InfoOutlinedIcon from '@mui/icons-material/InfoOutlined'
import WarningIcon from '@mui/icons-material/Warning'
import type { LogLevel, LogRecord } from '../../logging/index.ts'
import JsonView from '../json/JsonView.tsx'

export const levelStyles: Record<LogLevel, { icon: typeof InfoOutlinedIcon; color?: string; bg?: string }> = {
  debug: { icon: BugReportOutlinedIcon, color: 'text.secondary' },
  info: { icon: InfoOutlinedIcon },
  warn: { icon: WarningIcon, color: '#5c3c00', bg: '#fffbe5' },
  error: { icon: ErrorIcon, color: '#e00', bg: '#fff0f0' },
}

function time(date: Date, withDate: boolean): string {
  const clock = date.toLocaleTimeString(undefined, { hour12: false }) + `.${String(date.getMilliseconds()).padStart(3, '0')}`
  return withDate ? `${date.toLocaleDateString(undefined, { month: '2-digit', day: '2-digit' })} ${clock}` : clock
}

// The oops context of a record, shown when it is expanded
export function details(record: LogRecord): Record<string, unknown> {
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

export interface LogRowProps {
  record: LogRecord
  withDate?: boolean // for records that span days
  columns?: { key: string; value?: string }[] // extra values shown before the message
  onFilter?: (field: string, value: string) => void // makes the domain, tags and code filter on click
  expanded?: ReactNode // replaces the default view of the context when expanded
}

// One log record: DevTools' Log tab and the Log Explorer both render these
function LogRow({ record, withDate = false, columns = [], onFilter, expanded }: LogRowProps) {
  const [open, setOpen] = useState(false)
  const style = levelStyles[record.level]
  const Icon = style.icon
  const extra = details(record)
  const expandable = expanded !== undefined || Object.keys(extra).some((key) => key !== 'domain' && key !== 'tags')

  // A value that adds a filter on click, without toggling the row
  const filterable = (field: string, value: string, children: ReactNode) =>
    onFilter ? (
      <Box
        component="span"
        title={`Filter by ${field} = ${value}`}
        onClick={(event) => {
          event.stopPropagation()
          onFilter(field, value)
        }}
        sx={{ cursor: 'pointer', '&:hover': { textDecoration: 'underline' } }}
      >
        {children}
      </Box>
    ) : (
      children
    )

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
          {time(record.time, withDate)}
        </Box>
        {record.domain && (
          <Box component="span" sx={{ fontWeight: 600, flexShrink: 0 }}>
            {filterable('domain', record.domain, `[${record.domain}]`)}
          </Box>
        )}
        {record.tags.map((tag) => (
          <Chip
            key={tag}
            label={tag}
            size="small"
            sx={{ height: 16, fontSize: 10 }}
            onClick={
              onFilter
                ? (event) => {
                    event.stopPropagation()
                    onFilter('tags', tag)
                  }
                : undefined
            }
          />
        ))}
        {columns.map(({ key, value }) => (
          <Box key={key} component="span" title={key} sx={{ fontFamily: 'monospace', color: 'text.secondary', flexShrink: 0, maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {value === undefined ? '–' : filterable(key, value, value)}
          </Box>
        ))}
        <Box component="span" sx={{ fontFamily: 'monospace', wordBreak: 'break-word', flexGrow: 1 }}>
          {record.message}
        </Box>
        {record.code && (
          <Box component="span" sx={{ color: 'text.secondary', flexShrink: 0 }}>
            {filterable('code', record.code, record.code)}
          </Box>
        )}
      </Stack>
      {expandable && (
        <Collapse in={open} unmountOnExit>
          <Box sx={{ pl: 4, pr: 1, pb: 0.5, color: 'text.primary' }}>{expanded ?? <JsonView value={extra} />}</Box>
        </Collapse>
      )}
    </Box>
  )
}

export default LogRow
