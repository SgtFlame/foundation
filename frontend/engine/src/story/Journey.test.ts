import { test } from 'node:test'
import assert from 'node:assert/strict'
import { afterJourney, isAldermere, journeyReady, LEGS } from './Journey.ts'

test('the road to Aldermere opens with the reeve\'s letter (S34)', () => {
  assert.equal(journeyReady({ northroad: true }), false)
  assert.equal(journeyReady({ reeve: true }), true)
  assert.equal(journeyReady({ reeve: true, aldermere: true }), false)
  assert.ok(isAldermere('Aldermere gate'))
  assert.ok(!isAldermere("Hawk's Rest"))
  assert.deepEqual(afterJourney({ day: 23, hour: 8 }), { day: 27, hour: 12 })
  assert.equal(LEGS.length, 3) // several towns on the way, then the capital
})
