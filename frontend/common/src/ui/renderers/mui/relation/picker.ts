// mui.relationPicker's rules (forge 40b), pure so they are tested with
// node --test: a value as ids and back, picking and removing in single and
// multiple mode, when to offer Create "…", narrowing search results by
// where, the query a keystroke sends, and the debounce between keystrokes.

export const str = (v: unknown) => (v === undefined || v === null ? '' : String(v))

// A value (an id, a list of ids, or nothing) as the ids it holds, each once
export function idsOf(value: unknown): string[] {
  const list = Array.isArray(value) ? value : value === undefined || value === null || value === '' ? [] : [value]
  const out: string[] = []
  for (const v of list) {
    const id = typeof v === 'object' && v !== null ? str((v as Record<string, unknown>)._id ?? (v as Record<string, unknown>).id) : str(v)
    if (id && !out.includes(id)) out.push(id)
  }
  return out
}

// The ids as the value sent: a list when multiple, else one id or null
export function valueOf(ids: string[], multiple: boolean): string | string[] | null {
  return multiple ? ids : (ids[0] ?? null)
}

// Picking an id: multiple adds it once at the end, single replaces
export function pick(ids: string[], id: string, multiple: boolean): string[] {
  if (!id) return ids
  if (!multiple) return [id]
  return ids.includes(id) ? ids : [...ids, id]
}

export function remove(ids: string[], id: string): string[] {
  return ids.filter((x) => x !== id)
}

// Create "…" is offered for typed text no shown option is already titled
// (ignoring case and surrounding spaces)
export function offerCreate(create: boolean, text: string, titles: string[]): string | null {
  const t = text.trim()
  if (!create || !t) return null
  const lower = t.toLowerCase()
  return titles.some((x) => x.trim().toLowerCase() === lower) ? null : t
}

// Whether a record has every where value (a relation compared by id, a
// list field by holding it), for search results the server did not narrow
export function matchesWhere(record: Record<string, unknown>, where?: Record<string, unknown>): boolean {
  if (!where) return true
  return Object.entries(where).every(([field, want]) => {
    const have = record[field]
    const wants = idsOf(want)
    if (Array.isArray(have)) return wants.every((w) => idsOf(have).includes(w))
    if (typeof have === 'object' && have !== null) return idsOf(have)[0] === wants[0]
    return str(have) === str(want)
  })
}

export interface SearchQuery {
  kind: 'search' | 'find'
  request: Record<string, unknown>
}

// What one keystroke asks: records.search over searchable fields when the
// type has them and two characters are typed, else a records.find prefix
// scan of the title field through its index (the first page by title when
// nothing is typed)
export function queryFor(entity: string, text: string, opts: { searchable: boolean; titleField?: string; where?: Record<string, unknown>; limit?: number }): SearchQuery {
  const t = text.trim()
  const limit = opts.limit ?? 20
  if (opts.searchable && t.length >= 2) return { kind: 'search', request: { entity, text: t, limit: limit * 2 } }
  const request: Record<string, unknown> = { entity, limit }
  if (opts.where && Object.keys(opts.where).length > 0) request.where = opts.where
  if (opts.titleField) {
    request.sort = [{ field: opts.titleField }]
    if (t) request.filter = { field: opts.titleField, op: 'prefix', value: t }
  }
  return { kind: 'find', request }
}

// Runs fn wait milliseconds after the last call; a later call replaces an
// earlier one, and cancel drops the pending call
export function debounce<A extends unknown[]>(fn: (...args: A) => void, wait: number): { call(...args: A): void; cancel(): void } {
  let timer: ReturnType<typeof setTimeout> | undefined
  return {
    call(...args: A) {
      if (timer !== undefined) clearTimeout(timer)
      timer = setTimeout(() => {
        timer = undefined
        fn(...args)
      }, wait)
    },
    cancel() {
      if (timer !== undefined) clearTimeout(timer)
      timer = undefined
    },
  }
}
