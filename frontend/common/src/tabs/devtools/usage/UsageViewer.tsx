import { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  Box,
  IconButton,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TablePagination,
  Tooltip,
  Typography,
} from '@mui/material'
import RefreshIcon from '@mui/icons-material/Refresh'
import SubjectIcon from '@mui/icons-material/Subject'
import { log } from '../../../logging/index.ts'
import { Usage, type UsageRecord } from '../../../services/Usage.ts'
import CaptureDialog from './CaptureDialog.tsx'

const pageSize = 50

function cost(r: UsageRecord): string {
  if (!r.currency) return '–'
  const amount = r.costMicros / 1_000_000
  return `${r.currency === 'USD' ? '$' : r.currency + ' '}${amount < 0.01 && amount > 0 ? amount.toFixed(6) : amount.toFixed(4)}`
}

function tokens(r: UsageRecord): string {
  if (r.embedTokens) return `${r.embedTokens}${r.estimated ? ' (est.)' : ''}`
  const parts = [`${r.inputTokens} in`]
  if (r.cachedTokens) parts.push(`${r.cachedTokens} cached`)
  parts.push(`${r.outputTokens} out`)
  if (r.thinkingTokens) parts.push(`${r.thinkingTokens} thinking`)
  return parts.join(' · ')
}

// The raw usage records, newest first, until the Dashboard exists
function UsageViewer() {
  const [records, setRecords] = useState<UsageRecord[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(0)
  const [error, setError] = useState<string>()
  const [captured, setCaptured] = useState<string>()

  const load = useCallback(() => {
    Usage.records({ page, pageSize })
      .then((resp) => {
        setError(undefined)
        setRecords(resp.records ?? [])
        setTotal(resp.total)
      })
      .catch((err: unknown) => {
        log.In('usage').fromError(err).warn('cannot list usage records')
        setError(err instanceof Error ? err.message : String(err))
      })
  }, [page])
  useEffect(load, [load])

  const shownCost = records.reduce((sum, r) => sum + (r.currency ? r.costMicros : 0), 0) / 1_000_000

  return (
    <Stack sx={{ height: '100%' }}>
      <Stack direction="row" sx={{ alignItems: 'center', px: 1 }}>
        <Typography variant="caption" sx={{ flex: 1, color: 'text.secondary' }}>
          {total} AI calls · this page ${shownCost.toFixed(4)}
        </Typography>
        <Tooltip title="Reload">
          <IconButton size="small" onClick={load} aria-label="Reload the usage records">
            <RefreshIcon fontSize="small" />
          </IconButton>
        </Tooltip>
      </Stack>
      {error && (
        <Alert severity="warning" sx={{ mx: 1 }}>
          Usage records are unavailable: {error}
        </Alert>
      )}
      <Box sx={{ flex: 1, overflow: 'auto' }}>
        <Table size="small" stickyHeader sx={{ '& td, & th': { fontSize: 12, whiteSpace: 'nowrap', px: 1 } }}>
          <TableHead>
            <TableRow>
              <TableCell>Time</TableCell>
              <TableCell>Feature</TableCell>
              <TableCell>Model</TableCell>
              <TableCell>Tokens</TableCell>
              <TableCell align="right">Cost</TableCell>
              <TableCell align="right">ms</TableCell>
              <TableCell align="right">Status</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {records.map((r) => (
              <TableRow key={r.id} hover>
                <TableCell>{new Date(r.time).toLocaleString()}</TableCell>
                <TableCell>
                  {r.feature}
                  {r.level ? ` · ${r.level}` : ''}
                  {r.captured && (
                    <Tooltip title="Show the captured prompt and reply">
                      <IconButton size="small" sx={{ p: 0, ml: 0.5 }} onClick={() => setCaptured(r.id)} aria-label="Show the captured prompt and reply">
                        <SubjectIcon sx={{ fontSize: 14 }} />
                      </IconButton>
                    </Tooltip>
                  )}
                </TableCell>
                <TableCell>{r.model}</TableCell>
                <TableCell>{tokens(r)}</TableCell>
                <TableCell align="right">{cost(r)}</TableCell>
                <TableCell align="right">{r.latencyMs}</TableCell>
                <TableCell align="right" sx={{ color: r.status >= 400 ? 'error.main' : undefined }}>
                  {r.status}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Box>
      <TablePagination
        component="div"
        count={total}
        page={page}
        rowsPerPage={pageSize}
        rowsPerPageOptions={[pageSize]}
        onPageChange={(_, p) => setPage(p)}
      />
      <CaptureDialog recordId={captured} onClose={() => setCaptured(undefined)} />
    </Stack>
  )
}

export default UsageViewer
