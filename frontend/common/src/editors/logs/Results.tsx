import { useEffect, useMemo, useRef } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import {
  Box,
  Button,
  IconButton,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Tooltip,
  Typography,
} from '@mui/material'
import AddCircleOutlineIcon from '@mui/icons-material/AddCircleOutlineOutlined'
import RemoveCircleOutlineIcon from '@mui/icons-material/RemoveCircleOutlineOutlined'
import LogRow from '../../components/logs/LogRow.tsx'
import type { LogRow as StoredRow } from '../../services/Logs.ts'
import { recordFromRow, surroundingQuery } from './view.ts'

export interface ResultActions {
  onFilter: (field: string, value: string, exclude?: boolean) => void
  onQuery: (query: string) => void // replaces the query, e.g. with the surrounding records
}

// Every field of a stored record, each with filter buttons, and its error stack
function RowDetails({ row, onFilter, onQuery }: { row: StoredRow } & ResultActions) {
  const fields = Object.entries(row)
    .filter(([key]) => key !== 'error.stack')
    .sort(([a], [b]) => a.localeCompare(b))
  const surrounding = surroundingQuery(row)
  return (
    <Box sx={{ py: 0.5 }}>
      <Stack direction="row" spacing={1} sx={{ mb: 0.5 }}>
        {row.trace && (
          <Button size="small" onClick={() => onQuery(`trace:=${JSON.stringify(row.trace)}`)}>
            Show this trace
          </Button>
        )}
        {surrounding && (
          <Button size="small" onClick={() => onQuery(surrounding)}>
            Surrounding records
          </Button>
        )}
      </Stack>
      <Table size="small" sx={{ '& td': { fontSize: 12, py: 0.25, px: 0.5, verticalAlign: 'top', borderBottom: 0 } }}>
        <TableBody>
          {fields.map(([key, value]) => (
            <TableRow key={key} hover>
              <TableCell sx={{ width: 32, whiteSpace: 'nowrap' }}>
                <Tooltip title={`Filter by ${key}`}>
                  <IconButton size="small" sx={{ p: 0 }} onClick={() => onFilter(key, value)} aria-label={`Filter by ${key}`}>
                    <AddCircleOutlineIcon sx={{ fontSize: 14 }} />
                  </IconButton>
                </Tooltip>
                <Tooltip title={`Exclude this ${key}`}>
                  <IconButton size="small" sx={{ p: 0 }} onClick={() => onFilter(key, value, true)} aria-label={`Exclude this ${key}`}>
                    <RemoveCircleOutlineIcon sx={{ fontSize: 14 }} />
                  </IconButton>
                </Tooltip>
              </TableCell>
              <TableCell sx={{ fontFamily: 'monospace', color: 'text.secondary', whiteSpace: 'nowrap' }}>{key}</TableCell>
              <TableCell sx={{ fontFamily: 'monospace', wordBreak: 'break-all' }}>{value}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {row['error.stack'] && (
        <Box component="pre" sx={{ fontSize: 11, m: 0, mt: 0.5, p: 0.5, bgcolor: 'action.hover', overflow: 'auto', maxHeight: 240 }}>
          {row['error.stack']}
        </Box>
      )}
    </Box>
  )
}

interface RecordListProps extends ResultActions {
  rows: StoredRow[]
  columns: string[] // fields shown as columns
  follow?: boolean // keeps the newest record, at the top, in view
}

// The matching records, newest first, as LogRows; only the visible ones render
export function RecordList({ rows, columns, follow = false, onFilter, onQuery }: RecordListProps) {
  const parentRef = useRef<HTMLDivElement>(null)
  const records = useMemo(() => rows.map((row, i) => recordFromRow(row, i)), [rows])
  const spansDays = useMemo(() => {
    if (records.length < 2) return false
    return records[0].time.toDateString() !== records[records.length - 1].time.toDateString()
  }, [records])
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 22,
    overscan: 20,
  })
  useEffect(() => {
    if (follow && rows.length) virtualizer.scrollToIndex(0)
  }, [follow, rows, virtualizer])

  return (
    <Box ref={parentRef} sx={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
      <Box sx={{ height: virtualizer.getTotalSize(), position: 'relative' }}>
        {virtualizer.getVirtualItems().map((item) => (
          <Box
            key={item.key}
            data-index={item.index}
            ref={virtualizer.measureElement}
            sx={{ position: 'absolute', top: 0, left: 0, width: '100%', transform: `translateY(${item.start}px)` }}
          >
            <LogRow
              record={records[item.index]}
              withDate={spansDays}
              columns={columns.map((key) => ({ key, value: rows[item.index][key] }))}
              onFilter={onFilter}
              expanded={<RowDetails row={rows[item.index]} onFilter={onFilter} onQuery={onQuery} />}
            />
          </Box>
        ))}
      </Box>
    </Box>
  )
}

// The rows of a query whose results are not records, such as stats
export function StatsTable({ rows }: { rows: StoredRow[] }) {
  const keys = useMemo(() => [...new Set(rows.flatMap((row) => Object.keys(row)))], [rows])
  return (
    <Box sx={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
      <Table size="small" stickyHeader sx={{ '& td, & th': { fontSize: 12, fontFamily: 'monospace', px: 1 } }}>
        <TableHead>
          <TableRow>
            {keys.map((key) => (
              <TableCell key={key}>{key}</TableCell>
            ))}
          </TableRow>
        </TableHead>
        <TableBody>
          {rows.map((row, i) => (
            <TableRow key={i} hover>
              {keys.map((key) => (
                <TableCell key={key}>{row[key] ?? ''}</TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {rows.length === 0 && (
        <Typography sx={{ p: 2, fontSize: 12, color: 'text.secondary', textAlign: 'center' }}>No results</Typography>
      )}
    </Box>
  )
}

// Records have a time; stats and similar pipes return rows without one
export function isRecords(rows: StoredRow[]): boolean {
  return rows.length === 0 || rows.some((row) => '_time' in row && '_msg' in row)
}
