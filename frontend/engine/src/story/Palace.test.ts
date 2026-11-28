import { test } from 'node:test'
import assert from 'node:assert/strict'
import { atGate, gateSpot, palaceReady, PALACE_LINES } from './Palace.ts'

test('the Steward reads the letter once the player reaches Aldermere (S35)', () => {
  assert.equal(palaceReady({ reeve: true }), false)
  assert.equal(palaceReady({ aldermere: true }), true)
  assert.equal(palaceReady({ aldermere: true, steward: true }), false)
  const g = gateSpot(20, 2)
  assert.deepEqual(g, { x: 20, z: 3 })
  assert.ok(atGate(g, { x: 22, z: 5 }))
  assert.ok(!atGate(g, { x: 20, z: 12 }))
  assert.equal(PALACE_LINES.length, 3)
})
