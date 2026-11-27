// mui.calendar's pure layout (forge 28d): wall-clock times in a zone,
// the visible range of each view, overlapping items side by side, the
// all-day strip's lanes and snapping. No React and only type imports, so
// node --test runs it (yarn test in frontend/common).

export type View = 'month' | 'week' | 'workWeek' | 'day' | 'agenda'

export interface Item {
  id: string
  title: string
  start: string // RFC 3339, or YYYY-MM-DD when allDay
  end: string // exclusive
  allDay?: boolean
  color?: string
  readOnly?: boolean
  group?: string
}

export interface Parts {
  y: number
  m: number // 1 to 12
  d: number
  h: number
  mi: number
}

const formatters = new Map<string, Intl.DateTimeFormat>()

function formatter(zone: string): Intl.DateTimeFormat {
  let f = formatters.get(zone)
  if (!f) {
    f = new Intl.DateTimeFormat('en-US', {
      timeZone: zone,
      hourCycle: 'h23',
      year: 'numeric',
      month: 'numeric',
      day: 'numeric',
      hour: 'numeric',
      minute: 'numeric',
      second: 'numeric',
    })
    formatters.set(zone, f)
  }
  return f
}

// The viewer's zone, UTC when the browser does not say
export function localZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
}

// A zone the browser knows, else UTC
export function validZone(zone: string | undefined): string {
  if (!zone) return localZone()
  try {
    formatter(zone)
    return zone
  } catch {
    return 'UTC'
  }
}

// An instant's wall clock in a zone
export function partsIn(ms: number, zone: string): Parts {
  const p: Record<string, number> = {}
  for (const x of formatter(zone).formatToParts(new Date(ms))) {
    if (x.type !== 'literal') p[x.type] = Number(x.value)
  }
  return { y: p.year, m: p.month, d: p.day, h: p.hour === 24 ? 0 : p.hour, mi: p.minute }
}

function offsetAt(ms: number, zone: string): number {
  const p = partsIn(ms, zone)
  const asUTC = Date.UTC(p.y, p.m - 1, p.d, p.h, p.mi)
  return asUTC - Math.floor(ms / 60000) * 60000
}

// A wall-clock time in a zone as an instant. A time in the spring gap moves
// forward by the gap; one in the autumn overlap takes the earlier instant
// (as the server's calendar/when does).
export function instantOf(p: Parts, zone: string): number {
  const naive = Date.UTC(p.y, p.m - 1, p.d, p.h, p.mi)
  const before = offsetAt(naive - 36 * 3600000, zone)
  const after = offsetAt(naive + 36 * 3600000, zone)
  // Try the earlier offset first (the larger one in an overlap)
  const candidates = before >= after ? [before, after] : [after, before]
  for (const off of candidates) {
    const t = naive - off
    const q = partsIn(t, zone)
    if (q.y === p.y && q.m === p.m && q.d === p.d && q.h === p.h && q.mi === p.mi) return t
  }
  // In a gap: the offset before it, which lands after the gap
  return naive - before
}

// YYYY-MM-DD helpers, on dates with no zone
export function parseDate(key: string): { y: number; m: number; d: number } {
  const [y, m, d] = key.split('-').map(Number)
  return { y, m, d }
}

export function dateKey(y: number, m: number, d: number): string {
  const dt = new Date(Date.UTC(y, m - 1, d))
  return dt.toISOString().slice(0, 10)
}

export function addDays(key: string, n: number): string {
  const { y, m, d } = parseDate(key)
  return dateKey(y, m, d + n)
}

export function addMonths(key: string, n: number): string {
  const { y, m, d } = parseDate(key)
  const last = new Date(Date.UTC(y, m - 1 + n + 1, 0)).getUTCDate()
  return dateKey(y, m + n, Math.min(d, last))
}

// 0 Sunday to 6 Saturday
export function weekday(key: string): number {
  const { y, m, d } = parseDate(key)
  return new Date(Date.UTC(y, m - 1, d)).getUTCDay()
}

export function daysBetween(a: string, b: string): number {
  const pa = parseDate(a)
  const pb = parseDate(b)
  return Math.round((Date.UTC(pb.y, pb.m - 1, pb.d) - Date.UTC(pa.y, pa.m - 1, pa.d)) / 86400000)
}

// The day an instant falls on in a zone
export function dayOf(ms: number, zone: string): string {
  const p = partsIn(ms, zone)
  return dateKey(p.y, p.m, p.d)
}

export function todayIn(zone: string, now = Date.now()): string {
  return dayOf(now, zone)
}

// The instant a day starts in a zone
export function startOfDay(key: string, zone: string): number {
  const { y, m, d } = parseDate(key)
  return instantOf({ y, m, d, h: 0, mi: 0 }, zone)
}

// Weeks start on Monday
export function weekStart(key: string): string {
  return addDays(key, -((weekday(key) + 6) % 7))
}

// The days a view shows, and the range of instants they cover
export function visibleDays(view: View, date: string): string[] {
  const days: string[] = []
  switch (view) {
    case 'day':
      return [date]
    case 'week':
    case 'workWeek': {
      const s = weekStart(date)
      for (let i = 0; i < (view === 'week' ? 7 : 5); i++) days.push(addDays(s, i))
      return days
    }
    case 'month': {
      const { y, m } = parseDate(date)
      const s = weekStart(dateKey(y, m, 1))
      for (let i = 0; i < 42; i++) days.push(addDays(s, i))
      return days
    }
    case 'agenda':
      for (let i = 0; i < 14; i++) days.push(addDays(date, i))
      return days
  }
}

export function visibleRange(view: View, date: string, zone: string): { from: string; to: string } {
  const days = visibleDays(view, date)
  return {
    from: new Date(startOfDay(days[0], zone)).toISOString(),
    to: new Date(startOfDay(addDays(days[days.length - 1], 1), zone)).toISOString(),
  }
}

// The date the toolbar's back and forward go to
export function step(view: View, date: string, dir: 1 | -1): string {
  switch (view) {
    case 'day':
      return addDays(date, dir)
    case 'week':
    case 'workWeek':
      return addDays(date, 7 * dir)
    case 'month':
      return addMonths(date, dir)
    case 'agenda':
      return addDays(date, 14 * dir)
  }
}

// An item's start and end as instants; an all-day item's in the zone
export function spanOf(it: Item, zone: string): { start: number; end: number } {
  if (it.allDay) {
    const s = startOfDay(it.start.slice(0, 10), zone)
    const e = it.end ? startOfDay(it.end.slice(0, 10), zone) : startOfDay(addDays(it.start.slice(0, 10), 1), zone)
    return { start: s, end: Math.max(e, s + 1) }
  }
  const s = Date.parse(it.start)
  const e = Date.parse(it.end)
  return { start: s, end: Number.isFinite(e) && e > s ? e : s + 30 * 60000 }
}

// Whether an item goes in the strip above the grid: all-day, or timed
// across midnight
export function inStrip(it: Item, zone: string): boolean {
  if (it.allDay) return true
  const { start, end } = spanOf(it, zone)
  return dayOf(start, zone) !== dayOf(end - 1, zone)
}

export interface Placed {
  id: string
  top: number // minutes from the day's start
  bottom: number
  col: number
  cols: number
}

// A day's timed items placed in columns: each cluster of overlapping items
// splits the width, each item in the first column free when it starts.
export function layoutDay(items: Item[], day: string, zone: string): Placed[] {
  const s = startOfDay(day, zone)
  const e = startOfDay(addDays(day, 1), zone)
  const dayMinutes = (e - s) / 60000
  const rows = items
    .map((it) => ({ it, ...spanOf(it, zone) }))
    .filter((r) => r.end > s && r.start < e)
    .map((r) => ({
      id: r.it.id,
      top: Math.max(0, (r.start - s) / 60000),
      bottom: Math.min(dayMinutes, (r.end - s) / 60000),
    }))
    .sort((a, b) => a.top - b.top || b.bottom - a.bottom || (a.id < b.id ? -1 : 1))
  const out: Placed[] = []
  let cluster: Placed[] = []
  let ends: number[] = [] // each column's last bottom
  let clusterEnd = -1
  const close = () => {
    for (const p of cluster) p.cols = ends.length
    out.push(...cluster)
    cluster = []
    ends = []
  }
  for (const r of rows) {
    // Items shorter than 15 minutes take 15 for overlap
    const bottom = Math.max(r.bottom, r.top + 15)
    if (r.top >= clusterEnd && cluster.length) close()
    let col = ends.findIndex((b) => b <= r.top)
    if (col < 0) {
      col = ends.length
      ends.push(bottom)
    } else ends[col] = bottom
    cluster.push({ ...r, col, cols: 0 })
    clusterEnd = Math.max(clusterEnd, bottom)
  }
  if (cluster.length) close()
  return out
}

export interface Lane {
  id: string
  lane: number
  first: number // index of the first visible day it covers
  last: number
  clippedStart: boolean
  clippedEnd: boolean
}

// Strip items (all-day and multi-day) given lanes over a run of days: the
// longest first, each in the first lane free over its days.
export function layoutStrip(items: Item[], days: string[], zone: string): Lane[] {
  if (!days.length) return []
  const rows = items
    .map((it) => {
      const { start, end } = spanOf(it, zone)
      const a = daysBetween(days[0], dayOf(start, zone))
      const b = daysBetween(days[0], dayOf(end - 1, zone))
      return { id: it.id, a, b }
    })
    .filter((r) => r.b >= 0 && r.a < days.length)
    .sort((x, y) => x.a - y.a || y.b - y.a - (x.b - x.a) || (x.id < y.id ? -1 : 1))
  const lanes: number[] = [] // each lane's last day taken
  return rows.map((r) => {
    const first = Math.max(0, r.a)
    const last = Math.min(days.length - 1, r.b)
    let lane = lanes.findIndex((l) => l < first)
    if (lane < 0) {
      lane = lanes.length
      lanes.push(last)
    } else lanes[lane] = last
    return { id: r.id, lane, first, last, clippedStart: r.a < 0, clippedEnd: r.b > days.length - 1 }
  })
}

// Items on one day (for a month cell or the agenda), strip items first,
// then by start
export function itemsOn(items: Item[], day: string, zone: string): Item[] {
  const s = startOfDay(day, zone)
  const e = startOfDay(addDays(day, 1), zone)
  return items
    .map((it) => ({ it, sp: spanOf(it, zone), strip: inStrip(it, zone) }))
    .filter((r) => r.sp.end > s && r.sp.start < e)
    .sort((a, b) => Number(b.strip) - Number(a.strip) || a.sp.start - b.sp.start || (a.it.title < b.it.title ? -1 : 1))
    .map((r) => r.it)
}

// Minutes snapped to the slot
export function snap(minutes: number, slot: number): number {
  return Math.round(minutes / slot) * slot
}

// An item moved to start at a minute of a day, keeping its length; given
// as RFC 3339 UTC instants
export function moved(it: Item, day: string, minute: number, zone: string): { start: string; end: string } {
  const { start, end } = spanOf(it, zone)
  const { y, m, d } = parseDate(day)
  const s = instantOf({ y, m, d, h: Math.floor(minute / 60), mi: minute % 60 }, zone)
  return { start: iso(s), end: iso(s + (end - start)) }
}

// An all-day item moved by whole days
export function movedDays(it: Item, days: number, zone: string): { start: string; end: string } {
  if (it.allDay) {
    const s = it.start.slice(0, 10)
    const e = (it.end || addDays(s, 1)).slice(0, 10)
    return { start: addDays(s, days), end: addDays(e, days) }
  }
  const { start, end } = spanOf(it, zone)
  const p = partsIn(start, zone)
  const day = addDays(dateKey(p.y, p.m, p.d), days)
  const { y, m, d } = parseDate(day)
  const s = instantOf({ y, m, d, h: p.h, mi: p.mi }, zone)
  return { start: iso(s), end: iso(s + (end - start)) }
}

// The instant of a minute of a day
export function at(day: string, minute: number, zone: string): string {
  const { y, m, d } = parseDate(day)
  return iso(instantOf({ y, m, d, h: Math.floor(minute / 60), mi: minute % 60 }, zone))
}

export function iso(ms: number): string {
  return new Date(ms).toISOString().replace('.000Z', 'Z')
}

// "09:30" in a zone
export function clock(ms: number, zone: string): string {
  const p = partsIn(ms, zone)
  return `${String(p.h).padStart(2, '0')}:${String(p.mi).padStart(2, '0')}`
}

// "HH:MM" as minutes
export function minutesOf(hhmm: string | undefined, fallback: number): number {
  if (!hhmm) return fallback
  const [h, m] = hhmm.split(':').map(Number)
  return Number.isFinite(h) ? h * 60 + (Number.isFinite(m) ? m : 0) : fallback
}

// The items in props, dropping malformed ones
export function itemsFrom(v: unknown): Item[] {
  if (!Array.isArray(v)) return []
  const out: Item[] = []
  for (const x of v) {
    if (!x || typeof x !== 'object') continue
    const r = x as Record<string, unknown>
    const id = r.id === undefined || r.id === null ? '' : String(r.id)
    const start = typeof r.start === 'string' ? r.start : ''
    if (!id || !start) continue
    out.push({
      id,
      title: typeof r.title === 'string' ? r.title : '',
      start,
      end: typeof r.end === 'string' ? r.end : '',
      allDay: r.allDay === true || /^\d{4}-\d{2}-\d{2}$/.test(start),
      color: typeof r.color === 'string' ? r.color : undefined,
      readOnly: r.readOnly === true,
      group: r.group === undefined || r.group === null ? undefined : String(r.group),
    })
  }
  return out
}
