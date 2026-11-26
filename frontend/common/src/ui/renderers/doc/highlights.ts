// Selections and highlights in doc.markdown (forge 18c): the pure part.
// A selection is told as its quote, the markdown headings it falls under
// and the words around it, plus Yjs relative positions while a room is
// open; a highlight is placed by its positions when they still resolve,
// else by its range when the text there is still its quote, else by
// finding its quote again (under its headings, then anywhere, then with
// spaces and case ignored). Offsets are the editor's (UTF-16 units).

import * as Y from 'yjs'

export interface Anchor {
  quote: string
  headingPath?: string[]
  context?: string
}

// A highlight as the doc.markdown prop gives it
export interface Highlight {
  id: string
  range?: { from: number; to: number }
  anchor?: Anchor & { ypos?: { from: string; to: string } }
  kind?: string
  colour?: string
}

export interface Placed {
  id: string
  from: number
  to: number
  kind: string
  colour: string
}

export interface Selected {
  quote: string
  headingPath: string[]
  context: string
  range: { from: number; to: number }
  ypos?: { from: string; to: string }
}

interface Heading {
  at: number
  level: number
  title: string
}

const fence = /^\s{0,3}(`{3,}|~{3,})/
const heading = /^\s{0,3}(#{1,6})\s+(.*?)\s*#*\s*$/

// The headings of a markdown text in order, skipping fenced code
export function headings(text: string): Heading[] {
  const out: Heading[] = []
  let open = ''
  let at = 0
  for (const line of text.split('\n')) {
    const f = fence.exec(line)
    if (f) {
      if (!open) open = f[1][0]
      else if (f[1][0] === open) open = ''
    } else if (!open) {
      const h = heading.exec(line)
      if (h) out.push({ at, level: h[1].length, title: h[2] })
    }
    at += line.length + 1
  }
  return out
}

// The headings a place falls under, outermost first
export function headingPathAt(text: string, offset: number, hs = headings(text)): string[] {
  const path: Heading[] = []
  for (const h of hs) {
    if (h.at > offset) break
    while (path.length && path[path.length - 1].level >= h.level) path.pop()
    path.push(h)
  }
  return path.map((h) => h.title)
}

// The part of the text under a heading path: from its last heading to the
// next heading of the same or a higher level
function sectionOf(text: string, path: string[], hs: Heading[]): { from: number; to: number } | undefined {
  if (!path.length) return undefined
  const same = (a: string[], b: string[]) => a.length === b.length && a.every((x, i) => x.trim().toLowerCase() === b[i].trim().toLowerCase())
  for (let i = 0; i < hs.length; i++) {
    if (!same(headingPathAt(text, hs[i].at, hs), path)) continue
    let to = text.length
    for (let j = i + 1; j < hs.length; j++) {
      if (hs[j].level <= hs[i].level) {
        to = hs[j].at
        break
      }
    }
    return { from: hs[i].at, to }
  }
  return undefined
}

// The words around a range, for picking it again among several
export function contextAround(text: string, from: number, to: number, chars = 60): string {
  return (text.slice(Math.max(0, from - chars), from) + ' … ' + text.slice(to, to + chars)).replace(/\s+/g, ' ').trim()
}

function occurrences(text: string, quote: string): number[] {
  const out: number[] = []
  if (!quote) return out
  for (let i = text.indexOf(quote); i >= 0; i = text.indexOf(quote, i + 1)) out.push(i)
  return out
}

// How many of the context's words are near an occurrence
function nearness(text: string, at: number, len: number, context: string): number {
  if (!context) return 0
  const near = text.slice(Math.max(0, at - 80), at + len + 80).toLowerCase()
  return context.toLowerCase().split(/\W+/).filter((w) => w.length > 2 && near.includes(w)).length
}

// Text with runs of spaces made one and lowered, with a map from its
// offsets back to the text's
function normalized(text: string): { s: string; map: number[] } {
  let s = ''
  const map: number[] = []
  let space = false
  for (let i = 0; i < text.length; i++) {
    const c = text[i]
    if (/\s/.test(c)) {
      if (space) continue
      space = true
      s += ' '
    } else {
      space = false
      s += c.toLowerCase()
    }
    map.push(i)
  }
  map.push(text.length)
  return { s, map }
}

// Finds a quote again in a text that may have changed: exactly under its
// headings, then exactly anywhere, then with spaces and case ignored; the
// context picks one of several occurrences.
export function findAnchor(text: string, a: Anchor): { from: number; to: number } | undefined {
  const quote = a.quote.trim()
  if (!quote) return undefined
  const hs = headings(text)
  const within = sectionOf(text, a.headingPath ?? [], hs)
  const pick = (occ: number[], len: number) => {
    let best = occ[0]
    let score = -1
    for (const o of occ) {
      const n = nearness(text, o, len, a.context ?? '')
      if (n > score) {
        best = o
        score = n
      }
    }
    return { from: best, to: best + len }
  }
  const occ = occurrences(text, quote)
  const inside = within ? occ.filter((o) => o >= within.from && o + quote.length <= within.to) : []
  if (inside.length) return pick(inside, quote.length)
  if (occ.length) return pick(occ, quote.length)
  const n = normalized(text)
  const q = normalized(quote).s.trim()
  const loose = occurrences(n.s, q)
  if (!loose.length) return undefined
  const insideLoose = within ? loose.filter((o) => n.map[o] >= within.from && n.map[o + q.length] <= within.to) : []
  const o = (insideLoose.length ? insideLoose : loose)[0]
  return { from: n.map[o], to: n.map[o + q.length - 1] + 1 }
}

const toBase64 = (b: Uint8Array) => {
  let s = ''
  for (const c of b) s += String.fromCharCode(c)
  return btoa(s)
}
const fromBase64 = (s: string) => Uint8Array.from(atob(s), (c) => c.charCodeAt(0))

// Yjs relative positions of a range, which move as people type
export function relativeRange(ytext: Y.Text, from: number, to: number): { from: string; to: string } {
  return {
    from: toBase64(Y.encodeRelativePosition(Y.createRelativePositionFromTypeIndex(ytext, from))),
    // the end sticks to the character before it, so typing after the range stays out
    to: toBase64(Y.encodeRelativePosition(Y.createRelativePositionFromTypeIndex(ytext, to, -1))),
  }
}

// A range from its relative positions, when they still resolve in doc
export function absoluteRange(doc: Y.Doc, ypos: { from: string; to: string }): { from: number; to: number } | undefined {
  try {
    const a = Y.createAbsolutePositionFromRelativePosition(Y.decodeRelativePosition(fromBase64(ypos.from)), doc)
    const b = Y.createAbsolutePositionFromRelativePosition(Y.decodeRelativePosition(fromBase64(ypos.to)), doc)
    if (!a || !b || b.index <= a.index) return undefined
    return { from: a.index, to: b.index }
  } catch {
    return undefined
  }
}

const kindColour: Record<string, string> = { anchored: '#f5c542', suggestion: '#4fa3f7', conflict: '#f26b6b' }

// Where each highlight is in text now; one that cannot be placed is left out
export function placeHighlights(text: string, list: Highlight[], doc?: Y.Doc): Placed[] {
  const out: Placed[] = []
  for (const h of list ?? []) {
    if (!h?.id) continue
    let at: { from: number; to: number } | undefined
    if (doc && h.anchor?.ypos?.from && h.anchor.ypos.to) at = absoluteRange(doc, h.anchor.ypos)
    if (!at && h.range && h.range.from >= 0 && h.range.to > h.range.from && h.range.to <= text.length) {
      const quote = h.anchor?.quote?.trim()
      if (!quote || text.slice(h.range.from, h.range.to).trim() === quote) at = h.range
    }
    if (!at && h.anchor?.quote) at = findAnchor(text, h.anchor)
    if (!at) continue
    const kind = h.kind || 'anchored'
    out.push({ id: h.id, from: at.from, to: at.to, kind, colour: h.colour || kindColour[kind] || kindColour.anchored })
  }
  return out.sort((a, b) => a.from - b.from || a.to - b.to)
}

// What a selection is told as
export function describeSelection(text: string, from: number, to: number, ytext?: Y.Text): Selected {
  const out: Selected = { quote: text.slice(from, to), headingPath: headingPathAt(text, from), context: contextAround(text, from, to), range: { from, to } }
  if (ytext && to > from) out.ypos = relativeRange(ytext, from, to)
  return out
}
