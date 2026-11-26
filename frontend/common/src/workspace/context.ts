// A workspace's active knowledge (forge 16c): per kind of source, the
// items whose check differs from the kind's default. Every item the person
// can read is listed in every workspace; a Volume is checked unless set
// false, a Chronicle entry unchecked unless set true (question 10), as the
// server's workspace.Context. Kept free of React and the services so
// `node --test` runs its tests.

export type WorkspaceContext = Record<string, Record<string, boolean>>

export const kindVolume = 'volume'
export const kindChronicle = 'chronicle'

// Whether a kind's items are checked when a workspace says nothing of them
export const checkedByDefault = (kind: string) => kind === kindVolume

// A workspace's stored context; anything else is the defaults
export function parseContext(raw: unknown): WorkspaceContext {
  const out: WorkspaceContext = {}
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return out
  for (const [kind, items] of Object.entries(raw as Record<string, unknown>)) {
    if (!items || typeof items !== 'object' || Array.isArray(items)) continue
    const kept: Record<string, boolean> = {}
    for (const [id, on] of Object.entries(items as Record<string, unknown>)) {
      if (typeof on === 'boolean') kept[id] = on
    }
    out[kind] = kept
  }
  return out
}

export function isChecked(ctx: WorkspaceContext, kind: string, id: string): boolean {
  const v = ctx[kind]?.[id]
  return v === undefined ? checkedByDefault(kind) : v
}

// The ids of kind whose check differs from the default
export function differing(ctx: WorkspaceContext, kind: string): string[] {
  const def = checkedByDefault(kind)
  return Object.entries(ctx[kind] ?? {})
    .filter(([, on]) => on !== def)
    .map(([id]) => id)
    .sort()
}

// ctx with one item checked or not; only differences from the default are
// kept, and a kind left with none is dropped, so a workspace back at its
// defaults stores {}
export function withCheck(ctx: WorkspaceContext, kind: string, id: string, on: boolean): WorkspaceContext {
  const items = { ...(ctx[kind] ?? {}) }
  if (on === checkedByDefault(kind)) delete items[id]
  else items[id] = on
  const out: WorkspaceContext = { ...ctx }
  if (Object.keys(items).length === 0) delete out[kind]
  else out[kind] = items
  return out
}

const same = (a: WorkspaceContext, b: WorkspaceContext) => JSON.stringify(sorted(a)) === JSON.stringify(sorted(b))
function sorted(ctx: WorkspaceContext): [string, [string, boolean][]][] {
  return Object.keys(ctx)
    .sort()
    .map((k) => [k, Object.entries(ctx[k]).sort(([a], [b]) => a.localeCompare(b))])
}

export interface WriterDeps {
  // The workspace as this browser last saw it
  seen(): { version: number; context: unknown } | undefined
  // Sends workspace.update {version, context}; rejects with an error whose
  // errorCode is stale_workspace when the version was not the workspace's
  send(version: number, context: WorkspaceContext): Promise<void>
  // A failure other than a stale version
  failed?(err: unknown): void
}

interface Toggle {
  kind: string
  id: string
  on: boolean
  made?: number // the version the save that carried it made
  sending?: boolean // in a write not answered yet
}

const isStale = (err: unknown) => (err as { errorCode?: string } | undefined)?.errorCode === 'stale_workspace'

// Writes one workspace's checks at once, one toggle at a time on top of the
// newest version this browser saw (16c: on stale_workspace it waits for the
// newer version and re-applies its own toggles there, so another window's
// toggles are kept). Toggles show at once (effective) until the version
// that holds them arrives.
export class ContextWriter {
  private toggles: Toggle[] = []
  private inFlight = false
  private again = false
  private waitingPast?: number
  private ackedVersion = 0
  private readonly deps: WriterDeps

  constructor(deps: WriterDeps) {
    this.deps = deps
  }

  // The context as the person sees it: the saved one with their toggles
  // the browser has not seen saved yet
  effective(): WorkspaceContext {
    let ctx = parseContext(this.deps.seen()?.context)
    for (const t of this.toggles) ctx = withCheck(ctx, t.kind, t.id, t.on)
    return ctx
  }

  // Nothing waits to be written or seen
  idle(): boolean {
    return this.toggles.length === 0 && !this.inFlight
  }

  setChecked(kind: string, id: string, on: boolean) {
    // a toggle already written or being written stays, so what comes after it is sent on top
    this.toggles = [...this.toggles.filter((t) => !(t.kind === kind && t.id === id && t.made === undefined && !t.sending)), { kind, id, on }]
    void this.save()
  }

  // The browser saw a newer version: toggles it holds are dropped, and a
  // stale write goes again
  versionSeen() {
    const seen = this.deps.seen()
    if (!seen) return
    this.toggles = this.toggles.filter((t) => t.made === undefined || t.made > seen.version)
    if (this.waitingPast !== undefined && seen.version > this.waitingPast) {
      this.waitingPast = undefined
      void this.save()
    }
  }

  private async save() {
    if (this.inFlight || this.waitingPast !== undefined) {
      this.again = this.again || this.inFlight
      return
    }
    const seen = this.deps.seen()
    const unsent = this.toggles.filter((t) => t.made === undefined && !t.sending)
    if (!seen || unsent.length === 0) return
    // the version this browser's own last write made may not have reached it yet
    const version = Math.max(seen.version, this.ackedVersion)
    const base = this.effective()
    let before = parseContext(seen.context)
    for (const t of this.toggles.filter((t) => t.made !== undefined)) before = withCheck(before, t.kind, t.id, t.on)
    if (same(base, before)) {
      this.toggles = this.toggles.filter((t) => t.made !== undefined) // back where it was
      return
    }
    this.inFlight = true
    for (const t of unsent) t.sending = true
    try {
      await this.deps.send(version, base)
      this.ackedVersion = version + 1
      for (const t of unsent) t.made = version + 1
    } catch (err) {
      if (isStale(err)) this.waitingPast = version
      else {
        this.toggles = this.toggles.filter((t) => !unsent.includes(t)) // shown as saved again
        this.deps.failed?.(err)
      }
    } finally {
      this.inFlight = false
      for (const t of unsent) t.sending = false
    }
    if (this.waitingPast !== undefined) {
      this.versionSeen() // it may have arrived while the write was out
      return
    }
    this.versionSeen()
    if (this.again || this.toggles.some((t) => t.made === undefined)) {
      this.again = false
      void this.save()
    }
  }
}
