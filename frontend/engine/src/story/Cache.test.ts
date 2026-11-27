import assert from 'node:assert/strict'
import { test } from 'node:test'
import { hawksRest } from '../area/Samples.ts'
import { cacheReady, cacheSpot, POULTICE } from './Cache.ts'

test("the cache lies by Hawk's Rest's box bed, once the stones have remembered", () => {
  const s = cacheSpot(hawksRest.pieces, 2)
  assert.deepEqual(s, { x: 9, z: 3 })
  assert.equal(cacheSpot([], 2), null)
  assert.ok(!cacheReady({ companion: true }))
  assert.ok(cacheReady({ stones: true }))
  assert.ok(!cacheReady({ stones: true, cache: true }))
  assert.equal(POULTICE.herb, 'yarrow')
})
