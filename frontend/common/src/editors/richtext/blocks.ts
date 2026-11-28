// What the find panel, the outline and the selection read from a document
// (forge 35b-1).

import type { Node } from '@tiptap/pm/model'
import type { Block } from './find.ts'

// Every textblock's text, one character per position (find.ts)
export function textBlocks(doc: Node): Block[] {
  const out: Block[] = []
  doc.descendants((node, pos) => {
    if (!node.isTextblock) return true
    let text = ''
    node.forEach((child) => {
      text += child.isText ? (child.text ?? '') : '￼'.repeat(child.nodeSize)
    })
    out.push({ text, start: pos + 1 })
    return false
  })
  return out
}

export interface Heading {
  level: number
  text: string
  pos: number
  index: number // the top-level block's index
}

// The document's top-level headings, in order
export function headings(doc: Node): Heading[] {
  const out: Heading[] = []
  doc.forEach((node, offset, index) => {
    if (node.type.name === 'heading') out.push({ level: Number(node.attrs.level) || 1, text: node.textContent, pos: offset, index })
  })
  return out
}

// The selection as the workbench and a page see it: its text, the
// top-level blocks it touches, and the heading above it
export function describeSelection(doc: Node, from: number, to: number): { text: string; blocks: number[]; heading: string } {
  const first = doc.resolve(from).index(0)
  const last = doc.resolve(to).index(0)
  const blocks: number[] = []
  for (let i = first; i <= last && i < doc.childCount; i++) blocks.push(i)
  let heading = ''
  for (const h of headings(doc)) {
    if (h.index > first) break
    heading = h.text
  }
  return { text: doc.textBetween(from, to, '\n', '￼'), blocks, heading }
}
