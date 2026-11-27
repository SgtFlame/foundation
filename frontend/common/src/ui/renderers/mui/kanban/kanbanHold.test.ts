// node --test src/**/*.test.ts (yarn test in frontend/common)
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { between, layout, live, placedFrom, step, unmoved, type Hold, type Placed } from './kanbanHold.ts'

const placed: Placed[] = [
  { id: 'a', column: 'todo', version: 1 },
  { id: 'b', column: 'todo', version: 1 },
  { id: 'c', column: 'todo', version: 1 },
  { id: 'd', column: 'done', version: 1 },
]
const ids = (cols: Record<string, Placed[]>) => Object.fromEntries(Object.entries(cols).map(([k, v]) => [k, v.map((p) => p.id).join('')]))

test('placedFrom reads the server list or the cards by column field', () => {
  assert.deepEqual(placedFrom({ placed: [{ id: 'x', column: 'todo', version: 3, key: 'b_x' }] }), [{ id: 'x', column: 'todo', version: 3, key: 'b_x' }])
  assert.deepEqual(
    placedFrom({ cards: { records: [{ _id: 'x', _seq: 2, stage: { _id: 's1' } }, { _id: 'x', stage: 's2' }, { id: 'y', stage: ['s2'] }] }, columnField: 'stage' }),
    [
      { id: 'x', column: 's1', version: 2 },
      { id: 'y', column: 's2', version: undefined },
    ],
  )
})

test('a held drop shows where it was dropped', () => {
  const holds: Hold[] = [{ id: 'a', column: 'done', index: 1, version: 1, at: 0 }]
  assert.deepEqual(ids(layout(placed, ['todo', 'done'], holds)), { todo: 'bc', done: 'da' })
  // an index past the end goes last; a column not shown drops the card
  assert.deepEqual(ids(layout(placed, ['todo', 'done'], [{ ...holds[0], index: 9 }])), { todo: 'bc', done: 'da' })
  assert.deepEqual(ids(layout(placed, ['todo'], holds)), { todo: 'bc' })
})

test('the hold ends when the server version changes, the card goes, or it expires', () => {
  const h: Hold = { id: 'a', column: 'done', index: 0, version: 1, at: 1000 }
  assert.equal(live([h], placed, 2000).holds.length, 1)
  // the move committed (or someone else's later move): the server wins
  const moved = placed.map((p) => (p.id === 'a' ? { ...p, column: 'todo', version: 2 } : p))
  assert.deepEqual(live([h], moved, 2000), { holds: [], expired: [] })
  assert.deepEqual(live([h], placed.slice(1), 2000), { holds: [], expired: [] })
  assert.deepEqual(live([h], placed, 6000), { holds: [], expired: [h] })
})

test('a drop reports the cards it fell between', () => {
  const cols = layout(placed, ['todo', 'done'], [])
  assert.deepEqual(between(cols.todo, 'a', 2), { after: 'c', before: '', index: 2 })
  assert.deepEqual(between(cols.todo, 'c', 0), { after: '', before: 'a', index: 0 })
  assert.deepEqual(between(cols.done, 'a', 0), { after: '', before: 'd', index: 0 })
  assert.deepEqual(between([], 'a', 5), { after: '', before: '', index: 0 })
  assert.equal(unmoved(cols, 'b', 'todo', 1), true)
  assert.equal(unmoved(cols, 'b', 'todo', 0), false)
  assert.equal(unmoved(cols, 'b', 'done', 1), false)
})

test('the keyboard moves a lifted card within and across columns', () => {
  const cols = layout(placed, ['todo', 'done'], [])
  const order = ['todo', 'done']
  assert.deepEqual(step(cols, order, { column: 'todo', index: 0 }, 'a', 'ArrowUp'), { column: 'todo', index: 0 })
  assert.deepEqual(step(cols, order, { column: 'todo', index: 2 }, 'a', 'ArrowDown'), { column: 'todo', index: 2 })
  assert.deepEqual(step(cols, order, { column: 'todo', index: 2 }, 'c', 'ArrowRight'), { column: 'done', index: 1 })
  assert.deepEqual(step(cols, order, { column: 'done', index: 0 }, 'd', 'ArrowRight'), { column: 'done', index: 0 })
  assert.deepEqual(step(cols, order, { column: 'done', index: 0 }, 'd', 'ArrowLeft'), { column: 'todo', index: 0 })
})
