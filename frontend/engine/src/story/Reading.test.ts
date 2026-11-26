import assert from 'node:assert/strict'
import { test } from 'node:test'
import { clarity, ditchSpot, vision, WENLAN_READING, wenlanAnswer } from './Reading.ts'

test('three cards, past, present and future, the present naming the ditch', () => {
  assert.deepEqual(WENLAN_READING.map((c) => c.place), ['past', 'present', 'future'])
  assert.match(vision(WENLAN_READING[1], 1), /culvert/)
  assert.match(vision(WENLAN_READING[1], 0.2), /low corner of a field/)
})

test('stillness makes every reading at least half clear', () => {
  for (let n = 0; n < 30; n++) {
    assert.ok(clarity(n, true) >= 0.5)
    assert.ok(clarity(n, false) <= 0.7)
  }
})

test('Wenlan answers what he is told', () => {
  assert.match(wenlanAnswer('all'), /eyeless/)
  assert.doesNotMatch(wenlanAnswer('ditch'), /eyeless/)
})

test('the bough lies at the low corner of the field furthest east', () => {
  const s = ditchSpot({ a: { piece: 'field', x: 8, z: 7 }, b: { piece: 'field', x: 10, z: 7 }, c: { piece: 'road', x: 12, z: 7 } }, 10)
  assert.deepEqual(s, { x: 109.4, z: 90.4, field: { x: 105, z: 80, w: 10, d: 20 } })
  assert.equal(ditchSpot({}, 10), null)
})
