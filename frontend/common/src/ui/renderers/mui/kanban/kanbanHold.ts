// mui.kanban's pure logic (forge 30b), kept apart so it is tested with
// node --test: where each card is drawn, given what the server says and
// the drops this screen holds; what a drop reports (the cards it fell
// between); and where the keyboard moves a lifted card.
//
// A drop is held: the card stays where it was dropped until the server's
// version of it changes (the move committed, or someone else moved it
// later, whose move then wins) or the hold expires, when the card goes
// back to where the server has it.

export interface Placed {
  id: string
  column: string
  version?: unknown
  key?: string // the rendered child that draws it
}

export interface Hold {
  id: string
  column: string
  index: number // within the column, the card itself left out
  version: unknown // the server's version when it was dropped
  at: number // when, in ms
}

export const HOLD_MS = 5000

const str = (v: unknown) => (v === undefined || v === null ? '' : String(v))

// placedFrom reads the cards: the server's placed list when it sent one
// (inside a prefab), else the cards themselves by columnField.
export function placedFrom(props: { placed?: unknown; cards?: unknown; columnField?: unknown }): Placed[] {
  if (Array.isArray(props.placed)) {
    return props.placed.map((p) => {
      const o = (p ?? {}) as Record<string, unknown>
      return { id: str(o.id), column: str(o.column), version: o.version, key: o.key === undefined ? undefined : str(o.key) }
    })
  }
  let cards = props.cards
  if (cards && !Array.isArray(cards) && typeof cards === 'object') cards = (cards as { records?: unknown }).records
  if (!Array.isArray(cards)) return []
  const field = str(props.columnField) || 'status'
  const seen = new Set<string>()
  const out: Placed[] = []
  cards.forEach((c, i) => {
    const o = (c ?? {}) as Record<string, unknown>
    const id = str(o._id ?? o.id) || String(i)
    if (seen.has(id)) return
    seen.add(id)
    out.push({ id, column: columnOf(o[field]), version: o._seq })
  })
  return out
}

function columnOf(v: unknown): string {
  if (Array.isArray(v)) return v.length ? columnOf(v[0]) : ''
  if (v && typeof v === 'object') {
    const o = v as Record<string, unknown>
    return str(o._id ?? o.id)
  }
  return str(v)
}

// live keeps the holds still in force: a hold ends when its card is gone,
// its server version changed, or it is older than ttl.
export function live(holds: Hold[], placed: Placed[], now: number, ttl = HOLD_MS): { holds: Hold[]; expired: Hold[] } {
  const byId = new Map(placed.map((p) => [p.id, p]))
  const kept: Hold[] = []
  const expired: Hold[] = []
  for (const h of holds) {
    const p = byId.get(h.id)
    if (!p || !same(p.version, h.version)) continue
    if (now - h.at >= ttl) {
      expired.push(h)
      continue
    }
    kept.push(h)
  }
  return { holds: kept, expired }
}

const same = (a: unknown, b: unknown) => str(a) === str(b)

// layout is each column's cards in order: the server's order, with every
// held card moved to where it was dropped (oldest hold first). Cards of
// no known column are left out.
export function layout(placed: Placed[], columns: string[], holds: Hold[]): Record<string, Placed[]> {
  const out: Record<string, Placed[]> = {}
  for (const c of columns) out[c] = []
  const held = new Map(holds.map((h) => [h.id, h]))
  for (const p of placed) {
    if (held.has(p.id)) continue
    out[p.column]?.push(p)
  }
  const byId = new Map(placed.map((p) => [p.id, p]))
  for (const h of [...holds].sort((a, b) => a.at - b.at)) {
    const p = byId.get(h.id)
    const col = out[h.column]
    if (!p || !col) continue
    col.splice(Math.max(0, Math.min(h.index, col.length)), 0, { ...p, column: h.column })
  }
  return out
}

// between gives the cards a card dropped at index in a column falls
// between: after is the one above, before the one below ('' at an end),
// the card itself left out of the column.
export function between(column: Placed[], id: string, index: number): { after: string; before: string; index: number } {
  const rest = column.filter((p) => p.id !== id)
  const i = Math.max(0, Math.min(index, rest.length))
  return { after: rest[i - 1]?.id ?? '', before: rest[i]?.id ?? '', index: i }
}

// unmoved tells whether a drop leaves the card where it was.
export function unmoved(cols: Record<string, Placed[]>, id: string, to: string, index: number): boolean {
  const from = Object.keys(cols).find((c) => cols[c].some((p) => p.id === id))
  if (from !== to) return false
  const rest = cols[to].filter((p) => p.id !== id)
  return cols[to].findIndex((p) => p.id === id) === Math.max(0, Math.min(index, rest.length))
}

// step moves a lifted card by the keyboard: up and down within its
// column, left and right to the next column (keeping its index where it
// can), never past an end.
export function step(
  cols: Record<string, Placed[]>,
  order: string[],
  at: { column: string; index: number },
  id: string,
  key: 'ArrowUp' | 'ArrowDown' | 'ArrowLeft' | 'ArrowRight',
): { column: string; index: number } {
  const rest = (c: string) => (cols[c] ?? []).filter((p) => p.id !== id).length
  if (key === 'ArrowUp') return { column: at.column, index: Math.max(0, at.index - 1) }
  if (key === 'ArrowDown') return { column: at.column, index: Math.min(rest(at.column), at.index + 1) }
  const i = order.indexOf(at.column) + (key === 'ArrowLeft' ? -1 : 1)
  if (i < 0 || i >= order.length) return at
  const column = order[i]
  return { column, index: Math.min(at.index, rest(column)) }
}
