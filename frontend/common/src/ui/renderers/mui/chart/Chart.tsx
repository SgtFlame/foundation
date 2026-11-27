// mui.chart (forge 40a): bar, stacked bar, line, area, pie and funnel over a
// series prop or aggregate rows, in the dashboard palette, drawn as SVG by
// Recharts (lazy-loaded with this renderer). The data is chartData.ts (pure,
// tested); this picks colours for the theme's mode, formats values, animates
// changes (Recharts' own transitions) and sends select {series, x} on a click.

import { useMemo } from 'react'
import { Box, Typography, useTheme } from '@mui/material'
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Funnel,
  FunnelChart,
  LabelList,
  Legend,
  Line,
  LineChart,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { RendererProps } from '../../../points.ts'
import { otherColor, seriesColors } from '../../../../components/charts/palette.ts'
import { foldOther, formatValue, otherKey, seriesFrom, slicesOf, tableOf, xsOf, type Format } from '../../../../components/charts/chartData.ts'

type Kind = 'bar' | 'stackedBar' | 'line' | 'area' | 'pie' | 'funnel'
const KINDS: Kind[] = ['bar', 'stackedBar', 'line', 'area', 'pie', 'funnel']
const FORMATS: Format[] = ['number', 'money', 'percent', 'duration']

export default function MuiChart(p: RendererProps) {
  const theme = useTheme()
  const mode = theme.palette.mode
  const props = p.props
  const kind: Kind = KINDS.includes(props.kind as Kind) ? (props.kind as Kind) : 'bar'
  const format: Format = FORMATS.includes(props.format as Format) ? (props.format as Format) : 'number'
  const currency = typeof props.currency === 'string' && props.currency ? props.currency : 'USD'
  const height = typeof props.height === 'number' && props.height > 0 ? props.height : 240
  const title = typeof props.title === 'string' ? props.title : undefined
  const labels = useMemo(() => (props.labels && typeof props.labels === 'object' ? props.labels : {}) as Record<string, unknown>, [props.labels])

  const { series, xs } = useMemo(() => {
    const s = foldOther(
      seriesFrom({
        series: props.series,
        rows: props.rows,
        xField: props.xField as string | undefined,
        yField: props.yField as string | undefined,
        seriesField: props.seriesField as string | undefined,
        labels,
      }),
    )
    return { series: s, xs: xsOf(s, Array.isArray(props.order) ? props.order : undefined, props.kind === 'funnel') }
  }, [props.series, props.rows, props.xField, props.yField, props.seriesField, props.order, props.kind, labels])

  const colors = seriesColors(mode)
  const grey = otherColor(mode)
  const colorAt = (i: number, other?: boolean) => (other ? grey : colors[i % colors.length])
  const xLabel = (x: string) => (x === otherKey ? 'Other' : labels[x] !== undefined && labels[x] !== null ? String(labels[x]) : x)
  const fmt = (v: unknown) => formatValue(typeof v === 'number' ? v : Number(v) || 0, format, currency)
  const legend = typeof props.legend === 'boolean' ? props.legend : series.length > 1
  const select = (seriesKey: string, x: string) => {
    if (p.handles('select') && x !== otherKey && seriesKey !== otherKey) p.emit('select', { series: seriesKey, x })
  }
  const clickable = p.handles('select') ? 'pointer' : undefined
  const axis = { stroke: theme.palette.text.secondary, fontSize: 11 }
  const tooltip = (
    <Tooltip
      formatter={(v) => fmt(v)}
      labelFormatter={(l) => xLabel(String(l))}
      contentStyle={{ background: theme.palette.background.paper, border: `1px solid ${theme.palette.divider}`, borderRadius: 8, color: theme.palette.text.primary }}
    />
  )

  let chart
  if (kind === 'pie' || kind === 'funnel') {
    const slices = slicesOf(series, xs, kind).map((s, i) => ({ id: s.x, y: s.y, name: xLabel(s.x), fill: colorAt(i, s.other) }))
    const onSlice = (d: unknown) => select(seriesKey, String((d as { payload?: { id?: string } })?.payload?.id ?? ''))
    const seriesKey = series.length === 1 ? series[0].key : ''
    chart =
      kind === 'pie' ? (
        <PieChart>
          {tooltip}
          {legend !== false && <Legend formatter={(v) => <span style={{ color: theme.palette.text.primary }}>{v}</span>} />}
          <Pie data={slices} dataKey="y" nameKey="name" innerRadius="45%" outerRadius="80%" paddingAngle={1} stroke={theme.palette.background.paper}
            onClick={onSlice} style={{ cursor: clickable }}>
            {slices.map((s) => <Cell key={s.id} fill={s.fill} />)}
          </Pie>
        </PieChart>
      ) : (
        <FunnelChart>
          {tooltip}
          <Funnel data={slices} dataKey="y" nameKey="name" isAnimationActive
            onClick={onSlice} style={{ cursor: clickable }}>
            {slices.map((s) => <Cell key={s.id} fill={s.fill} stroke={theme.palette.background.paper} />)}
            <LabelList position="right" dataKey="name" fill={theme.palette.text.primary} stroke="none" fontSize={12} />
            <LabelList position="center" dataKey="y" fill="#fff" stroke="none" fontSize={12} formatter={(v: unknown) => fmt(v)} />
          </Funnel>
        </FunnelChart>
      )
  } else {
    const data = tableOf(series, xs)
    const stacked = kind === 'stackedBar' || props.stacked === true
    const common = (
      <>
        <CartesianGrid stroke={theme.palette.divider} vertical={false} />
        <XAxis dataKey="x" tickFormatter={(x) => xLabel(String(x))} tick={axis} stroke={theme.palette.divider} />
        <YAxis tickFormatter={(v) => fmt(v)} tick={axis} stroke={theme.palette.divider} width={64} />
        {tooltip}
        {legend && <Legend formatter={(v) => <span style={{ color: theme.palette.text.primary }}>{v}</span>} />}
      </>
    )
    const onPoint = (key: string) => (d: { payload?: { x?: string } }) => select(key, String(d?.payload?.x ?? ''))
    if (kind === 'line') {
      chart = (
        <LineChart data={data}>
          {common}
          {series.map((s, i) => (
            <Line key={s.key} type="monotone" dataKey={`s${i}`} name={s.label} stroke={colorAt(i, s.other)} strokeWidth={2} dot={{ r: 3 }}
              activeDot={{ r: 5, onClick: (_e: unknown, d: unknown) => onPoint(s.key)(d as { payload?: { x?: string } }), style: { cursor: clickable } }} />
          ))}
        </LineChart>
      )
    } else if (kind === 'area') {
      chart = (
        <AreaChart data={data}>
          {common}
          {series.map((s, i) => (
            <Area key={s.key} type="monotone" dataKey={`s${i}`} name={s.label} stackId={stacked ? 'a' : undefined} stroke={colorAt(i, s.other)} fill={colorAt(i, s.other)} fillOpacity={0.25}
              activeDot={{ r: 5, onClick: (_e: unknown, d: unknown) => onPoint(s.key)(d as { payload?: { x?: string } }), style: { cursor: clickable } }} />
          ))}
        </AreaChart>
      )
    } else {
      chart = (
        <BarChart data={data}>
          {common}
          {series.map((s, i) => (
            <Bar key={s.key} dataKey={`s${i}`} name={s.label} stackId={stacked ? 'a' : undefined} fill={colorAt(i, s.other)} radius={stacked && i < series.length - 1 ? 0 : [4, 4, 0, 0]}
              onClick={onPoint(s.key)} style={{ cursor: clickable }} />
          ))}
        </BarChart>
      )
    }
  }

  return (
    <Box sx={props.sx as object} role="figure" aria-label={title ?? 'Chart'}>
      {title && <Typography variant="subtitle2" sx={{ mb: 1 }}>{title}</Typography>}
      {xs.length === 0 ? (
        <Box sx={{ height, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <Typography variant="body2" color="text.secondary">No data</Typography>
        </Box>
      ) : (
        <Box sx={{ height, width: '100%' }}>
          <ResponsiveContainer width="100%" height="100%">{chart}</ResponsiveContainer>
        </Box>
      )}
    </Box>
  )
}
