import { useEffect, useMemo, useRef, useState } from 'react'
import { Box, Stack, Typography } from '@mui/material'
import type { HitsSeries } from '../../services/Logs.ts'

// Bar colours by level; other values (an unlevelled record) are grey
const levelColors: Record<string, string> = {
  debug: '#9e9e9e',
  info: '#64b5f6',
  warn: '#ffb300',
  error: '#e53935',
}
const stackOrder = ['error', 'warn', 'info', 'debug']

const height = 72
const axis = 14

interface HistogramProps {
  series: HitsSeries[]
  stepMs: number
  start: string
  end: string
  onSelect: (start: string, end: string) => void // dragged across: narrow the range
}

interface Bucket {
  time: number
  counts: { level: string; count: number }[]
  total: number
}

// The matching records per step, stacked by level
function Histogram({ series, stepMs, start, end, onSelect }: HistogramProps) {
  const ref = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(600)
  const [drag, setDrag] = useState<{ from: number; to: number }>()

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const observer = new ResizeObserver(([entry]) => setWidth(Math.max(100, entry.contentRect.width)))
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  const from = Date.parse(start)
  const to = Date.parse(end)
  const span = Math.max(1, to - from)

  const buckets = useMemo(() => {
    const byTime = new Map<number, Bucket>()
    for (const s of series) {
      const level = s.fields.level ?? ''
      s.timestamps.forEach((ts, i) => {
        const time = Date.parse(ts)
        const bucket = byTime.get(time) ?? { time, counts: [], total: 0 }
        if (s.values[i]) {
          bucket.counts.push({ level, count: s.values[i] })
          bucket.total += s.values[i]
        }
        byTime.set(time, bucket)
      })
    }
    for (const b of byTime.values()) {
      b.counts.sort((x, y) => stackOrder.indexOf(y.level) - stackOrder.indexOf(x.level))
    }
    return [...byTime.values()].filter((b) => b.total > 0)
  }, [series])

  const max = Math.max(1, ...buckets.map((b) => b.total))
  const x = (time: number) => ((time - from) / span) * width
  const barWidth = Math.max(1, (stepMs / span) * width - 1)
  const timeAt = (px: number) => from + (Math.min(Math.max(px, 0), width) / width) * span
  const total = buckets.reduce((sum, b) => sum + b.total, 0)

  const local = (event: React.PointerEvent) => event.clientX - (ref.current?.getBoundingClientRect().left ?? 0)
  const label = (time: number) =>
    new Date(time).toLocaleString(undefined, span > 36 * 3600_000
      ? { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false }
      : { hour: '2-digit', minute: '2-digit', second: span < 3600_000 ? '2-digit' : undefined, hour12: false })

  return (
    <Box sx={{ px: 1, pt: 0.5 }}>
      <Stack direction="row" sx={{ justifyContent: 'space-between' }}>
        <Typography variant="caption" sx={{ color: 'text.secondary' }}>
          {total.toLocaleString()} records · drag across the chart to zoom in
        </Typography>
        <Stack direction="row" spacing={1}>
          {stackOrder.map((level) => (
            <Typography key={level} variant="caption" sx={{ color: 'text.secondary', display: 'flex', alignItems: 'center', gap: 0.5 }}>
              <Box component="span" sx={{ width: 8, height: 8, bgcolor: levelColors[level], display: 'inline-block' }} />
              {level}
            </Typography>
          ))}
        </Stack>
      </Stack>
      <Box
        ref={ref}
        sx={{ position: 'relative', cursor: 'crosshair', userSelect: 'none', touchAction: 'none' }}
        onPointerDown={(event) => {
          event.currentTarget.setPointerCapture(event.pointerId)
          const px = local(event)
          setDrag({ from: px, to: px })
        }}
        onPointerMove={(event) => drag && setDrag({ ...drag, to: local(event) })}
        onPointerUp={() => {
          if (drag && Math.abs(drag.to - drag.from) > 3) {
            const a = timeAt(Math.min(drag.from, drag.to))
            const b = timeAt(Math.max(drag.from, drag.to))
            onSelect(new Date(a).toISOString(), new Date(b).toISOString())
          }
          setDrag(undefined)
        }}
      >
        <svg width={width} height={height + axis} role="img" aria-label="Records over time by level">
          {buckets.map((b) => {
            let y = height
            return (
              <g key={b.time}>
                <title>{`${label(b.time)}: ${b.counts.map((c) => `${c.count} ${c.level || 'other'}`).join(', ')}`}</title>
                {b.counts.map((c) => {
                  const h = (c.count / max) * (height - 2)
                  y -= h
                  return <rect key={c.level} x={x(b.time)} y={y} width={barWidth} height={h} fill={levelColors[c.level] ?? '#bdbdbd'} />
                })}
              </g>
            )
          })}
          <line x1={0} x2={width} y1={height + 0.5} y2={height + 0.5} stroke="currentColor" strokeOpacity={0.2} />
          {[0, 0.25, 0.5, 0.75, 1].map((f) => (
            <text
              key={f}
              x={f * width}
              y={height + axis - 2}
              fontSize={10}
              fill="currentColor"
              fillOpacity={0.6}
              textAnchor={f === 0 ? 'start' : f === 1 ? 'end' : 'middle'}
            >
              {label(from + f * span)}
            </text>
          ))}
          {drag && (
            <rect
              x={Math.min(drag.from, drag.to)}
              y={0}
              width={Math.abs(drag.to - drag.from)}
              height={height}
              fill="#1976d2"
              fillOpacity={0.15}
              stroke="#1976d2"
            />
          )}
        </svg>
      </Box>
    </Box>
  )
}

export default Histogram
