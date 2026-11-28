// Page view's rules (forge 35b-2): page sizes and settings, and the
// paginator that places measured blocks on pages. The pagination plugin
// measures the rendered blocks and draws page gaps from this layout as
// decorations, so page breaks never enter the document or the room. Pure,
// so it is tested on its own.

// Lengths in settings are points (1/72 inch), as print dialogs count them
export type PageSize = 'letter' | 'a4' | 'legal' | 'custom'

export interface Margins {
  top: number
  right: number
  bottom: number
  left: number
}

export interface PageSetup {
  size: PageSize
  width: number // portrait width, points
  height: number // portrait height, points
  landscape: boolean
  margins: Margins
  color: string // the page colour; '' is the theme's paper
  differentFirst: boolean // the first page has its own header and footer
  pageless: boolean
  blocks: boolean // the document opens in block mode (forge 35c-2)
}

export const pageSizes: Record<Exclude<PageSize, 'custom'>, { name: string; width: number; height: number }> = {
  letter: { name: 'Letter (8.5 × 11 in)', width: 612, height: 792 },
  a4: { name: 'A4 (210 × 297 mm)', width: 595.28, height: 841.89 },
  legal: { name: 'Legal (8.5 × 14 in)', width: 612, height: 1008 },
}

export const defaultSetup: PageSetup = {
  size: 'letter',
  width: 612,
  height: 792,
  landscape: false,
  margins: { top: 72, right: 72, bottom: 72, left: 72 },
  color: '',
  differentFirst: false,
  pageless: false,
  blocks: false,
}

const num = (v: unknown, fallback: number, min = 0, max = 5000) => {
  const n = typeof v === 'number' ? v : Number(v)
  return Number.isFinite(n) ? Math.min(Math.max(n, min), max) : fallback
}

// A page setup from what a document holds (doc.attrs.page), every field
// checked: a stranger's file never breaks the view
export function setupOf(raw: unknown): PageSetup {
  const r = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>
  const size: PageSize = r.size === 'a4' || r.size === 'legal' || r.size === 'custom' ? r.size : 'letter'
  const known = size === 'custom' ? undefined : pageSizes[size]
  const width = known?.width ?? num(r.width, defaultSetup.width, 72)
  const height = known?.height ?? num(r.height, defaultSetup.height, 72)
  const m = (r.margins && typeof r.margins === 'object' ? r.margins : {}) as Record<string, unknown>
  const margins = {
    top: num(m.top, 72, 0, height / 3),
    right: num(m.right, 72, 0, width / 3),
    bottom: num(m.bottom, 72, 0, height / 3),
    left: num(m.left, 72, 0, width / 3),
  }
  const color = typeof r.color === 'string' && /^#[0-9a-f]{3,8}$/i.test(r.color) ? r.color : ''
  return { size, width, height, landscape: r.landscape === true, margins, color, differentFirst: r.differentFirst === true, pageless: r.pageless === true, blocks: r.blocks === true }
}

// Only what differs from the defaults, as the document keeps it
export function storedSetup(s: PageSetup): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  if (s.size !== defaultSetup.size) out.size = s.size
  if (s.size === 'custom') (out.width = s.width), (out.height = s.height)
  if (s.landscape) out.landscape = true
  const m = s.margins
  if (m.top !== 72 || m.right !== 72 || m.bottom !== 72 || m.left !== 72) out.margins = { ...m }
  if (s.color) out.color = s.color
  if (s.differentFirst) out.differentFirst = true
  if (s.pageless) out.pageless = true
  if (s.blocks) out.blocks = true
  return out
}

export const pxPerPt = 96 / 72

export interface PageBox {
  width: number // px, as laid out
  height: number
  margins: Margins // px
  contentWidth: number
  contentHeight: number
}

// The page as the screen draws it, in CSS pixels
export function pageBox(s: PageSetup): PageBox {
  const w = (s.landscape ? s.height : s.width) * pxPerPt
  const h = (s.landscape ? s.width : s.height) * pxPerPt
  const margins = { top: s.margins.top * pxPerPt, right: s.margins.right * pxPerPt, bottom: s.margins.bottom * pxPerPt, left: s.margins.left * pxPerPt }
  return { width: w, height: h, margins, contentWidth: w - margins.left - margins.right, contentHeight: h - margins.top - margins.bottom }
}

// Indents a paragraph may have, in points, kept within the text column
export interface Indents {
  left: number
  right: number
  first: number // the first line's, relative to left (negative hangs)
}

export function clampIndents(i: Indents, columnWidth: number): Indents {
  const left = Math.min(Math.max(Math.round(i.left), 0), columnWidth - 36)
  const right = Math.min(Math.max(Math.round(i.right), 0), columnWidth - left - 36)
  const first = Math.min(Math.max(Math.round(i.first), -left), columnWidth - left - right - 36) || 0
  return { left, right, first }
}

// ---- The paginator ----

export type BlockKind = 'block' | 'heading' | 'table' | 'pageBreak' | 'sectionBreak'

export interface MeasuredBlock {
  height: number // px, with its margins
  kind?: BlockKind
  rows?: number[] // a table's row heights; its height is their sum
  headerRows?: number // how many of the first rows are its header
  footnotes?: number[] // heights of the footnotes this block refers to
}

export interface PaginateOptions {
  contentHeight: number
  footnoteSeparator?: number // the rule above a page's footnotes
}

export interface PageBreak {
  block: number // the block that starts the new page
  row?: number // the row of that table that starts it, when it is split
  page: number // the new page's number, from 1
  filler: number // the empty space left at the foot of the previous page
  footnoteHeight: number // the previous page's footnote area, separator included
  footnotes: number[] // the previous page's footnotes, by their number from 0
  repeatHeader: number // the height of the header rows repeated on the new page
}

export interface Layout {
  pages: number
  breaks: PageBreak[]
  pageOf: number[] // the page each block starts on, from 1
  lastFootnotes: number[] // the last page's footnotes
  lastFootnoteHeight: number
  lastFiller: number
  overflow: number[] // blocks taller than a page, which run past its foot
}

const sum = (xs: readonly number[]) => xs.reduce((a, b) => a + b, 0)

// Places blocks on pages: a block that does not fit starts a new page; a
// heading keeps with what follows it; a table splits between rows and
// repeats its header rows; a block's footnotes go on its own page, so a
// block whose footnotes do not fit moves to the next page; page and
// section breaks start a new page.
export function paginate(blocks: readonly MeasuredBlock[], o: PaginateOptions): Layout {
  const H = Math.max(o.contentHeight, 1)
  const sep = o.footnoteSeparator ?? 16
  const breaks: PageBreak[] = []
  const pageOf: number[] = []
  const overflow: number[] = []
  let page = 1
  let used = 0 // the blocks' height on this page
  let notes: number[] = [] // this page's footnotes
  let noteHeight = 0 // and their area, separator included
  let footnote = 0 // the next footnote's number
  let force = false

  const newPage = (block: number, row?: number, repeatHeader = 0) => {
    breaks.push({ block, row, page: page + 1, filler: Math.max(H - used - noteHeight, 0), footnoteHeight: noteHeight, footnotes: notes, repeatHeader })
    page++
    used = repeatHeader
    notes = []
    noteHeight = 0
  }
  const notesCost = (fns: readonly number[]) => (fns.length === 0 ? 0 : sum(fns) + (notes.length === 0 ? sep : 0))
  const addNotes = (fns: readonly number[]) => {
    noteHeight += notesCost(fns)
    for (let k = 0; k < fns.length; k++) notes.push(footnote++)
  }
  const isBreak = (b: MeasuredBlock) => b.kind === 'pageBreak' || b.kind === 'sectionBreak'
  // The least of a block that must share a page with a heading before it
  const leastOf = (i: number): number => {
    const b = blocks[i]
    if (!b || isBreak(b)) return 0
    if (b.kind === 'table' && b.rows?.length) return sum(b.rows.slice(0, Math.min((b.headerRows ?? 0) + 1, b.rows.length)))
    if (b.kind === 'heading') return b.height + leastOf(i + 1)
    return b.height
  }

  for (let i = 0; i < blocks.length; i++) {
    const b = blocks[i]
    if (force) {
      force = false
      newPage(i)
    }
    pageOf.push(page)
    if (isBreak(b)) {
      used += b.height
      force = i < blocks.length - 1
      continue
    }
    const fns = b.footnotes ?? []
    if (b.kind === 'table' && b.rows && b.rows.length > 0) {
      const rows = b.rows
      const headerRows = Math.min(b.headerRows ?? 0, rows.length - 1)
      const header = sum(rows.slice(0, headerRows))
      const first = header + rows[headerRows]
      if (used > 0 && used + noteHeight + first + notesCost(fns) > H) {
        newPage(i)
        pageOf[i] = page
      }
      addNotes(fns)
      let onPage = 0 // rows of the table on this page, a repeated header not counted
      for (let r = 0; r < rows.length; r++) {
        if (onPage > 0 && r >= headerRows && used + noteHeight + rows[r] > H) {
          newPage(i, r, headerRows > 0 ? header : 0)
          onPage = 0
        }
        used += rows[r]
        if (rows[r] + (onPage === 0 ? used - rows[r] : 0) > H) overflow.push(i)
        onPage++
      }
      continue
    }
    const keep = b.kind === 'heading' ? leastOf(i + 1) : 0
    const need = b.height + notesCost(fns)
    if (used > 0 && used + noteHeight + need + (need + keep <= H ? keep : 0) > H) {
      newPage(i)
      pageOf[i] = page
    }
    used += b.height
    addNotes(fns)
    if (b.height > H) overflow.push(i)
  }
  return {
    pages: page,
    breaks,
    pageOf,
    lastFootnotes: notes,
    lastFootnoteHeight: noteHeight,
    lastFiller: Math.max(H - used - noteHeight, 0),
    overflow: [...new Set(overflow)],
  }
}

// A footnote's height, estimated from its length: the measuring needs no
// hidden element per footnote
export function footnoteHeight(text: string, contentWidth: number, lineHeight = 17, charWidth = 6.4): number {
  const perLine = Math.max(Math.floor(contentWidth / charWidth), 10)
  return Math.max(Math.ceil((text.length + 4) / perLine), 1) * lineHeight
}
