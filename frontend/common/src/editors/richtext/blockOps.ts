// Block mode's pure operations (forge 35c-2): moving a range of top-level
// blocks as one transaction (so one Yjs transaction and one undo step), the
// slash menu's query and filtering. Kept free of the editor so node --test
// can run them.

import type { EditorState, Transaction } from 'prosemirror-state'

// A slash menu item: an app gives its own through doc.blocks' items prop.
// One with content inserts it; one with command raises the component's
// command event {id, block} for the page's handler graph to answer.
export interface SlashItem {
  id: string
  title: string
  description?: string
  icon?: string
  keywords?: string[]
  content?: Record<string, unknown> | Record<string, unknown>[]
  command?: boolean
}

// The top-level index range [from, to) that a selection's blocks cover
export function selectedBlocks(state: EditorState): { from: number; to: number } {
  const { doc, selection } = state
  const from = doc.resolve(selection.from).index(0)
  const to = Math.max(from + 1, doc.resolve(selection.to).index(0) + 1)
  return { from, to: Math.min(to, doc.childCount) }
}

// The position before top-level block i (the document's end past the last)
export function posOfBlock(state: EditorState, i: number): number {
  let pos = 0
  for (let j = 0; j < i && j < state.doc.childCount; j++) pos += state.doc.child(j).nodeSize
  return pos
}

// Moves top-level blocks [from, to) to go before block target (an index in
// the document before the move; childCount for the end), as one
// transaction; null when the move changes nothing.
export function moveBlocks(state: EditorState, from: number, to: number, target: number): Transaction | null {
  const n = state.doc.childCount
  if (from < 0 || to > n || from >= to || target < 0 || target > n) return null
  if (target >= from && target <= to) return null
  const start = posOfBlock(state, from)
  const end = posOfBlock(state, to)
  const slice = state.doc.slice(start, end)
  const at = posOfBlock(state, target)
  const tr = state.tr
  if (target > to) {
    tr.insert(at, slice.content)
    tr.delete(start, end)
  } else {
    tr.delete(start, end)
    tr.insert(at, slice.content)
  }
  return tr.scrollIntoView()
}

// The slash menu's query: the text typed after a / that starts a word in
// the cursor's textblock, with where the / is; null when there is none.
export function slashQuery(state: EditorState): { query: string; from: number; to: number } | null {
  const { selection } = state
  if (!selection.empty) return null
  const $pos = selection.$from
  if (!$pos.parent.isTextblock || $pos.parent.type.spec.code) return null
  const before = $pos.parent.textBetween(0, $pos.parentOffset, undefined, '￼')
  const m = /(?:^|\s)\/([\p{L}\p{N} ]{0,24})$/u.exec(before)
  if (!m) return null
  const query = m[1]
  if (query.startsWith(' ') || query.endsWith('  ')) return null
  return { query, from: $pos.pos - query.length - 1, to: $pos.pos }
}

// The items matching a query, best first: titles starting with it, then
// titles or keywords containing it
export function filterItems(items: SlashItem[], query: string): SlashItem[] {
  const q = query.trim().toLowerCase()
  if (!q) return items
  const starts: SlashItem[] = []
  const has: SlashItem[] = []
  for (const it of items) {
    const title = it.title.toLowerCase()
    if (title.startsWith(q)) starts.push(it)
    else if (title.includes(q) || (it.keywords ?? []).some((k) => k.toLowerCase().includes(q))) has.push(it)
  }
  return [...starts, ...has]
}

const para = (text = '') => ({ type: 'paragraph', content: text ? [{ type: 'text', text }] : undefined })

// The items every block editor has; an app's items come after them, and an
// app item with a built-in id replaces it
export const defaultSlashItems: SlashItem[] = [
  { id: 'text', title: 'Text', icon: 'notes', description: 'Plain text', content: para() },
  { id: 'h1', title: 'Heading 1', icon: 'title', keywords: ['title'], content: { type: 'heading', attrs: { level: 1 } } },
  { id: 'h2', title: 'Heading 2', icon: 'title', content: { type: 'heading', attrs: { level: 2 } } },
  { id: 'h3', title: 'Heading 3', icon: 'title', content: { type: 'heading', attrs: { level: 3 } } },
  { id: 'bullets', title: 'Bulleted list', icon: 'format_list_bulleted', content: { type: 'bulletList', content: [{ type: 'listItem', content: [para()] }] } },
  { id: 'numbers', title: 'Numbered list', icon: 'format_list_numbered', content: { type: 'orderedList', content: [{ type: 'listItem', content: [para()] }] } },
  { id: 'todo', title: 'To-do list', icon: 'checklist', keywords: ['task', 'check'], content: { type: 'taskList', content: [{ type: 'taskItem', attrs: { checked: false }, content: [para()] }] } },
  { id: 'toggle', title: 'Toggle', icon: 'arrow_right', keywords: ['collapse', 'details'], content: { type: 'toggle', attrs: { open: true }, content: [{ type: 'toggleTitle' }, para()] } },
  { id: 'toggleHeading', title: 'Toggle heading', icon: 'arrow_right', content: { type: 'toggle', attrs: { open: true, level: 2 }, content: [{ type: 'toggleTitle' }, para()] } },
  { id: 'callout', title: 'Callout', icon: 'lightbulb', keywords: ['note', 'tip', 'warning'], content: { type: 'callout', attrs: { icon: '💡', color: 'info' }, content: [para()] } },
  { id: 'columns', title: 'Two columns', icon: 'view_column', content: { type: 'columns', content: [{ type: 'column', content: [para()] }, { type: 'column', content: [para()] }] } },
  { id: 'columns3', title: 'Three columns', icon: 'view_column', content: { type: 'columns', content: [0, 1, 2].map(() => ({ type: 'column', content: [para()] })) } },
  { id: 'quote', title: 'Quote', icon: 'format_quote', content: { type: 'blockquote', content: [para()] } },
  { id: 'divider', title: 'Divider', icon: 'horizontal_rule', keywords: ['rule', 'line'], content: { type: 'horizontalRule' } },
  { id: 'code', title: 'Code', icon: 'code', content: { type: 'codeBlock' } },
  { id: 'table', title: 'Table', icon: 'table_chart', content: { type: 'table', content: [0, 1, 2].map((r) => ({ type: 'tableRow', content: [0, 1, 2].map(() => ({ type: r === 0 ? 'tableHeader' : 'tableCell', content: [para()] })) })) } },
]

// The items a block editor offers: the defaults with an app's after them,
// an app's item replacing a default with its id
export function slashItems(app?: SlashItem[]): SlashItem[] {
  const own = (app ?? []).filter((i) => i && typeof i.id === 'string' && typeof i.title === 'string')
  const ids = new Set(own.map((i) => i.id))
  return [...defaultSlashItems.filter((i) => !ids.has(i.id)), ...own]
}
