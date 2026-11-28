// Block mode's nodes (forge 35c-2), in the one document schema both modes
// edit: toggles (collapsible, or in their heading form a collapsible
// section), callouts with an icon and a colour, columns, and the atoms that
// link out: a page link (to a Docs page), a file and an embed, whose clicks
// go through the view's onLink. Page mode shows them as plain content: a
// toggle always open, a callout as a box. text/prosemirror derives their
// Markdown on the server.

import { Node, mergeAttributes } from '@tiptap/core'

declare module '@tiptap/core' {
  interface Commands<ReturnType> {
    blockMode: {
      toggleOpen: (pos: number) => ReturnType
    }
  }
}

// The href an atom block opens, or undefined for any other node
export function hrefOfBlock(type: string, attrs: Record<string, unknown>): string | undefined {
  if (type === 'pageLink') return attrs.page ? `workbench:docsPage/${String(attrs.page)}` : undefined
  if (type === 'fileBlock') return attrs.url ? String(attrs.url) : undefined
  if (type === 'embed') return attrs.href ? String(attrs.href) : undefined
  return undefined
}

export const ToggleTitle = Node.create({
  name: 'toggleTitle',
  content: 'inline*',
  defining: true,
  parseHTML: () => [{ tag: 'div[data-toggle-title]' }],
  renderHTML: ({ HTMLAttributes }) => ['div', mergeAttributes(HTMLAttributes, { 'data-toggle-title': '', class: 'rt-toggle-title' }), 0],
})

export const Toggle = Node.create({
  name: 'toggle',
  group: 'block',
  content: 'toggleTitle block*',
  defining: true,
  addAttributes: () => ({
    open: { default: true, parseHTML: (el) => (el as HTMLElement).dataset.open !== 'false', renderHTML: (a) => ({ 'data-open': a.open ? 'true' : 'false' }) },
    level: { default: 0, parseHTML: (el) => Number((el as HTMLElement).dataset.level) || 0, renderHTML: (a) => (a.level ? { 'data-level': String(a.level) } : {}) },
  }),
  parseHTML: () => [{ tag: 'div[data-toggle]' }],
  renderHTML: ({ HTMLAttributes }) => ['div', mergeAttributes(HTMLAttributes, { 'data-toggle': '', class: 'rt-toggle' }), 0],
  addCommands() {
    return {
      toggleOpen: (pos) => ({ tr, state, dispatch }) => {
        const node = state.doc.nodeAt(pos)
        if (node?.type.name !== this.name) return false
        if (dispatch) tr.setNodeMarkup(pos, undefined, { ...node.attrs, open: !node.attrs.open })
        return true
      },
    }
  },
  // A chevron before the title opens and closes it; the body is hidden by
  // CSS in block mode only
  addNodeView() {
    return ({ node, getPos, editor }) => {
      const dom = document.createElement('div')
      dom.className = 'rt-toggle'
      dom.dataset.toggle = ''
      const chevron = document.createElement('button')
      chevron.type = 'button'
      chevron.className = 'rt-toggle-chevron'
      chevron.contentEditable = 'false'
      chevron.setAttribute('aria-label', 'Open or close')
      chevron.addEventListener('mousedown', (e) => e.preventDefault())
      chevron.addEventListener('click', () => {
        const pos = typeof getPos === 'function' ? getPos() : undefined
        if (pos !== undefined) editor.commands.toggleOpen(pos)
      })
      const content = document.createElement('div')
      content.className = 'rt-toggle-content'
      dom.append(chevron, content)
      const set = (n: typeof node) => {
        dom.dataset.open = n.attrs.open ? 'true' : 'false'
        if (n.attrs.level) dom.dataset.level = String(n.attrs.level)
        else delete dom.dataset.level
        chevron.setAttribute('aria-expanded', n.attrs.open ? 'true' : 'false')
      }
      set(node)
      return {
        dom,
        contentDOM: content,
        update: (n) => {
          if (n.type.name !== 'toggle') return false
          set(n)
          return true
        },
        ignoreMutation: (m) => m.target === chevron || chevron.contains(m.target as globalThis.Node),
      }
    }
  },
})

export const calloutColors = ['info', 'success', 'warning', 'error', 'neutral'] as const

export const Callout = Node.create({
  name: 'callout',
  group: 'block',
  content: 'block+',
  defining: true,
  addAttributes: () => ({
    icon: { default: '💡', parseHTML: (el) => (el as HTMLElement).dataset.icon ?? '💡', renderHTML: (a) => ({ 'data-icon': String(a.icon ?? '') }) },
    color: {
      default: 'info',
      parseHTML: (el) => ((calloutColors as readonly string[]).includes((el as HTMLElement).dataset.color ?? '') ? (el as HTMLElement).dataset.color : 'info'),
      renderHTML: (a) => ({ 'data-color': String(a.color ?? 'info') }),
    },
  }),
  parseHTML: () => [{ tag: 'div[data-callout]' }],
  renderHTML: ({ HTMLAttributes }) => ['div', mergeAttributes(HTMLAttributes, { 'data-callout': '', class: 'rt-callout' }), 0],
  addNodeView() {
    return ({ node, getPos, editor }) => {
      const dom = document.createElement('div')
      dom.className = 'rt-callout'
      dom.dataset.callout = ''
      const icon = document.createElement('button')
      icon.type = 'button'
      icon.className = 'rt-callout-icon'
      icon.contentEditable = 'false'
      icon.setAttribute('aria-label', 'Change the callout colour')
      icon.addEventListener('mousedown', (e) => e.preventDefault())
      // A click moves to the next colour
      icon.addEventListener('click', () => {
        const pos = typeof getPos === 'function' ? getPos() : undefined
        if (pos === undefined || !editor.isEditable) return
        const current = editor.state.doc.nodeAt(pos)
        if (!current) return
        const i = calloutColors.indexOf(current.attrs.color as (typeof calloutColors)[number])
        editor.view.dispatch(editor.state.tr.setNodeMarkup(pos, undefined, { ...current.attrs, color: calloutColors[(i + 1) % calloutColors.length] }))
      })
      const content = document.createElement('div')
      content.className = 'rt-callout-content'
      dom.append(icon, content)
      const set = (n: typeof node) => {
        dom.dataset.color = String(n.attrs.color ?? 'info')
        icon.textContent = String(n.attrs.icon ?? '')
      }
      set(node)
      return {
        dom,
        contentDOM: content,
        update: (n) => {
          if (n.type.name !== 'callout') return false
          set(n)
          return true
        },
        ignoreMutation: (m) => m.target === icon || icon.contains(m.target as globalThis.Node),
      }
    }
  },
})

export const Column = Node.create({
  name: 'column',
  content: 'block+',
  isolating: true,
  parseHTML: () => [{ tag: 'div[data-column]' }],
  renderHTML: ({ HTMLAttributes }) => ['div', mergeAttributes(HTMLAttributes, { 'data-column': '', class: 'rt-column' }), 0],
})

export const Columns = Node.create({
  name: 'columns',
  group: 'block',
  content: 'column{2,4}',
  defining: true,
  parseHTML: () => [{ tag: 'div[data-columns]' }],
  renderHTML: ({ HTMLAttributes }) => ['div', mergeAttributes(HTMLAttributes, { 'data-columns': '', class: 'rt-columns' }), 0],
})

// An atom block drawn as a card: its icon, its text and what it opens
function card(name: string, attrs: Record<string, { default: unknown }>, icon: string, text: (a: Record<string, unknown>) => string) {
  return Node.create({
    name,
    group: 'block',
    atom: true,
    selectable: true,
    draggable: false,
    addAttributes: () => attrs,
    parseHTML: () => [{ tag: `div[data-${name}]`, getAttrs: (el) => JSON.parse((el as HTMLElement).dataset.attrs ?? '{}') as Record<string, unknown> }],
    renderHTML: ({ node }) => [
      'div',
      { [`data-${name}`]: '', 'data-attrs': JSON.stringify(node.attrs), class: `rt-card rt-${name}`, 'data-href': hrefOfBlock(name, node.attrs) ?? '' },
      ['span', { class: 'rt-card-icon' }, icon],
      ['span', { class: 'rt-card-text' }, text(node.attrs)],
    ],
  })
}

export const PageLink = card('pageLink', { page: { default: '' }, title: { default: '' } }, '📄', (a) => String(a.title || 'Untitled'))
export const FileBlock = card('fileBlock', { url: { default: '' }, name: { default: '' }, size: { default: 0 } }, '📎', (a) => String(a.name || 'File'))
export const Embed = card('embed', { href: { default: '' }, title: { default: '' } }, '🔗', (a) => String(a.title || a.href || 'Embed'))

export const blockNodes = [ToggleTitle, Toggle, Callout, Column, Columns, PageLink, FileBlock, Embed]
