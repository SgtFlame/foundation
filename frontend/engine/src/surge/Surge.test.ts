import assert from 'node:assert/strict'
import { test } from 'node:test'
import { countdown, dronesOf, shotHits, SURGE_LENGTH, SURGE_PERIOD, surgeAt } from './Surge.ts'

test('a surge every period, on for its length, seen coming', () => {
  const t = 100 * SURGE_PERIOD
  assert.deepEqual(surgeAt(t + 1000), { index: 100, active: true, startsIn: 0, into: 1000 })
  const later = surgeAt(t + SURGE_LENGTH + 1000)
  assert.equal(later.active, false)
  assert.equal(later.startsIn, SURGE_PERIOD - SURGE_LENGTH - 1000)
  assert.equal(countdown(61_000), '1:01')
})

test("a surge's drones are everyone's, and wake while it lasts", () => {
  assert.deepEqual(dronesOf(7), dronesOf(7))
  assert.notDeepEqual(dronesOf(7), dronesOf(8))
  for (const d of dronesOf(7)) assert.ok(d.wakes >= 0 && d.wakes < SURGE_LENGTH - 20_000)
})

test('a shot hits what is close to its line, ahead and in range', () => {
  const eye = { x: 0, y: 0, z: 0 }
  const dir = { x: 0, y: 0, z: -1 }
  assert.equal(shotHits(eye, dir, { x: 0.5, y: 0, z: -10 }), 10)
  assert.equal(shotHits(eye, dir, { x: 3, y: 0, z: -10 }), null)
  assert.equal(shotHits(eye, dir, { x: 0, y: 0, z: 10 }), null)
  assert.equal(shotHits(eye, dir, { x: 0, y: 0, z: -80 }), null)
})
