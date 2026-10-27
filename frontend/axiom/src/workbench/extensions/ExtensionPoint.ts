// Extension points: named places a feature lets other features contribute
// to, like Eclipse's. See documentation/plans/Contributors.md.

// What a when() predicate can look at, e.g. { surface: 'chat' }
export type ContributionContext = Record<string, unknown>

export interface Contribution {
  id: string // unique within its point
  order?: number // sort key, default 1000
  source: string // the contributing feature, e.g. 'chat', shown in DevTools
  when?: (ctx: ContributionContext) => boolean
}

export const defaultOrder = 1000

// A typed token: contribute() and useContributions() are type-checked per point
export interface ExtensionPoint<T extends Contribution> {
  readonly id: string
  readonly description: string
  // Never set; it only carries T
  readonly contributionType?: T
}
