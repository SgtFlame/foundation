// The registry of extension points and their contributions. Features declare
// points in their points.ts and contribute in their contributions.ts; register.ts
// imports both before the first render. The API never assumes compile-time
// loading, so bundles loaded at runtime can call contribute() the same way.

import { useSyncExternalStore } from 'react'
import { log } from '../../logging/index.ts'
import {
  defaultOrder,
  type Contribution,
  type ContributionContext,
  type ExtensionPoint,
} from './ExtensionPoint.ts'

type Listener = () => void

export interface PointSnapshot {
  id: string
  description: string
  contributions: readonly Contribution[] // sorted by order, then id
}

const extensionsLog = log.In('extensions')

class ExtensionRegistry {
  private readonly points = new Map<string, PointSnapshot>()
  private readonly listeners = new Set<Listener>()
  private all: readonly PointSnapshot[] = []

  declare(point: ExtensionPoint<Contribution>) {
    if (this.points.has(point.id)) return
    this.points.set(point.id, { id: point.id, description: point.description, contributions: [] })
    this.changed()
  }

  // Adds c to point and returns a function that removes it again. A
  // contribution with the same id replaces the earlier one, which keeps
  // Vite's hot reload working.
  contribute<T extends Contribution>(point: ExtensionPoint<T>, c: T): () => void {
    this.declare(point as ExtensionPoint<Contribution>)
    const current = this.points.get(point.id)!
    const others = current.contributions.filter((old) => old.id !== c.id)
    if (others.length < current.contributions.length) {
      extensionsLog.With('point', point.id, 'id', c.id).warn('%s: %s replaces an earlier contribution', point.id, c.id)
    }
    this.points.set(point.id, { ...current, contributions: sorted([...others, c]) })
    this.changed()
    return () => {
      const now = this.points.get(point.id)
      if (!now?.contributions.includes(c)) return
      this.points.set(point.id, { ...now, contributions: now.contributions.filter((old) => old !== c) })
      this.changed()
    }
  }

  // point's contributions, sorted, and filtered by when() if ctx is given
  get<T extends Contribution>(point: ExtensionPoint<T>, ctx?: ContributionContext): readonly T[] {
    return visible(this.contributionsOf(point), ctx)
  }

  // The same array until point changes, so it can back useSyncExternalStore
  contributionsOf<T extends Contribution>(point: ExtensionPoint<T>): readonly T[] {
    return (this.points.get(point.id)?.contributions ?? none) as readonly T[]
  }

  // Every point in declaration order; a new array after every change
  snapshot = (): readonly PointSnapshot[] => this.all

  subscribe = (listener: Listener) => {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }

  private changed() {
    this.all = [...this.points.values()]
    for (const listener of this.listeners) listener()
  }
}

const none: readonly Contribution[] = []

function sorted<T extends Contribution>(cs: T[]): T[] {
  return cs.sort(
    (a, b) => (a.order ?? defaultOrder) - (b.order ?? defaultOrder) || a.id.localeCompare(b.id),
  )
}

function visible<T extends Contribution>(cs: readonly T[], ctx?: ContributionContext): readonly T[] {
  if (!ctx) return cs
  return cs.filter((c) => !c.when || c.when(ctx))
}

export const extensions = new ExtensionRegistry()

// Declares a point; call it once, in the owning feature's points.ts
export function defineExtensionPoint<T extends Contribution>(
  id: string,
  description: string,
): ExtensionPoint<T> {
  const point: ExtensionPoint<T> = { id, description }
  extensions.declare(point as ExtensionPoint<Contribution>)
  return point
}

// point's contributions, re-rendering when they change
export function useContributions<T extends Contribution>(
  point: ExtensionPoint<T>,
  ctx?: ContributionContext,
): readonly T[] {
  const all = useSyncExternalStore(extensions.subscribe, () => extensions.contributionsOf(point))
  return visible(all, ctx)
}
