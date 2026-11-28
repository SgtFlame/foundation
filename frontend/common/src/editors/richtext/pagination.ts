// Page view's pagination (forge 35b-2): a ProseMirror plugin that measures
// the rendered top-level blocks (and a table's rows), places them on pages
// with paginate (pages.ts), and draws the page gaps as decorations: the
// empty foot of a page, its footnotes, its footer, the band between pages,
// the next page's header and, where a table splits, its repeated header
// rows. Nothing enters the document or the room, so two people's edits
// never fight over page breaks. Footnotes are numbered in document order
// here too; outside page view they are listed at the end.

import { Extension } from '@tiptap/core'
import { Plugin, PluginKey, TextSelection, type EditorState } from '@tiptap/pm/state'
import { Decoration, DecorationSet, type EditorView } from '@tiptap/pm/view'
import { DOMSerializer, type Node as PMNode, type Schema } from '@tiptap/pm/model'
import { defaultSetup, footnoteHeight, pageBox, paginate, type Layout, type MeasuredBlock, type PageBox, type PageSetup } from './pages.ts'
import type { HeaderFooter } from './content.ts'

export interface PaginationOptions {
  enabled(): boolean // page view, rather than pageless or compact
  setup(): PageSetup
  // A header or footer's document (JSON), if it has one
  headerFooter(name: HeaderFooter): Record<string, unknown> | undefined
  bandHeight: number
  onLayout?(layout: Layout | undefined): void
}

export interface PaginationStorage {
  pageOf?: number[]
  layout?: Layout
  version: number
  relayout(): void
}

export const paginationKey = new PluginKey<DecorationSet>('pagination')

interface Measured {
  blocks: MeasuredBlock[]
  starts: number[] // each top-level block's position
  footnotes: string[] // every footnote's text, in order
  tables: Map<number, TableInfo>
}

interface TableInfo {
  rowPos: number[]
  rowSize: number[]
  cellLeft: number // the first cell's left edge, from the text column's
  width: number
  header?: HTMLElement // a copy of the header rows, with the columns' widths
}

const widgetClass = 'rt-page-widget'

function outerHeight(el: HTMLElement): number {
  const cs = getComputedStyle(el)
  return el.offsetHeight + (parseFloat(cs.marginTop) || 0) + (parseFloat(cs.marginBottom) || 0)
}

function footnoteTexts(doc: PMNode): string[] {
  const out: string[] = []
  doc.descendants((n) => {
    if (n.type.name === 'footnote') out.push(n.textContent)
    return true
  })
  return out
}

// Measures the blocks as laid out now, leaving out the widgets this plugin
// drew and the padding it gave split rows
function measure(view: EditorView, box: PageBox): Measured | undefined {
  const dom = view.dom as HTMLElement
  if (!dom.isConnected || dom.offsetParent === null) return undefined
  const domLeft = dom.getBoundingClientRect().left
  const tops = new Map<Element, number>()
  let shift = 0
  for (const k of Array.from(dom.children) as HTMLElement[]) {
    if (k.classList.contains(widgetClass)) shift += outerHeight(k)
    else tops.set(k, k.offsetTop - shift)
  }
  const doc = view.state.doc
  const blocks: MeasuredBlock[] = []
  const starts: number[] = []
  const tables = new Map<number, TableInfo>()
  const elements: (HTMLElement | null)[] = []
  doc.forEach((_node, offset) => {
    starts.push(offset)
    elements.push(view.nodeDOM(offset) as HTMLElement | null)
  })
  doc.forEach((node, offset, index) => {
    const el = elements[index]
    const top = el ? tops.get(el) : undefined
    let next: number | undefined
    for (let j = index + 1; j < elements.length && next === undefined; j++) {
      const e = elements[j]
      if (e) next = tops.get(e)
    }
    let height = el && top !== undefined ? (next !== undefined ? next - top : outerHeight(el)) : 0
    const fns: number[] = []
    node.descendants((n) => {
      if (n.type.name === 'footnote') fns.push(footnoteHeight(n.textContent, box.contentWidth))
      return true
    })
    const name = node.type.name
    const b: MeasuredBlock = { height, kind: name === 'heading' ? 'heading' : name === 'pageBreak' ? 'pageBreak' : name === 'sectionBreak' ? 'sectionBreak' : 'block' }
    if (fns.length) b.footnotes = fns
    if (name === 'table' && el) {
      const table = el.tagName === 'TABLE' ? (el as HTMLTableElement) : el.querySelector('table')
      const trs = table ? Array.from(table.rows) : []
      if (trs.length === node.childCount && trs.length > 0) {
        let pads = 0
        const rows = trs.map((tr) => {
          const pad = Number(tr.dataset.rtPad) || 0
          pads += pad
          return tr.offsetHeight - pad
        })
        height -= pads
        const extra = height - rows.reduce((a, r) => a + r, 0)
        if (extra > 0) rows[0] += extra
        let headerRows = 0
        while (headerRows < node.childCount && headerRows < rows.length - 1) {
          const row = node.child(headerRows)
          let all = row.childCount > 0
          row.forEach((c) => (all &&= c.type.name === 'tableHeader'))
          if (!all) break
          headerRows++
        }
        const rowPos: number[] = []
        const rowSize: number[] = []
        let p = offset + 1
        node.forEach((row) => {
          rowPos.push(p)
          rowSize.push(row.nodeSize)
          p += row.nodeSize
        })
        const firstCell = trs[0].cells[0]
        let header: HTMLElement | undefined
        if (headerRows > 0 && table) {
          const copy = document.createElement('table')
          copy.className = 'rt-repeat-header'
          const body = document.createElement('tbody')
          for (let r = 0; r < headerRows; r++) {
            const tr = trs[r].cloneNode(true) as HTMLTableRowElement
            Array.from(tr.cells).forEach((cell, c) => {
              cell.style.boxSizing = 'border-box'
              cell.style.width = `${trs[r].cells[c]?.offsetWidth ?? 0}px`
            })
            tr.removeAttribute('data-rt-pad')
            tr.removeAttribute('style')
            tr.classList.remove('rt-split')
            tr.querySelectorAll(`.${widgetClass}`).forEach((w) => w.remove())
            body.append(tr)
          }
          copy.append(body)
          copy.style.width = `${table.offsetWidth}px`
          header = copy
        }
        tables.set(index, {
          rowPos, rowSize, header,
          cellLeft: firstCell ? firstCell.getBoundingClientRect().left - domLeft : 0,
          width: table?.offsetWidth ?? 0,
        })
        b.kind = 'table'
        b.rows = rows
        b.headerRows = headerRows
      }
    }
    b.height = Math.max(height, 0)
    blocks.push(b)
  })
  return { blocks, starts, tables, footnotes: footnoteTexts(doc) }
}

function renderHeaderFooter(schema: Schema, json: Record<string, unknown> | undefined, page: number, pages: number): HTMLElement {
  const out = document.createElement('div')
  if (!json) return out
  try {
    const node = schema.nodeFromJSON(json)
    out.append(DOMSerializer.fromSchema(schema).serializeFragment(node.content))
    out.querySelectorAll<HTMLElement>('[data-page-number]').forEach((el) => {
      el.textContent = String(el.dataset.pageNumber === 'pages' ? pages : page)
    })
  } catch {
    // a header the schema cannot read: drawn empty
  }
  return out
}

interface GapArgs {
  schema: Schema
  opts: PaginationOptions
  box: PageBox
  setup: PageSetup
  pageBefore: number // 0 when this is the first page's top
  pageAfter: number // 0 when this is the last page's foot
  pages: number
  filler: number
  notes: number[]
  noteHeight: number
  footnotes: string[]
}

const hfName = (base: 'header' | 'footer', page: number, setup: PageSetup): HeaderFooter =>
  setup.differentFirst && page === 1 ? (base === 'header' ? 'firstHeader' : 'firstFooter') : base

// The foot of one page and the top of the next (either may be absent)
function gapDom(a: GapArgs): HTMLElement {
  const { box } = a
  const m = box.margins
  const gap = document.createElement('div')
  gap.className = `${widgetClass} rt-page-gap`
  gap.contentEditable = 'false'
  gap.style.width = `${box.width}px`
  gap.style.marginLeft = `${-m.left}px`
  const part = (cls: string, height: number) => {
    const d = document.createElement('div')
    d.className = cls
    d.style.height = `${height}px`
    d.style.paddingLeft = `${m.left}px`
    d.style.paddingRight = `${m.right}px`
    gap.append(d)
    return d
  }
  if (a.pageBefore > 0) {
    part('rt-gap-fill', a.filler)
    const notes = part('rt-page-notes', a.noteHeight)
    for (const n of a.notes) {
      const item = document.createElement('div')
      item.className = 'rt-page-note'
      item.textContent = `${n + 1}. ${a.footnotes[n] ?? ''}`
      notes.append(item)
    }
    const footer = part('rt-page-footer', m.bottom)
    footer.append(renderHeaderFooter(a.schema, a.opts.headerFooter(hfName('footer', a.pageBefore, a.setup)), a.pageBefore, a.pages))
  }
  if (a.pageBefore > 0 && a.pageAfter > 0) part('rt-page-band', a.opts.bandHeight)
  if (a.pageAfter > 0) {
    const header = part('rt-page-header', m.top)
    header.append(renderHeaderFooter(a.schema, a.opts.headerFooter(hfName('header', a.pageAfter, a.setup)), a.pageAfter, a.pages))
  }
  return gap
}

function gapHeight(a: Pick<GapArgs, 'box' | 'opts' | 'filler' | 'noteHeight'>): number {
  return a.filler + a.noteHeight + a.box.margins.bottom + a.opts.bandHeight + a.box.margins.top
}

function decorate(state: EditorState, opts: PaginationOptions, m: Measured, layout: Layout, setup: PageSetup, box: PageBox, version: number): DecorationSet {
  const schema = state.schema
  const decos: Decoration[] = []
  const base = { schema, opts, box, setup, pages: layout.pages, footnotes: m.footnotes }
  const v = `${version}:${layout.pages}`
  decos.push(Decoration.widget(0, () => {
    const top = gapDom({ ...base, pageBefore: 0, pageAfter: 1, filler: 0, notes: [], noteHeight: 0 })
    top.classList.add('rt-page-top')
    top.style.marginTop = `${-box.margins.top}px`
    return top
  }, { side: -1, key: `top:${v}`, ignoreSelection: true }))
  for (const b of layout.breaks) {
    const args = { ...base, pageBefore: b.page - 1, pageAfter: b.page, filler: b.filler, notes: b.footnotes, noteHeight: b.footnoteHeight }
    const key = `gap:${v}:${b.page}:${Math.round(b.filler)}:${Math.round(b.footnoteHeight)}:${b.footnotes.join(',')}`
    const table = b.row !== undefined ? m.tables.get(b.block) : undefined
    if (b.row === undefined || !table) {
      decos.push(Decoration.widget(m.starts[b.block], () => gapDom(args), { side: -1, key, ignoreSelection: true }))
      continue
    }
    const pad = gapHeight(args) + b.repeatHeader
    const from = table.rowPos[b.row]
    decos.push(Decoration.node(from, from + table.rowSize[b.row], { class: 'rt-split', style: `--rt-pad: ${pad}px`, 'data-rt-pad': String(Math.round(pad)) }))
    decos.push(Decoration.widget(from + 2, () => {
      const holder = document.createElement('div')
      holder.className = 'rt-page-split'
      holder.contentEditable = 'false'
      holder.style.left = `${-table.cellLeft}px`
      const gap = gapDom(args)
      gap.classList.remove(widgetClass)
      holder.append(gap)
      if (table.header) {
        const copy = table.header.cloneNode(true) as HTMLElement
        copy.style.marginLeft = `${table.cellLeft}px`
        holder.append(copy)
      }
      return holder
    }, { side: -1, key: `split:${key}:${b.repeatHeader}`, ignoreSelection: true }))
  }
  const end = state.doc.content.size
  decos.push(Decoration.widget(end, () => {
    const foot = gapDom({ ...base, pageBefore: layout.pages, pageAfter: 0, filler: layout.lastFiller, notes: layout.lastFootnotes, noteHeight: layout.lastFootnoteHeight })
    foot.classList.add('rt-page-bottom')
    foot.style.marginBottom = `${-box.margins.bottom}px`
    return foot
  }, { side: 1, key: `bottom:${v}:${Math.round(layout.lastFiller)}:${layout.lastFootnotes.join(',')}`, ignoreSelection: true }))
  return DecorationSet.create(state.doc, decos)
}

// Footnote numbers, the open footnote, and outside page view the notes at the end
function footnoteDecorations(state: EditorState, paged: boolean): DecorationSet {
  const decos: Decoration[] = []
  const texts: string[] = []
  const { from, to } = state.selection
  state.doc.descendants((n, pos) => {
    if (n.type.name !== 'footnote') return true
    texts.push(n.textContent)
    const open = from > pos && to < pos + n.nodeSize
    decos.push(Decoration.node(pos, pos + n.nodeSize, { 'data-n': String(texts.length), class: open ? 'rt-footnote-open' : '' }))
    return false
  })
  if (!paged && texts.length > 0) {
    decos.push(Decoration.widget(state.doc.content.size, () => {
      const notes = document.createElement('div')
      notes.className = 'rt-endnotes'
      notes.contentEditable = 'false'
      texts.forEach((t, i) => {
        const item = document.createElement('div')
        item.textContent = `${i + 1}. ${t}`
        notes.append(item)
      })
      return notes
    }, { side: 1, key: `notes:${JSON.stringify(texts)}`, ignoreSelection: true }))
  }
  return DecorationSet.create(state.doc, decos)
}

export const Pagination = Extension.create<PaginationOptions, PaginationStorage>({
  name: 'pagination',

  addOptions() {
    return { enabled: () => false, setup: () => defaultSetup, headerFooter: () => undefined, bandHeight: 24 }
  },

  addStorage() {
    return { pageOf: undefined, layout: undefined, version: 0, relayout: () => {} }
  },

  addProseMirrorPlugins() {
    const opts = this.options
    const storage = this.storage
    let noteCache: { doc: PMNode; from: number; to: number; paged: boolean; set: DecorationSet } | undefined
    return [
      new Plugin<DecorationSet>({
        key: paginationKey,
        state: {
          init: () => DecorationSet.empty,
          apply(tr, set) {
            const next = tr.getMeta(paginationKey) as DecorationSet | undefined
            return next ?? set.map(tr.mapping, tr.doc)
          },
        },
        props: {
          decorations(state) {
            const paged = opts.enabled()
            const { from, to } = state.selection
            if (!noteCache || noteCache.doc !== state.doc || noteCache.from !== from || noteCache.to !== to || noteCache.paged !== paged) {
              noteCache = { doc: state.doc, from, to, paged, set: footnoteDecorations(state, paged) }
            }
            const pages = paginationKey.getState(state) ?? DecorationSet.empty
            return DecorationSet.create(state.doc, [...pages.find(), ...noteCache.set.find()])
          },
          handleClickOn(view, _pos, node, nodePos) {
            if (node.type.name !== 'footnote' || !view.editable) return false
            const inside = nodePos + node.nodeSize - 1
            view.dispatch(view.state.tr.setSelection(TextSelection.create(view.state.doc, inside)))
            return true
          },
        },
        view(view) {
          let frame = 0
          let last = ''
          let width = 0
          const run = () => {
            frame = 0
            if (!opts.enabled()) {
              if (last !== '') {
                last = ''
                storage.layout = undefined
                storage.pageOf = undefined
                view.dispatch(view.state.tr.setMeta(paginationKey, DecorationSet.empty).setMeta('addToHistory', false))
                opts.onLayout?.(undefined)
              }
              return
            }
            const setup = opts.setup()
            const box = pageBox(setup)
            const m = measure(view, box)
            if (!m) return
            const layout = paginate(m.blocks, { contentHeight: box.contentHeight })
            const sig = JSON.stringify([storage.version, setup, layout])
            if (sig === last) return
            last = sig
            storage.layout = layout
            storage.pageOf = layout.pageOf
            view.dispatch(view.state.tr.setMeta(paginationKey, decorate(view.state, opts, m, layout, setup, box, storage.version)).setMeta('addToHistory', false))
            opts.onLayout?.(layout)
          }
          const schedule = () => {
            if (!frame) frame = requestAnimationFrame(run)
          }
          storage.relayout = () => {
            storage.version++
            schedule()
          }
          const resized = new ResizeObserver(() => {
            const w = (view.dom as HTMLElement).offsetWidth
            if (w !== width) {
              width = w
              schedule()
            }
          })
          resized.observe(view.dom)
          const loaded = () => schedule()
          view.dom.addEventListener('load', loaded, true)
          void document.fonts?.ready.then(schedule)
          schedule()
          return {
            update(v, prev) {
              if (v.state.doc !== prev.doc) schedule()
            },
            destroy() {
              if (frame) cancelAnimationFrame(frame)
              resized.disconnect()
              view.dom.removeEventListener('load', loaded, true)
              storage.relayout = () => {}
            },
          }
        },
      }),
    ]
  },
})
