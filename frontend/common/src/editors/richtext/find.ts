// Find and replace in a rich-text document (forge 35b-1), pure so it is
// tested on its own: each textblock's text is searched with one character
// per position (an inline leaf such as an image is U+FFFC), so a match's
// offsets plus the block's start are its document positions.

export interface FindOptions {
  caseSensitive?: boolean
  wholeWord?: boolean
}

export interface Range {
  from: number
  to: number
}

// A textblock's text and the position of its first character
export interface Block {
  text: string
  start: number
}

const wordChar = /[\p{L}\p{N}_]/u

function escape(s: string) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

// The matches of query in text, left to right and never overlapping
export function findInText(text: string, query: string, options: FindOptions = {}): Range[] {
  if (!query) return []
  const re = new RegExp(escape(query), options.caseSensitive ? 'gu' : 'giu')
  const out: Range[] = []
  for (const m of text.matchAll(re)) {
    const from = m.index ?? 0
    const to = from + m[0].length
    if (options.wholeWord && (wordChar.test(text[from - 1] ?? '') || wordChar.test(text[to] ?? ''))) continue
    out.push({ from, to })
  }
  return out
}

// The matches in every block, as document positions in document order
export function findInBlocks(blocks: readonly Block[], query: string, options: FindOptions = {}): Range[] {
  return blocks.flatMap((b) => findInText(b.text, query, options).map((r) => ({ from: r.from + b.start, to: r.to + b.start })))
}

// The replacements to make, last first, so that each replacement leaves
// the positions of those still to make where they were. Each match found
// is replaced exactly once: text a replacement puts in is never searched
// again, so replacing "a" with "aa" ends.
export function replacements(matches: readonly Range[]): Range[] {
  return [...matches].sort((a, b) => b.from - a.from)
}

// The match to go to from pos: the first at or after it, wrapping round;
// backwards, the last before it
export function nextMatch(matches: readonly Range[], pos: number, backwards = false): number {
  if (matches.length === 0) return -1
  if (backwards) {
    for (let i = matches.length - 1; i >= 0; i--) if (matches[i].from < pos) return i
    return matches.length - 1
  }
  const i = matches.findIndex((m) => m.from >= pos)
  return i < 0 ? 0 : i
}
