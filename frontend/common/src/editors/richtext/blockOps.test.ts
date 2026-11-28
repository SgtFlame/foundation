import { test } from 'node:test'
import assert from 'node:assert/strict'
import { schema } from 'prosemirror-schema-basic'
import { EditorState, TextSelection } from 'prosemirror-state'
import { history, undo, undoDepth } from 'prosemirror-history'
import type { Node } from 'prosemirror-model'
import { defaultSlashItems, filterItems, moveBlocks, posOfBlock, selectedBlocks, slashItems, slashQuery } from './blockOps.ts'

const p = (text: string) => schema.node('paragraph', null, text ? [schema.text(text)] : [])
const docOf = (...texts: string[]) => schema.node('doc', null, texts.map(p))
const texts = (doc: Node) => {
  const out: string[] = []
  doc.forEach((n) => out.push(n.textContent))
  return out
}
const stateOf = (doc: Node, plugins = [history()]) => EditorState.create({ doc, plugins })

test('a block moves down, before the target as the document was', () => {
  const s = stateOf(docOf('a', 'b', 'c', 'd'))
  const tr = moveBlocks(s, 0, 1, 3)
  assert.ok(tr)
  assert.deepEqual(texts(tr.doc), ['b', 'c', 'a', 'd'])
})

test('a range of blocks moves up, and to the end', () => {
  const s = stateOf(docOf('a', 'b', 'c', 'd'))
  assert.deepEqual(texts(moveBlocks(s, 2, 4, 0)!.doc), ['c', 'd', 'a', 'b'])
  assert.deepEqual(texts(moveBlocks(s, 0, 2, 4)!.doc), ['c', 'd', 'a', 'b'])
  assert.deepEqual(texts(moveBlocks(s, 1, 3, 4)!.doc), ['a', 'd', 'b', 'c'])
})

test('a move onto itself or out of range changes nothing', () => {
  const s = stateOf(docOf('a', 'b', 'c'))
  assert.equal(moveBlocks(s, 1, 2, 1), null)
  assert.equal(moveBlocks(s, 1, 2, 2), null)
  assert.equal(moveBlocks(s, 0, 4, 0), null)
  assert.equal(moveBlocks(s, 2, 1, 0), null)
})

test('dragging a block range is one undo step', () => {
  const doc = docOf('a', 'b', 'c', 'd', 'e')
  let s = stateOf(doc)
  s = s.apply(moveBlocks(s, 1, 3, 5)!)
  assert.deepEqual(texts(s.doc), ['a', 'd', 'e', 'b', 'c'])
  assert.equal(undoDepth(s), 1)
  undo(s, (tr) => (s = s.apply(tr)))
  assert.ok(s.doc.eq(doc))
})

test('the selected blocks are the range its ends touch', () => {
  const s = stateOf(docOf('aa', 'bb', 'cc'))
  const sel = (from: number, to: number) => selectedBlocks(s.apply(s.tr.setSelection(TextSelection.create(s.doc, from, to))))
  assert.deepEqual(sel(1, 1), { from: 0, to: 1 })
  assert.deepEqual(sel(2, 6), { from: 0, to: 2 })
  assert.deepEqual(sel(5, 10), { from: 1, to: 3 })
  assert.equal(posOfBlock(s, 1), 4)
  assert.equal(posOfBlock(s, 3), s.doc.content.size)
})

test('the slash query is what follows a / that starts a word', () => {
  const at = (text: string) => {
    const s = stateOf(docOf(text))
    return slashQuery(s.apply(s.tr.setSelection(TextSelection.create(s.doc, 1 + text.length))))
  }
  assert.deepEqual(at('/'), { query: '', from: 1, to: 2 })
  assert.deepEqual(at('hello /call'), { query: 'call', from: 7, to: 12 })
  assert.deepEqual(at('/heading 2'), { query: 'heading 2', from: 1, to: 11 })
  assert.equal(at('a/b'), null)
  assert.equal(at('http://x'), null)
  assert.equal(at('/ x'), null)
  assert.equal(at('no slash'), null)
})

test('items starting with the query come before those containing it', () => {
  const found = filterItems(defaultSlashItems, 'list').map((i) => i.id)
  assert.deepEqual(found, ['bullets', 'numbers', 'todo'])
  assert.deepEqual(filterItems(defaultSlashItems, 'to').map((i) => i.id).slice(0, 3), ['todo', 'toggle', 'toggleHeading'])
  assert.deepEqual(filterItems(defaultSlashItems, 'tip').map((i) => i.id), ['callout'])
  assert.equal(filterItems(defaultSlashItems, '').length, defaultSlashItems.length)
})

test("an app's items follow the defaults, replacing one with its id", () => {
  const items = slashItems([{ id: 'database', title: 'Database', command: true }, { id: 'quote', title: 'Pull quote' }, { title: 'no id' } as never])
  assert.equal(items.at(-1)?.id, 'quote')
  assert.equal(items.at(-2)?.id, 'database')
  assert.equal(items.filter((i) => i.id === 'quote').length, 1)
  assert.equal(items.length, defaultSlashItems.length + 1)
})
