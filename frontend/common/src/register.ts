// Extension points declare themselves in their owner's points.ts, and features
// contribute in their contributions.ts (documentation/Contributors.md). An app
// imports this before its first render.
import.meta.glob(['./**/points.ts', './**/contributions.ts'], { eager: true })
