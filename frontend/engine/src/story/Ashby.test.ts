import assert from 'node:assert/strict'
import { test } from 'node:test'
import { ashbyReady, remnantAt, remnantSpots, REMNANTS, trackPrints, trackReady } from './Ashby.ts'

test('the track calls after the well ward, the remnants after the track (S28, S29)', () => {
  assert.equal(trackReady({ well: true }), false)
  assert.equal(trackReady({ wellward: true }), true)
  assert.equal(trackReady({ wellward: true, track: true }), false)
  assert.equal(ashbyReady({ wellward: true }), false)
  assert.equal(ashbyReady({ track: true }), true)
  assert.equal(ashbyReady({ track: true, ashby: true }), false)
})

test('frost prints run from the well to the signpost (S28)', () => {
  const prints = trackPrints({ x: 0, z: 0 }, { x: 10, z: 0 })
  assert.ok(prints.length >= 4)
  assert.ok(prints.every((p) => p.x > 1 && p.x < 9 && Math.abs(p.z) < 0.5))
  assert.deepEqual(trackPrints({ x: 1, z: 1 }, { x: 1, z: 1 }), [])
})

test("Ashby's remnants lie round the middle of its area (S29)", () => {
  const spots = remnantSpots(6, 6, 10)
  assert.equal(spots.length, REMNANTS.length)
  assert.deepEqual({ x: spots[0].x, z: spots[0].z }, { x: 22, z: 24 })
  assert.equal(remnantAt(spots, { x: 22.5, z: 24 })?.id, 'door')
  assert.equal(remnantAt(spots, { x: 30, z: 30 }), null)
  // the lore: taken, not killed
  assert.ok(REMNANTS.some((r) => /no blood|none/i.test(r.text)))
})
