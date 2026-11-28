// Nodes a document file may hold beyond TipTap's MIT extensions (forge
// 35b-1), as text/prosemirror reads them on the server: footnotes,
// equations (shown as their LaTeX until KaTeX rendering is built) and
// mentions. Without them a document holding one could not be loaded.

import { Node, mergeAttributes } from '@tiptap/core'

export const Footnote = Node.create({
  name: 'footnote',
  group: 'inline',
  inline: true,
  content: 'text*',
  marks: '',
  parseHTML: () => [{ tag: 'span[data-footnote]' }],
  renderHTML: ({ HTMLAttributes }) => ['span', mergeAttributes(HTMLAttributes, { 'data-footnote': '', class: 'rt-footnote' }), 0],
})

export const MathInline = Node.create({
  name: 'mathInline',
  group: 'inline',
  inline: true,
  atom: true,
  addAttributes: () => ({ latex: { default: '' } }),
  parseHTML: () => [{ tag: 'span[data-math-inline]', getAttrs: (el) => ({ latex: (el as HTMLElement).textContent ?? '' }) }],
  renderHTML: ({ node, HTMLAttributes }) => ['span', mergeAttributes(HTMLAttributes, { 'data-math-inline': '', class: 'rt-math' }), String(node.attrs.latex ?? '')],
})

export const MathBlock = Node.create({
  name: 'mathBlock',
  group: 'block',
  atom: true,
  addAttributes: () => ({ latex: { default: '' } }),
  parseHTML: () => [{ tag: 'div[data-math-block]', getAttrs: (el) => ({ latex: (el as HTMLElement).textContent ?? '' }) }],
  renderHTML: ({ node, HTMLAttributes }) => ['div', mergeAttributes(HTMLAttributes, { 'data-math-block': '', class: 'rt-math' }), String(node.attrs.latex ?? '')],
})

export const Mention = Node.create({
  name: 'mention',
  group: 'inline',
  inline: true,
  atom: true,
  addAttributes: () => ({ id: { default: null }, label: { default: null } }),
  parseHTML: () => [{ tag: 'span[data-mention]' }],
  renderHTML: ({ node, HTMLAttributes }) => ['span', mergeAttributes(HTMLAttributes, { 'data-mention': '', class: 'rt-mention' }), `@${String(node.attrs.label ?? node.attrs.id ?? '')}`],
})
