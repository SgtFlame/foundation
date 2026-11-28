import { test } from 'node:test'
import assert from 'node:assert/strict'
import { clampIndents, defaultSetup, pageBox, paginate, setupOf, storedSetup, type MeasuredBlock } from './pages.ts'

const H = 100

test('blocks that fit share a page; the next starts a new one', () => {
  const l = paginate([{ height: 40 }, { height: 40 }, { height: 40 }], { contentHeight: H })
  assert.equal(l.pages, 2)
  assert.deepEqual(l.pageOf, [1, 1, 2])
  assert.equal(l.breaks.length, 1)
  assert.equal(l.breaks[0].block, 2)
  assert.equal(l.breaks[0].filler, 20)
  assert.equal(l.lastFiller, 60)
})

test('a heading keeps with the block after it', () => {
  const blocks: MeasuredBlock[] = [{ height: 60 }, { height: 20, kind: 'heading' }, { height: 30 }]
  const l = paginate(blocks, { contentHeight: H })
  assert.deepEqual(l.pageOf, [1, 2, 2])
  assert.equal(l.breaks[0].filler, 40)
})

test('headings in a row keep together with what follows', () => {
  const blocks: MeasuredBlock[] = [{ height: 50 }, { height: 15, kind: 'heading' }, { height: 15, kind: 'heading' }, { height: 30 }]
  assert.deepEqual(paginate(blocks, { contentHeight: H }).pageOf, [1, 2, 2, 2])
})

test('a heading whose follower cannot fit any page still stays put', () => {
  const blocks: MeasuredBlock[] = [{ height: 10, kind: 'heading' }, { height: 150 }]
  const l = paginate(blocks, { contentHeight: H })
  assert.deepEqual(l.pageOf, [1, 2])
  assert.deepEqual(l.overflow, [1])
})

test('a table splits between rows and repeats its header row', () => {
  const table: MeasuredBlock = { height: 130, kind: 'table', rows: [10, 30, 30, 30, 30], headerRows: 1 }
  const l = paginate([{ height: 20 }, table], { contentHeight: H })
  assert.equal(l.pages, 2)
  assert.deepEqual(l.pageOf, [1, 1])
  // 20 + 10 + 30 + 30 = 90: the fourth row (index 3) starts page 2 under a repeated header
  assert.equal(l.breaks.length, 1)
  assert.equal(l.breaks[0].block, 1)
  assert.equal(l.breaks[0].row, 3)
  assert.equal(l.breaks[0].repeatHeader, 10)
  assert.equal(l.breaks[0].filler, 10)
  assert.equal(l.lastFiller, 100 - 10 - 30 - 30)
})

test('a table that cannot fit its header and first row moves to the next page', () => {
  const table: MeasuredBlock = { height: 60, kind: 'table', rows: [10, 25, 25], headerRows: 1 }
  const l = paginate([{ height: 70 }, table], { contentHeight: H })
  assert.deepEqual(l.pageOf, [1, 2])
  assert.equal(l.breaks[0].row, undefined)
})

test('a block whose footnote does not fit moves to the next page with it', () => {
  const blocks: MeasuredBlock[] = [{ height: 50 }, { height: 30, footnotes: [20] }, { height: 10 }]
  const l = paginate(blocks, { contentHeight: H, footnoteSeparator: 10 })
  // 50 + 30 + 10 + 20 = 110 > 100
  assert.deepEqual(l.pageOf, [1, 2, 2])
  assert.deepEqual(l.breaks[0].footnotes, [])
  assert.deepEqual(l.lastFootnotes, [0])
  assert.equal(l.lastFootnoteHeight, 30)
  assert.equal(l.lastFiller, 100 - 40 - 30)
})

test('footnotes are numbered in document order across pages', () => {
  const blocks: MeasuredBlock[] = [{ height: 30, footnotes: [10, 10] }, { height: 40 }, { height: 30, footnotes: [10] }]
  const l = paginate(blocks, { contentHeight: H, footnoteSeparator: 5 })
  assert.deepEqual(l.breaks[0].footnotes, [0, 1])
  assert.equal(l.breaks[0].footnoteHeight, 25)
  assert.equal(l.breaks[0].filler, 100 - 70 - 25)
  assert.deepEqual(l.lastFootnotes, [2])
})

test('page and section breaks start a new page', () => {
  const blocks: MeasuredBlock[] = [{ height: 10 }, { height: 0, kind: 'pageBreak' }, { height: 10 }, { height: 0, kind: 'sectionBreak' }, { height: 10 }]
  const l = paginate(blocks, { contentHeight: H })
  assert.equal(l.pages, 3)
  assert.deepEqual(l.pageOf, [1, 1, 2, 2, 3])
  assert.equal(l.breaks[0].filler, 90)
})

test('a break at the end makes no empty page', () => {
  assert.equal(paginate([{ height: 10 }, { height: 0, kind: 'pageBreak' }], { contentHeight: H }).pages, 1)
})

test('page setup is checked, stored as differences and sized in pixels', () => {
  assert.deepEqual(setupOf(undefined), defaultSetup)
  assert.deepEqual(setupOf({ size: 'a4', margins: { top: 'x' }, color: 'red' }).margins.top, 72)
  assert.equal(setupOf({ size: 'a4', color: 'red' }).color, '')
  assert.deepEqual(storedSetup(defaultSetup), {})
  const s = setupOf({ size: 'a4', landscape: true })
  assert.deepEqual(storedSetup(s), { size: 'a4', landscape: true })
  const box = pageBox(defaultSetup)
  assert.equal(box.width, 816)
  assert.equal(box.height, 1056)
  assert.equal(box.contentHeight, 1056 - 192)
  assert.ok(pageBox(s).width > pageBox(s).height)
})

test('indents stay within the column', () => {
  assert.deepEqual(clampIndents({ left: -10, right: 0, first: -20 }, 468), { left: 0, right: 0, first: 0 })
  assert.deepEqual(clampIndents({ left: 36, right: 0, first: -18 }, 468), { left: 36, right: 0, first: -18 })
  assert.equal(clampIndents({ left: 1000, right: 0, first: 0 }, 468).left, 432)
})
