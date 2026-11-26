// Saves one workspace's layout as the person changes it (forge 16b):
// debounced, through workspace.update {version}, and sent again on a newer
// version when it was stale (the window's layout wins: it is the one the
// person is looking at). Kept free of React and the services so `node
// --test` runs its tests.
import type { SavedLayout } from './layout.ts'

export interface SaverDeps {
  // The layout to save now, or undefined when this window must not save
  layout(): SavedLayout | undefined
  // The workspace as this browser last saw it: its version and saved layout
  seen(): { version: number; layout: unknown } | undefined
  // Sends workspace.update; rejects with an error whose errorCode is
  // stale_workspace when the version was not the workspace's
  send(version: number, layout: SavedLayout): Promise<void>
  // A failure other than a stale version
  failed?(err: unknown): void
}

const isStale = (err: unknown) => (err as { errorCode?: string } | undefined)?.errorCode === 'stale_workspace'

export class LayoutSaver {
  private timer?: ReturnType<typeof setTimeout>
  private inFlight = false
  private again = false
  // the version a stale send waits for the browser to see past
  private waitingPast?: number
  // the last layout sent and acknowledged, and the version that made
  private sent?: string
  private ackedVersion = 0
  // a retired saver's last layout: its workspace is no longer shown
  private frozen?: SavedLayout
  private retired = false

  private readonly deps: SaverDeps
  private readonly delay: number

  constructor(deps: SaverDeps, delay = 500) {
    this.deps = deps
    this.delay = delay
  }

  // The version this saver's own last save made; a workspace at or below
  // it holds nothing newer from another window
  get acked(): number {
    return this.ackedVersion
  }

  // Nothing waits to be saved: no change pending, in flight or stale
  idle(): boolean {
    return !this.timer && !this.inFlight && this.waitingPast === undefined
  }

  // A change: saved once no other change comes for the delay
  schedule() {
    if (this.retired) return
    if (this.timer) clearTimeout(this.timer)
    this.timer = setTimeout(() => {
      this.timer = undefined
      void this.save()
    }, this.delay)
  }

  // Saves a pending change at once
  flush() {
    if (!this.timer) return
    clearTimeout(this.timer)
    this.timer = undefined
    void this.save()
  }

  // The workspace is no longer shown (a switch): its layout as it is now is
  // saved at once if it changed, and this saver saves nothing else but a
  // resend of that
  retire() {
    if (this.retired) return
    const pending = !!this.timer || this.waitingPast !== undefined || this.inFlight
    if (this.timer) clearTimeout(this.timer)
    this.timer = undefined
    this.frozen = this.deps.layout()
    this.retired = true
    if (pending && this.frozen && !this.inFlight && this.waitingPast === undefined) void this.save()
    else if (this.inFlight) this.again = true
  }

  // The browser saw a newer version: a stale save goes again
  versionSeen() {
    const seen = this.deps.seen()
    if (this.waitingPast !== undefined && seen && seen.version > this.waitingPast) {
      this.waitingPast = undefined
      void this.save()
    }
  }

  // Forgets what was sent, as after the layout was replaced from the server
  reset() {
    if (this.timer) clearTimeout(this.timer)
    this.timer = undefined
    this.waitingPast = undefined
    this.again = false
    this.sent = undefined
  }

  private async save() {
    if (this.inFlight) {
      this.again = true
      return
    }
    const layout = this.retired ? this.frozen : this.deps.layout()
    const seen = this.deps.seen()
    if (!layout || !seen) return
    const json = JSON.stringify(layout)
    if (json === JSON.stringify(seen.layout ?? null) || (json === this.sent && seen.version <= this.ackedVersion)) return
    // the version this window's own last save made may not have reached it yet
    const version = Math.max(seen.version, this.ackedVersion)
    this.inFlight = true
    try {
      await this.deps.send(version, layout)
      this.sent = json
      this.ackedVersion = version + 1
    } catch (err) {
      if (isStale(err)) this.waitingPast = version
      else this.deps.failed?.(err)
    } finally {
      this.inFlight = false
    }
    if (this.waitingPast !== undefined) {
      this.versionSeen() // it may have arrived while the send was out
      return
    }
    if (this.again) {
      this.again = false
      void this.save()
    }
  }
}
