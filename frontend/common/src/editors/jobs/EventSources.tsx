// The Jobs view's Event sources (forge 07c): each connection whose events
// an installed trigger listens to, who runs its source (adapter, instance
// and host, by lease), how old its cursor is, its last event and error,
// and a source whose type no adapter serves.

import { Alert, Chip, Table, TableBody, TableCell, TableHead, TableRow, Tooltip, Typography } from '@mui/material'
import { useQuery } from '../../live/useQuery.ts'
import type { SourceList } from '../../services/Connectors.ts'

function age(t?: string): string {
  if (!t || t.startsWith('0001-')) return ''
  const s = Math.max(0, Math.round((Date.now() - new Date(t).getTime()) / 1000))
  if (s < 60) return `${s} s ago`
  if (s < 3600) return `${Math.round(s / 60)} min ago`
  return `${Math.round(s / 3600)} h ago`
}

export default function EventSources() {
  const { data, error } = useQuery<SourceList>('connectors.sources', {})
  const sources = data?.sources ?? []
  if (error) return <Alert severity="error">{error.message}</Alert>
  if (sources.length === 0) {
    return <Typography color="text.secondary">No event sources. A connection gets one while an installed trigger listens to its events.</Typography>
  }
  return (
    <Table size="small" aria-label="Event sources">
      <TableHead>
        <TableRow>
          <TableCell>Connection</TableCell>
          <TableCell>Type</TableCell>
          <TableCell>Contracts</TableCell>
          <TableCell>Run by</TableCell>
          <TableCell>Cursor</TableCell>
          <TableCell>Last event</TableCell>
          <TableCell>Last error</TableCell>
        </TableRow>
      </TableHead>
      <TableBody>
        {sources.map((s) => (
          <TableRow key={s.connection}>
            <TableCell>{s.name || s.connection}</TableCell>
            <TableCell sx={{ fontFamily: 'monospace' }}>{s.type}</TableCell>
            <TableCell sx={{ fontFamily: 'monospace' }}>{s.contracts.join(', ')}</TableCell>
            <TableCell>
              {!s.online ? (
                <Chip size="small" color="warning" label="no adapter serves this type" />
              ) : s.holder ? (
                <Tooltip title={`instance ${s.holder.instance}, since ${new Date(s.holder.since).toLocaleString()}`}>
                  <span>
                    {s.holder.adapter} on {s.holder.host}
                  </span>
                </Tooltip>
              ) : (
                <Chip size="small" variant="outlined" label="waiting for an instance" />
              )}
            </TableCell>
            <TableCell>{age(s.cursorAt)}</TableCell>
            <TableCell sx={{ fontFamily: 'monospace' }}>{s.lastEvent}</TableCell>
            <TableCell sx={{ maxWidth: 280 }}>
              <Tooltip title={s.holder?.lastError ?? ''}>
                <Typography variant="body2" color="error" noWrap>
                  {s.holder?.lastError}
                </Typography>
              </Tooltip>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
