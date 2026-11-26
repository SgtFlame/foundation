// Asks a file's Markdown editor to show some of its lines: a citation opens
// the file, then asks for the cited lines. The request waits until the
// editor's Live Preview has the document, even when the tab is new.

export interface Reveal {
  from: number // 1-based, inclusive
  to: number
}

const pending = new Map<string, Reveal>() // by fileId
const listeners = new Set<() => void>()

export function revealLines(fileId: string, from: number, to = from) {
  pending.set(fileId, { from, to: Math.max(from, to) })
  for (const listener of listeners) listener()
}

// The waiting request for the file, which is then forgotten
export function takeReveal(fileId: string): Reveal | undefined {
  const reveal = pending.get(fileId)
  pending.delete(fileId)
  return reveal
}

export function hasReveal(fileId: string): boolean {
  return pending.has(fileId)
}

export function subscribeReveals(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}
