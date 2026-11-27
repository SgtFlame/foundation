import assert from 'node:assert/strict'
import { test } from 'node:test'
import { riverStonesReady, thresholdSpot, wardedNight, wardMissing, wardReady } from './Ward.ts'

test('the stones wait for the true dream, and the ward for stones and sage', () => {
  assert.equal(riverStonesReady({}, {}), false)
  assert.equal(riverStonesReady({ truedream: true }, {}), true)
  assert.equal(riverStonesReady({ truedream: true }, { riverstones: 1 }), false)
  assert.equal(wardReady({ truedream: true }, { riverstones: 1 }), false)
  assert.equal(wardReady({ truedream: true }, { riverstones: 1, sage: 1 }), true)
  assert.equal(wardReady({ truedream: true, ward: true }, { riverstones: 1, sage: 1 }), false)
  assert.match(wardMissing({ sage: 1 }), /river stones/)
  assert.match(wardMissing({ riverstones: 1 }), /sage/)
})

test('the threshold is the door cell, and a warded night clears the Chill', () => {
  assert.deepEqual(thresholdSpot([{ piece: 'door', x: 2, z: 3 }], 2), { x: 5, z: 7 })
  assert.equal(thresholdSpot([], 2), null)
  assert.deepEqual(wardedNight({ hum: 1, chill: 70, hunger: 40 }), { hum: 1, chill: 0, hunger: 40 })
})
