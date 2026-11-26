import assert from 'node:assert/strict'
import { test } from 'node:test'
import { aching, dreams, remembered, WOUND_CHILL } from './Remember.ts'

test('the ford is dreamt once, in a blessed place, after the thanks', () => {
  assert.equal(dreams({ thanked: true }, true), true)
  assert.equal(dreams({ thanked: true }, false), false)
  assert.equal(dreams({ defended: true }, true), false)
  assert.equal(dreams({ thanked: true, dream: true }, true), false)
})

test('the dream teaches the long spear, and its wound keeps a little Chill', () => {
  const base = { blade: 2, polearm: 1, reading: 1 }
  assert.deepEqual(remembered(base, {}), base)
  assert.equal(remembered(base, { dream: true }).polearm, 2)
  assert.equal(remembered({ ...base, polearm: 5 }, { dream: true }).polearm, 5)
  assert.equal(aching({ hum: 1, chill: 5, hunger: 10 }, true).chill, WOUND_CHILL)
  assert.equal(aching({ hum: 1, chill: 50, hunger: 10 }, true).chill, 50)
  assert.equal(aching({ hum: 1, chill: 5, hunger: 10 }, false).chill, 5)
})
