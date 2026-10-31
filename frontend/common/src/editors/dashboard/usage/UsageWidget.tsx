import { useState } from 'react'
import {
  Alert,
  Box,
  CircularProgress,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from '@mui/material'
import { formatCost, formatTokens, useLevels } from '../../../components/chat/levels.ts'
import type { SummaryGroup, UsageTotals } from '../../../services/Usage.ts'
import type { DashboardWidgetProps } from '../points.ts'
import CostChart from './CostChart.tsx'
import { useUsage } from './useUsage.ts'

function Tile({ label, totals }: { label: string; totals: UsageTotals }) {
  const cachedShare = totals.inputTokens > 0 ? Math.round((totals.cachedTokens / totals.inputTokens) * 100) : 0
  return (
    <Box sx={{ minWidth: 150, flex: 1 }}>
      <Typography variant="caption" color="text.secondary">
        {label}
      </Typography>
      <Typography variant="h5" component="p" sx={{ fontVariantNumeric: 'tabular-nums' }}>
        {formatCost(totals.costMicros)}
      </Typography>
      <Typography variant="caption" color="text.secondary" component="p">
        {totals.requests} requests · {formatTokens(totals.inputTokens)} in
        {cachedShare > 0 ? ` (${cachedShare}% cached)` : ''} · {formatTokens(totals.outputTokens + totals.thinkingTokens)} out
      </Typography>
    </Box>
  )
}

// Usage & cost (documentation/Usage.md › Dashboard): totals, cost per
// day stacked by level, model or feature, the costliest conversations, and
// what indexing cost
function UsageWidget({ refresh }: DashboardWidgetProps) {
  const [groupBy, setGroupBy] = useState<SummaryGroup>('level')
  const [table, setTable] = useState(false)
  const { data, error } = useUsage(groupBy, refresh)
  const { levels } = useLevels()

  const label = (g: string) => {
    if (groupBy === 'level') {
      if (g === '(none)') return 'No level (embeddings, other calls)'
      const level = levels.find((l) => l.id === g)
      return level ? `${level.icon ?? ''} ${level.label}`.trim() : g
    }
    return g
  }

  if (error && !data) return <Alert severity="warning">{error}</Alert>
  if (!data) return <CircularProgress size={24} />
  return (
    <Stack spacing={2.5}>
      <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 2 }}>
        <Tile label="Today" totals={data.today} />
        <Tile label="Last 7 days" totals={data.week} />
        <Tile label="Last 30 days" totals={data.month} />
        <Box sx={{ minWidth: 150, flex: 1 }}>
          <Typography variant="caption" color="text.secondary">
            Indexing, 30 days
          </Typography>
          <Typography variant="h5" component="p" sx={{ fontVariantNumeric: 'tabular-nums' }}>
            {formatCost(data.indexing.costMicros)}
          </Typography>
          <Typography variant="caption" color="text.secondary" component="p">
            {formatTokens(data.indexing.embedTokens)} tokens embedded
          </Typography>
        </Box>
      </Stack>

      <Box>
        <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1, mb: 1 }}>
          <Typography variant="subtitle2" sx={{ flex: 1 }}>
            Cost per day, last 30 days
          </Typography>
          <ToggleButtonGroup size="small" exclusive value={groupBy} onChange={(_, v: SummaryGroup | null) => v && setGroupBy(v)} aria-label="Stack by">
            <ToggleButton value="level">Level</ToggleButton>
            <ToggleButton value="model">Model</ToggleButton>
            <ToggleButton value="feature">Feature</ToggleButton>
          </ToggleButtonGroup>
          <ToggleButtonGroup size="small" exclusive value={table ? 'table' : 'chart'} onChange={(_, v: string | null) => v && setTable(v === 'table')} aria-label="View">
            <ToggleButton value="chart">Chart</ToggleButton>
            <ToggleButton value="table">Table</ToggleButton>
          </ToggleButtonGroup>
        </Stack>
        {data.month.requests === 0 ? (
          <Typography variant="body2" color="text.secondary">
            No AI calls in the last 30 days.
          </Typography>
        ) : (
          <CostChart series={data.series} label={label} table={table} />
        )}
      </Box>

      <Box>
        <Typography variant="subtitle2" sx={{ mb: 0.5 }}>
          Costliest conversations, last 30 days
        </Typography>
        {data.top.length === 0 ? (
          <Typography variant="body2" color="text.secondary">
            No conversations with a cost yet.
          </Typography>
        ) : (
          <Table size="small" aria-label="Costliest conversations">
            <TableHead>
              <TableRow>
                <TableCell>Conversation</TableCell>
                <TableCell align="right">Requests</TableCell>
                <TableCell align="right">Cost</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {data.top.map((c) => (
                <TableRow key={c.id}>
                  <TableCell>{c.title}</TableCell>
                  <TableCell align="right">{c.requests}</TableCell>
                  <TableCell align="right">{formatCost(c.costMicros)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Box>
    </Stack>
  )
}

export default UsageWidget
