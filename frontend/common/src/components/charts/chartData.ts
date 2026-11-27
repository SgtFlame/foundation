// mui.chart's data (forge 40a), pure so it is tested with node --test: the
// series a chart draws, from its series prop or from rows (records.aggregate
// groups) read by dotted field paths; at most maxSeries series, the rest
// folded into Other; the rows Recharts draws; and value formatting.

import { maxSeries } from './palette.ts'

export interface Point {
  x: string
  y: number
}

export interface Series {
  key: string
  label: string
  points: Point[]
  other?: boolean // the folded series, drawn grey
}

export type Format = 'number' | 'money' | 'percent' | 'duration'

export const otherKey = '__other'

// The value at a dotted path (key.stage), or undefined
export function getPath(obj: unknown, path: string): unknown {
  let cur = obj
  for (const part of path.split('.')) {
    if (cur === null || typeof cur !== 'object') return undefined
    cur = (cur as Record<string, unknown>)[part]
  }
  return cur
}

// A value as an x: an expanded record by its id, a list by its first item
export function xOf(v: unknown): string {
  if (v === undefined || v === null) return ''
  if (Array.isArray(v)) return xOf(v[0])
  if (typeof v === 'object') {
    const r = v as Record<string, unknown>
    return xOf(r._id ?? r.id ?? r.value ?? '')
  }
  return String(v)
}

export function num(v: unknown): number {
  if (typeof v === 'number') return Number.isFinite(v) ? v : 0
  if (typeof v === 'string') {
    const n = Number(v) // decimals travel as strings
    return Number.isFinite(n) ? n : 0
  }
  return 0
}

export interface ChartInput {
  series?: unknown
  rows?: unknown
  xField?: string
  yField?: string
  seriesField?: string
  labels?: Record<string, unknown>
}

// The series of a chart's props, points summed per x within a series
export function seriesFrom(p: ChartInput): Series[] {
  const label = (v: string) => {
    const l = p.labels?.[v]
    return l === undefined || l === null ? v : String(l)
  }
  if (Array.isArray(p.series)) {
    return p.series.map((s, i) => {
      const r = (s ?? {}) as Record<string, unknown>
      const key = r.key === undefined ? `s${i}` : String(r.key)
      const pts = Array.isArray(r.points) ? r.points : []
      return {
        key,
        label: r.label === undefined ? label(key) : String(r.label),
        points: sumByX(pts.map((pt) => ({ x: xOf(getPath(pt, 'x')), y: num(getPath(pt, 'y')) }))),
      }
    })
  }
  const rows = Array.isArray(p.rows) ? p.rows : rowsOf(p.rows)
  const xf = p.xField || 'x'
  const yf = p.yField || 'y'
  const by = new Map<string, Point[]>()
  for (const row of rows) {
    const key = p.seriesField ? xOf(getPath(row, p.seriesField)) : 'value'
    if (!by.has(key)) by.set(key, [])
    by.get(key)!.push({ x: xOf(getPath(row, xf)), y: num(getPath(row, yf)) })
  }
  return [...by].map(([key, pts]) => ({ key, label: p.seriesField ? label(key) : (p.yField ? lastPart(p.yField) : 'value'), points: sumByX(pts) }))
}

// A records.aggregate result ({groups}) or a records.find page ({records})
function rowsOf(v: unknown): unknown[] {
  if (v && typeof v === 'object') {
    const r = v as Record<string, unknown>
    if (Array.isArray(r.groups)) return r.groups
    if (Array.isArray(r.records)) return r.records
  }
  return []
}

const lastPart = (path: string) => path.split('.').pop() || path

function sumByX(points: Point[]): Point[] {
  const out = new Map<string, number>()
  for (const pt of points) out.set(pt.x, (out.get(pt.x) ?? 0) + pt.y)
  return [...out].map(([x, y]) => ({ x, y }))
}

const total = (s: Series) => s.points.reduce((a, p) => a + Math.abs(p.y), 0)

// Keeps the max largest series (by total, in their given order) and folds
// the rest into one Other series
export function foldOther(series: Series[], max = maxSeries): Series[] {
  if (series.length <= max) return series
  const keep = new Set([...series].sort((a, b) => total(b) - total(a)).slice(0, max).map((s) => s.key))
  const kept = series.filter((s) => keep.has(s.key))
  const rest = series.filter((s) => !keep.has(s.key))
  return [...kept, { key: otherKey, label: 'Other', other: true, points: sumByX(rest.flatMap((s) => s.points)) }]
}

// The x values in order: those of order first (only those with points,
// unless all), then the rest as first seen
export function xsOf(series: Series[], order?: unknown[], all = false): string[] {
  const seen: string[] = []
  const has = new Set<string>()
  const add = (x: string) => {
    if (!has.has(x)) {
      has.add(x)
      seen.push(x)
    }
  }
  const present = new Set(series.flatMap((s) => s.points.map((p) => p.x)))
  for (const o of order ?? []) if (all || present.has(xOf(o))) add(xOf(o))
  for (const s of series) for (const p of s.points) add(p.x)
  return seen
}

// One row per x with a field per series (s0, s1…, never the series key,
// which may hold dots Recharts reads as paths)
export function tableOf(series: Series[], xs: string[]): Record<string, string | number>[] {
  return xs.map((x) => {
    const row: Record<string, string | number> = { x }
    series.forEach((s, i) => {
      row[`s${i}`] = s.points.find((p) => p.x === x)?.y ?? 0
    })
    return row
  })
}

// Slices of a pie or stages of a funnel: every series summed per x; a pie
// keeps the max largest slices and folds the rest into Other, a funnel
// keeps every stage in order
export function slicesOf(series: Series[], xs: string[], kind: 'pie' | 'funnel', max = maxSeries): { x: string; y: number; other?: boolean }[] {
  const all = xs.map((x) => ({ x, y: series.reduce((a, s) => a + (s.points.find((p) => p.x === x)?.y ?? 0), 0) }))
  if (kind === 'funnel' || all.length <= max) return all
  const keep = new Set([...all].sort((a, b) => Math.abs(b.y) - Math.abs(a.y)).slice(0, max).map((s) => s.x))
  const rest = all.filter((s) => !keep.has(s.x))
  return [...all.filter((s) => keep.has(s.x)), { x: otherKey, y: rest.reduce((a, s) => a + s.y, 0), other: true }]
}

export function formatValue(v: number, format: Format = 'number', currency = 'USD', locale?: string): string {
  switch (format) {
    case 'money':
      try {
        return new Intl.NumberFormat(locale, { style: 'currency', currency, maximumFractionDigits: Math.abs(v) >= 1000 ? 0 : 2 }).format(v)
      } catch {
        return `${v} ${currency}`
      }
    case 'percent':
      return new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 1 }).format(v)
    case 'duration':
      return duration(v)
    default:
      return new Intl.NumberFormat(locale, { maximumFractionDigits: 2 }).format(v)
  }
}

function duration(seconds: number): string {
  const s = Math.round(Math.abs(seconds))
  const sign = seconds < 0 ? '-' : ''
  if (s < 60) return `${sign}${s}s`
  if (s < 3600) return `${sign}${Math.floor(s / 60)}m ${s % 60}s`.replace(/ 0s$/, '')
  if (s < 86400) return `${sign}${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`.replace(/ 0m$/, '')
  return `${sign}${Math.floor(s / 86400)}d ${Math.floor((s % 86400) / 3600)}h`.replace(/ 0h$/, '')
}

// The change from compare to value: its fraction (null when compare is 0
// or missing) and its direction
export function deltaOf(value: unknown, compare: unknown): { fraction: number | null; direction: 'up' | 'down' | 'flat' } | null {
  if (compare === undefined || compare === null || compare === '') return null
  const v = num(value)
  const c = num(compare)
  const direction = v > c ? 'up' : v < c ? 'down' : 'flat'
  return { fraction: c === 0 ? null : (v - c) / Math.abs(c), direction }
}
