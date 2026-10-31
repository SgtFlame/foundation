// The Usage & cost widget's data: totals for today, 7 and 30 days, the last
// 30 days' cost per day by a group, the costliest conversations and the
// cost of indexing, all from usageservice.Summary.

import { useEffect, useState } from 'react'
import { log } from '../../../logging/index.ts'
import { Conversations } from '../../../services/Conversation.ts'
import { Usage, type SummaryGroup, type UsageTotals } from '../../../services/Usage.ts'
import { maxSeries } from './palette.ts'

export interface DaySeries {
  days: string[] // "2026-10-23", oldest first, in the viewer's time zone
  groups: string[] // costliest first; "Other" last when groups were folded
  cost: Record<string, Record<string, number>> // micros, by day then group
}

export interface TopConversation {
  id: string
  title: string
  costMicros: number
  requests: number
}

export interface UsageData {
  today: UsageTotals
  week: UsageTotals
  month: UsageTotals
  indexing: UsageTotals // embed-index over 30 days
  series: DaySeries
  top: TopConversation[]
}

export const other = 'Other'

// Local midnight `days` days ago, as RFC 3339
function startOfDay(daysAgo: number): Date {
  const d = new Date()
  d.setHours(0, 0, 0, 0)
  d.setDate(d.getDate() - daysAgo)
  return d
}

function dayKey(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

async function load(groupBy: SummaryGroup): Promise<UsageData> {
  const tzOffsetMinutes = -new Date().getTimezoneOffset()
  const from = (days: number) => startOfDay(days).toISOString()
  const month = from(29)
  const [today, week, monthTotals, indexing, perDay, perGroup, convs] = await Promise.all([
    Usage.summary({ from: from(0) }),
    Usage.summary({ from: from(6) }),
    Usage.summary({ from: month }),
    Usage.summary({ from: month, feature: 'embed-index' }),
    Usage.summary({ from: month, bucket: 'day', tzOffsetMinutes }),
    Usage.summary({ from: month, bucket: 'day', groupBy, tzOffsetMinutes, limit: maxSeries }),
    Usage.summary({ from: month, groupBy: 'conversation', feature: 'chat', limit: 6 }),
  ])

  const days = Array.from({ length: 30 }, (_, i) => dayKey(startOfDay(29 - i)))
  const cost: DaySeries['cost'] = Object.fromEntries(days.map((d) => [d, {}]))
  const totals: Record<string, number> = {}
  for (const r of perGroup.rows ?? []) {
    const g = r.group || '(none)'
    if (!cost[r.bucket ?? '']) continue
    cost[r.bucket ?? ''][g] = (cost[r.bucket ?? ''][g] ?? 0) + r.costMicros
    totals[g] = (totals[g] ?? 0) + r.costMicros
  }
  const groups = Object.keys(totals).sort((a, b) => totals[b] - totals[a])
  // What the kept groups miss of each day's total is Other
  let folded = false
  for (const r of perDay.rows ?? []) {
    const day = cost[r.bucket ?? '']
    if (!day) continue
    const rest = r.costMicros - Object.values(day).reduce((a, b) => a + b, 0)
    if (rest > 0) {
      day[other] = rest
      folded = true
    }
  }
  if (folded) groups.push(other)

  const rows = (convs.rows ?? []).filter((r) => r.group && r.group !== '0').slice(0, 5)
  const titles = new Map<string, string>()
  if (rows.length > 0) {
    const list = await Conversations.list({ ids: rows.map((r) => r.group ?? ''), limit: 50 })
    for (const c of list.conversations ?? []) titles.set(c.id, c.title || 'Untitled conversation')
  }
  const top = rows.map((r) => ({
    id: r.group ?? '',
    title: titles.get(r.group ?? '') ?? 'Deleted conversation',
    costMicros: r.costMicros,
    requests: r.requests,
  }))
  return { today: today.totals, week: week.totals, month: monthTotals.totals, indexing: indexing.totals, series: { days, groups, cost }, top }
}

export function useUsage(groupBy: SummaryGroup, refresh: number) {
  const [data, setData] = useState<UsageData>()
  const [error, setError] = useState<string>()
  useEffect(() => {
    let live = true
    load(groupBy)
      .then((d) => {
        if (!live) return
        setData(d)
        setError(undefined)
      })
      .catch((err: unknown) => {
        log.In('usage').fromError(err).warn('cannot load the usage summary')
        if (live) setError(err instanceof Error ? err.message : String(err))
      })
    return () => {
      live = false
    }
  }, [groupBy, refresh])
  return { data, error }
}
