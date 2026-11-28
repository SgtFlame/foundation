import { test } from 'node:test'
import assert from 'node:assert/strict'
import { findInBlocks, findInText, nextMatch, replacements } from './find.ts'

test('finds every match, ignoring case unless asked', () => {
  assert.deepEqual(findInText('Cat cat CAT', 'cat'), [{ from: 0, to: 3 }, { from: 4, to: 7 }, { from: 8, to: 11 }])
  assert.deepEqual(findInText('Cat cat CAT', 'cat', { caseSensitive: true }), [{ from: 4, to: 7 }])
})

test('whole words only', () => {
  assert.deepEqual(findInText('cat concatenate cat_ cat.', 'cat', { wholeWord: true }), [{ from: 0, to: 3 }, { from: 21, to: 24 }])
  assert.deepEqual(findInText('été étés', 'été', { wholeWord: true }), [{ from: 0, to: 3 }])
})

test('special characters are literal', () => {
  assert.deepEqual(findInText('a.b axb (c)', 'a.b'), [{ from: 0, to: 3 }])
  assert.deepEqual(findInText('a.b axb (c)', '(c)'), [{ from: 8, to: 11 }])
  assert.deepEqual(findInText('anything', ''), [])
})

test('matches never overlap', () => {
  assert.deepEqual(findInText('aaaa', 'aa'), [{ from: 0, to: 2 }, { from: 2, to: 4 }])
})

test('blocks map to document positions', () => {
  const blocks = [{ text: 'one two', start: 1 }, { text: 'two￼three', start: 10 }]
  assert.deepEqual(findInBlocks(blocks, 'two'), [{ from: 5, to: 8 }, { from: 10, to: 13 }])
})

test('each match is replaced exactly once, last first', () => {
  const matches = findInText('a a a', 'a')
  const order = replacements(matches)
  assert.deepEqual(order.map((r) => r.from), [4, 2, 0])
  let text = 'a a a'
  for (const r of order) text = text.slice(0, r.from) + 'aa' + text.slice(r.to)
  assert.equal(text, 'aa aa aa')
})

test('next and previous wrap round', () => {
  const m = [{ from: 2, to: 3 }, { from: 8, to: 9 }]
  assert.equal(nextMatch(m, 0), 0)
  assert.equal(nextMatch(m, 3), 1)
  assert.equal(nextMatch(m, 9), 0)
  assert.equal(nextMatch(m, 8, true), 0)
  assert.equal(nextMatch(m, 2, true), 1)
  assert.equal(nextMatch([], 0), -1)
})
