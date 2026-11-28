// Page view's nodes (forge 35b-2): explicit page and section breaks, the
// table of contents (rebuilt from the headings on every change, with each
// heading's page in page view), the page-number field of headers and
// footers, and paragraph indents set from the ruler. text/prosemirror
// derives Markdown from them on the server: a break is a rule, and the
// table of contents and page numbers are left out.

import { Extension, Node, mergeAttributes, type Editor } from '@tiptap/core'
import { headings } from './blocks.ts'

declare module '@tiptap/core' {
  interface Commands<ReturnType> {
    pageView: {
      setPageBreak: () => ReturnType
      setSectionBreak: () => ReturnType
      insertTableOfContents: () => ReturnType
      insertPageNumber: (kind?: 'page' | 'pages') => ReturnType
      setIndents: (indents: { left?: number; right?: number; first?: number }) => ReturnType
    }
  }
}

export const PageBreak = Node.create({
  name: 'pageBreak',
  group: 'block',
  atom: true,
  selectable: true,
  parseHTML: () => [{ tag: 'div[data-page-break]' }],
  renderHTML: ({ HTMLAttributes }) => ['div', mergeAttributes(HTMLAttributes, { 'data-page-break': '', class: 'rt-page-break' })],
  addCommands() {
    return { setPageBreak: () => ({ commands }) => commands.insertContent({ type: this.name }) }
  },
  addKeyboardShortcuts() {
    return { 'Mod-Enter': () => this.editor.commands.setPageBreak() }
  },
})

// A break that starts the next section on a new page; every section has the
// document's page setup until sections get their own
export const SectionBreak = Node.create({
  name: 'sectionBreak',
  group: 'block',
  atom: true,
  selectable: true,
  parseHTML: () => [{ tag: 'div[data-section-break]' }],
  renderHTML: ({ HTMLAttributes }) => ['div', mergeAttributes(HTMLAttributes, { 'data-section-break': '', class: 'rt-section-break' })],
  addCommands() {
    return { setSectionBreak: () => ({ commands }) => commands.insertContent({ type: this.name }) }
  },
})

export const PageNumber = Node.create({
  name: 'pageNumber',
  group: 'inline',
  inline: true,
  atom: true,
  addAttributes: () => ({ kind: { default: 'page' } }),
  parseHTML: () => [{ tag: 'span[data-page-number]', getAttrs: (el) => ({ kind: (el as HTMLElement).dataset.pageNumber === 'pages' ? 'pages' : 'page' }) }],
  renderHTML: ({ node, HTMLAttributes }) => [
    'span',
    mergeAttributes(HTMLAttributes, { 'data-page-number': node.attrs.kind === 'pages' ? 'pages' : 'page', class: 'rt-page-number' }),
    '#',
  ],
  addCommands() {
    return { insertPageNumber: (kind = 'page') => ({ commands }) => commands.insertContent({ type: this.name, attrs: { kind } }) }
  },
})

// The pages each top-level block starts on, set by the pagination plugin
export interface PageNumbers {
  pageOf?: number[]
}

function renderToc(dom: HTMLElement, editor: Editor) {
  const list = headings(editor.state.doc)
  const pageOf = (editor.storage.pagination as PageNumbers | undefined)?.pageOf
  const key = JSON.stringify(list.map((h) => [h.level, h.text, h.pos, pageOf?.[h.index]]))
  if (dom.dataset.key === key) return
  dom.dataset.key = key
  dom.replaceChildren()
  const title = document.createElement('div')
  title.className = 'rt-toc-title'
  title.textContent = 'Contents'
  dom.append(title)
  if (list.length === 0) {
    const empty = document.createElement('div')
    empty.className = 'rt-toc-empty'
    empty.textContent = 'Headings appear here.'
    dom.append(empty)
  }
  for (const h of list) {
    const a = document.createElement('a')
    a.className = 'rt-toc-entry'
    a.href = `#heading-${h.index}`
    a.style.paddingLeft = `${(h.level - 1) * 1.25}em`
    a.dataset.pos = String(h.pos)
    const text = document.createElement('span')
    text.textContent = h.text || 'Untitled'
    a.append(text)
    const page = pageOf?.[h.index]
    if (page) {
      const n = document.createElement('span')
      n.className = 'rt-toc-page'
      n.textContent = String(page)
      a.append(n)
    }
    dom.append(a)
  }
}

export const TableOfContents = Node.create({
  name: 'tableOfContents',
  group: 'block',
  atom: true,
  selectable: true,
  parseHTML: () => [{ tag: 'nav[data-toc]' }],
  renderHTML: ({ HTMLAttributes }) => ['nav', mergeAttributes(HTMLAttributes, { 'data-toc': '', class: 'rt-toc' })],
  addCommands() {
    return { insertTableOfContents: () => ({ commands }) => commands.insertContent({ type: this.name }) }
  },
  addNodeView() {
    return ({ editor }) => {
      const dom = document.createElement('nav')
      dom.className = 'rt-toc'
      dom.setAttribute('data-toc', '')
      dom.contentEditable = 'false'
      const render = () => renderToc(dom, editor)
      const onClick = (e: MouseEvent) => {
        const a = (e.target as HTMLElement).closest('a.rt-toc-entry') as HTMLAnchorElement | null
        if (!a) return
        e.preventDefault()
        editor.chain().focus().setTextSelection(Number(a.dataset.pos) + 1).scrollIntoView().run()
      }
      dom.addEventListener('click', onClick)
      editor.on('transaction', render)
      render()
      return {
        dom,
        ignoreMutation: () => true,
        stopEvent: (e) => e.type === 'click' || e.type === 'mousedown',
        destroy: () => {
          editor.off('transaction', render)
          dom.removeEventListener('click', onClick)
        },
      }
    }
  },
})

const indentTypes = ['paragraph', 'heading']

const px = (v: number) => `${(v * 96) / 72}px`

const pt = (name: 'indentLeft' | 'indentRight' | 'indentFirst', css: string) => ({
  default: 0,
  parseHTML: (el: HTMLElement) => Number(el.dataset[name] ?? 0) || 0,
  renderHTML: (attrs: Record<string, unknown>) => {
    const v = Number(attrs[name]) || 0
    const data = `data-${name.replace(/[A-Z]/g, (c) => `-${c.toLowerCase()}`)}`
    return v ? { [data]: String(v), style: `${css}: ${px(v)}` } : {}
  },
})

// Left, right and first-line indents in points, set from the ruler for the
// selected paragraphs
export const Indent = Extension.create({
  name: 'indent',
  addGlobalAttributes() {
    return [
      {
        types: indentTypes,
        attributes: {
          indentLeft: pt('indentLeft', 'margin-left'),
          indentRight: pt('indentRight', 'margin-right'),
          indentFirst: pt('indentFirst', 'text-indent'),
        },
      },
    ]
  },
  addCommands() {
    return {
      setIndents: (indents) => ({ tr, state, dispatch }) => {
        const { from, to } = state.selection
        let changed = false
        state.doc.nodesBetween(from, to, (node, pos) => {
          if (!indentTypes.includes(node.type.name)) return true
          const attrs = { ...node.attrs }
          if (indents.left !== undefined) attrs.indentLeft = indents.left
          if (indents.right !== undefined) attrs.indentRight = indents.right
          if (indents.first !== undefined) attrs.indentFirst = indents.first
          tr.setNodeMarkup(pos, undefined, attrs)
          changed = true
          return false
        })
        if (changed && dispatch) dispatch(tr)
        return changed
      },
    }
  },
})

// The indents of the selection's first paragraph, in points
export function indentsAt(editor: Editor): { left: number; right: number; first: number } {
  const node = editor.state.selection.$from.parent
  return { left: Number(node.attrs.indentLeft) || 0, right: Number(node.attrs.indentRight) || 0, first: Number(node.attrs.indentFirst) || 0 }
}
