// The notification centre's logic (forge 26e-2), free of React so it is
// tested with node --test: which notifications are new since the last
// look, and how long ago one was sent.

export interface Notification {
  _id: string
  title?: string
  body?: string
  link?: string
  kind?: string
  app?: string
  read?: boolean
  created?: string
}

// fresh are the unread notifications not in seen, oldest first; seen then
// holds every id given. The first call (seen undefined) finds none, so a
// page that opens with notifications waiting does not toast them all.
export function fresh(list: readonly Notification[], seen: Set<string> | undefined): { fresh: Notification[]; seen: Set<string> } {
  const next = new Set(list.map((n) => n._id))
  if (!seen) return { fresh: [], seen: next }
  const out = list.filter((n) => !n.read && !seen.has(n._id))
  out.sort((a, b) => (a.created ?? '').localeCompare(b.created ?? ''))
  return { fresh: out, seen: new Set([...seen, ...next]) }
}

// ago says how long ago a time was, shortly: now, 5m, 3h, 2d, else the date.
export function ago(when: string | undefined, now: number = Date.now()): string {
  if (!when) return ''
  const t = Date.parse(when)
  if (Number.isNaN(t)) return ''
  const s = Math.max(0, Math.round((now - t) / 1000))
  if (s < 60) return 'now'
  if (s < 3600) return `${Math.floor(s / 60)}m`
  if (s < 86400) return `${Math.floor(s / 3600)}h`
  if (s < 7 * 86400) return `${Math.floor(s / 86400)}d`
  return new Date(t).toLocaleDateString()
}

// countOf reads notifications.count's result: {groups: [{values: {count}}]}.
export function countOf(v: unknown): number {
  const groups = (v as { groups?: { values?: { count?: number } }[] } | undefined)?.groups
  const n = groups?.[0]?.values?.count
  return typeof n === 'number' ? n : 0
}
