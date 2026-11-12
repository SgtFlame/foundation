import { useLayoutEffect, useRef, useState } from 'react'
import { Box, Stack, Table, TableBody, TableCell, TableHead, TableRow, Typography, useTheme } from '@mui/material'
import { formatCost } from '../../../components/chat/levels.ts'
import { otherColor, seriesColors } from './palette.ts'
import { other, type DaySeries } from './useUsage.ts'

const height = 200
const pad = { top: 8, right: 8, bottom: 22, left: 56 }
const gap = 2 // the surface gap between bars and between stacked segments
const radius = 4

// A bar segment with its top corners rounded, for the top of a stack
function roundedTop(x: number, y: number, w: number, h: number): string {
  const r = Math.min(radius, w / 2, h)
  return `M${x},${y + h}V${y + r}Q${x},${y} ${x + r},${y}H${x + w - r}Q${x + w},${y} ${x + w},${y + r}V${y + h}Z`
}

// A round axis maximum: 1, 2 or 5 times a power of ten, in micros
function niceMax(v: number): number {
  if (v <= 0) return 10_000 // one cent
  const p = 10 ** Math.floor(Math.log10(v))
  return ([1, 2, 5, 10].find((m) => m * p >= v) ?? 10) * p
}

const shortDay = (day: string) => new Date(`${day}T00:00`).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })

// Cost per day, stacked by group, with a legend, a hover tooltip per day,
// and a table view of the same numbers
function CostChart({ series, label, table }: { series: DaySeries; label: (group: string) => string; table: boolean }) {
  const theme = useTheme()
  const box = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(600)
  const [hover, setHover] = useState<number>()
  useLayoutEffect(() => {
    const el = box.current
    if (!el) return
    const observer = new ResizeObserver(() => setWidth(el.clientWidth))
    observer.observe(el)
    setWidth(el.clientWidth)
    return () => observer.disconnect()
  }, [table])

  const mode = theme.palette.mode
  const colors = seriesColors(mode)
  const colorOf = (g: string) => (g === other ? otherColor(mode) : colors[series.groups.indexOf(g) % colors.length])
  const dayTotal = (d: string) => Object.values(series.cost[d]).reduce((a, b) => a + b, 0)
  const max = niceMax(Math.max(...series.days.map(dayTotal)))
  const plotW = Math.max(width - pad.left - pad.right, 100)
  const plotH = height - pad.top - pad.bottom
  const slot = plotW / series.days.length
  const barW = Math.max(slot - gap, 1)
  const y = (micros: number) => (micros / max) * plotH
  const ticks = [0, max / 2, max]

  if (table) {
    return (
      <Box sx={{ overflowX: 'auto', maxHeight: 320 }}>
        <Table size="small" stickyHeader aria-label="Cost per day">
          <TableHead>
            <TableRow>
              <TableCell>Day</TableCell>
              {series.groups.map((g) => (
                <TableCell key={g} align="right">
                  {label(g)}
                </TableCell>
              ))}
              <TableCell align="right">Total</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {[...series.days].reverse().map((d) => (
              <TableRow key={d}>
                <TableCell>{shortDay(d)}</TableCell>
                {series.groups.map((g) => (
                  <TableCell key={g} align="right">
                    {series.cost[d][g] ? formatCost(series.cost[d][g]) : '–'}
                  </TableCell>
                ))}
                <TableCell align="right">{formatCost(dayTotal(d))}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Box>
    )
  }

  const hovered = hover !== undefined ? series.days[hover] : undefined
  return (
    <Box>
      {series.groups.length > 1 && (
        <Stack direction="row" sx={{ flexWrap: 'wrap', columnGap: 2, rowGap: 0.5, mb: 1 }} aria-label="Legend">
          {series.groups.map((g) => (
            <Typography key={g} variant="caption" sx={{ display: 'flex', alignItems: 'center', gap: 0.5, color: 'text.secondary' }}>
              <Box component="span" sx={{ width: 10, height: 10, borderRadius: '2px', bgcolor: colorOf(g) }} />
              {label(g)}
            </Typography>
          ))}
        </Stack>
      )}
      <Box ref={box} sx={{ position: 'relative' }} onMouseLeave={() => setHover(undefined)}>
        <svg width={width} height={height} role="img" aria-label="Cost per day over the last 30 days" style={{ display: 'block' }}>
          {ticks.map((t) => (
            <g key={t}>
              <line x1={pad.left} x2={pad.left + plotW} y1={pad.top + plotH - y(t)} y2={pad.top + plotH - y(t)} stroke={theme.palette.divider} />
              <text x={pad.left - 6} y={pad.top + plotH - y(t)} dy="0.32em" textAnchor="end" fontSize={11} fill={theme.palette.text.secondary}>
                {formatCost(t).replace(/0+$/, '').replace(/\.$/, '')}
              </text>
            </g>
          ))}
          {series.days.map((d, i) => {
            const x = pad.left + i * slot + gap / 2
            let base = pad.top + plotH
            const parts = series.groups.filter((g) => (series.cost[d][g] ?? 0) > 0)
            return (
              <g key={d} opacity={hover === undefined || hover === i ? 1 : 0.45}>
                {parts.map((g, j) => {
                  const h = Math.max(y(series.cost[d][g]) - (j > 0 ? gap : 0), 1)
                  base -= h + (j > 0 ? gap : 0)
                  const top = j === parts.length - 1
                  return top ? (
                    <path key={g} d={roundedTop(x, base, barW, h)} fill={colorOf(g)} />
                  ) : (
                    <rect key={g} x={x} y={base} width={barW} height={h} fill={colorOf(g)} />
                  )
                })}
                {/* a hit target the full height of the plot */}
                <rect x={pad.left + i * slot} y={pad.top} width={slot} height={plotH} fill="transparent" onMouseEnter={() => setHover(i)} />
              </g>
            )
          })}
          {series.days.map((d, i) =>
            i % 7 === series.days.length % 7 || i === series.days.length - 1 ? (
              <text key={d} x={pad.left + i * slot + slot / 2} y={height - 6} textAnchor="middle" fontSize={11} fill={theme.palette.text.secondary}>
                {shortDay(d)}
              </text>
            ) : null,
          )}
        </svg>
        {hovered && (
          <Box
            role="tooltip"
            sx={{
              position: 'absolute',
              top: 0,
              left: Math.min(pad.left + (hover ?? 0) * slot + slot + 8, width - 200),
              width: 190,
              p: 1,
              borderRadius: 1,
              bgcolor: 'background.paper',
              boxShadow: 3,
              pointerEvents: 'none',
            }}
          >
            <Typography variant="caption" sx={{ fontWeight: 600, display: 'block' }}>
              {shortDay(hovered)} · {formatCost(dayTotal(hovered))}
            </Typography>
            {series.groups
              .filter((g) => series.cost[hovered][g])
              .map((g) => (
                <Typography key={g} variant="caption" sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
                  <Box component="span" sx={{ width: 8, height: 8, borderRadius: '2px', bgcolor: colorOf(g), flexShrink: 0 }} />
                  <Box component="span" sx={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {label(g)}
                  </Box>
                  {formatCost(series.cost[hovered][g])}
                </Typography>
              ))}
          </Box>
        )}
      </Box>
    </Box>
  )
}

export default CostChart
