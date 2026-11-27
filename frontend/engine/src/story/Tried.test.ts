import assert from 'node:assert/strict'
import { test } from 'node:test'
import { TRIED_NIGHT, triedReady } from './Tried.ts'

test('the ward is tried once it is laid, and only once (S23)', () => {
  assert.equal(triedReady({}), false)
  assert.equal(triedReady({ truedream: true }), false)
  assert.equal(triedReady({ ward: true }), true)
  assert.equal(triedReady({ ward: true, tried: true }), false)
  assert.equal(TRIED_NIGHT.raiders, 1)
  assert.ok(TRIED_NIGHT.length < 180)
})
