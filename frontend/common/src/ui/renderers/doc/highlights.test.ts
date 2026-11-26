// node --test src/**/*.test.ts (yarn test in frontend/common)
import { test } from 'node:test'
import assert from 'node:assert/strict'
import * as Y from 'yjs'
import { describeSelection, findAnchor, headingPathAt, placeHighlights } from './highlights.ts'

const board = `# Table booking

## Bookings
A table holds four. Guests book online.

\`\`\`
# not a heading
\`\`\`

## Deposits
A table holds four at lunch. Deposits are £10 a head.
`

test('a selection is told with the headings it falls under', () => {
  const from = board.indexOf('Deposits are')
  const s = describeSelection(board, from, from + 'Deposits are £10'.length)
  assert.equal(s.quote, 'Deposits are £10')
  assert.deepEqual(s.headingPath, ['Table booking', 'Deposits'])
  assert.match(s.context, /four at lunch/)
  // a heading inside fenced code is not one
  assert.deepEqual(headingPathAt(board, board.indexOf('not a heading')), ['Table booking', 'Bookings'])
})

test('a quote is found again under its headings, by its context, and loosely', () => {
  const twice = 'A table holds four'
  const deposits = findAnchor(board, { quote: twice, headingPath: ['Table booking', 'Deposits'] })
  assert.equal(deposits?.from, board.lastIndexOf(twice))
  const byContext = findAnchor(board, { quote: twice, context: 'lunch deposits' })
  assert.equal(byContext?.from, board.lastIndexOf(twice))
  const loose = findAnchor(board, { quote: 'guests  BOOK\nonline' })
  assert.equal(board.slice(loose!.from, loose!.to), 'Guests book online')
  assert.equal(findAnchor(board, { quote: 'children eat free' }), undefined)
})

test('a range is used only while it still holds its quote', () => {
  const at = board.indexOf('Guests book online')
  const [kept] = placeHighlights(board, [{ id: 't1', range: { from: at, to: at + 18 }, anchor: { quote: 'Guests book online' } }])
  assert.deepEqual([kept.from, kept.to, kept.kind], [at, at + 18, 'anchored'])
  const edited = board.replace('Guests book', 'People reserve')
  assert.equal(placeHighlights(edited, [{ id: 't1', range: { from: at, to: at + 18 }, anchor: { quote: 'Guests book online' } }]).length, 0)
})

test('highlights follow concurrent typing by their relative positions', () => {
  // Two people's documents, synced both ways
  const ana = new Y.Doc()
  const bob = new Y.Doc()
  ana.on('update', (u: Uint8Array, origin: unknown) => origin !== 'sync' && Y.applyUpdate(bob, u, 'sync'))
  bob.on('update', (u: Uint8Array, origin: unknown) => origin !== 'sync' && Y.applyUpdate(ana, u, 'sync'))
  ana.getText('markdown').insert(0, board)
  const quote = 'Deposits are £10 a head'
  const at = board.indexOf(quote)
  // Ana selects the deposit rule and starts a thread there
  const sel = describeSelection(board, at, at + quote.length, ana.getText('markdown'))
  assert.ok(sel.ypos)
  // Bob types above it and inside the sentence before it; Ana types right after it
  bob.getText('markdown').insert(board.indexOf('Guests'), 'Walk-ins are welcome. ')
  bob.getText('markdown').insert(bob.getText('markdown').toString().indexOf('at lunch'), 'only ')
  ana.getText('markdown').insert(ana.getText('markdown').toString().indexOf(quote) + quote.length, ', paid when booking')
  for (const doc of [ana, bob]) {
    const text = doc.getText('markdown').toString()
    const [h] = placeHighlights(text, [{ id: 't1', range: sel.range, anchor: { quote, ypos: sel.ypos } }], doc)
    assert.equal(text.slice(h.from, h.to), quote)
  }
})
