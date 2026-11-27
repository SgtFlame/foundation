import assert from 'node:assert/strict'
import { test } from 'node:test'
import { moons } from '../sky/Moons.ts'
import { BUNDLE_HERBS, bundleSpots, bundlesReady, nextGreaterFull, sillLine, sillSpot } from './Bundles.ts'

const cottage = [
  { id: 'a', piece: 'shelf', x: 0, z: 0 },
  { id: 'h', piece: 'hearth', x: 1, z: 0 },
  { id: 'b', piece: 'shelf', x: 3, z: 0 },
  { id: 'w', piece: 'window', x: 4, z: 0 },
]

test('the bundles hang two to a shelf, one of each herb', () => {
  const s = bundleSpots(cottage, 2)
  assert.equal(s.length, BUNDLE_HERBS.length)
  assert.deepEqual(s.map((x) => [x.spot, x.x, x.z]), [['bundle-lavender', 0.5, 1], ['bundle-mugwort', 1.5, 1], ['bundle-valerian', 6.5, 1], ['bundle-sage', 7.5, 1]])
  assert.deepEqual(bundleSpots([{ id: 'w', piece: 'window', x: 0, z: 0 }], 2), [])
})

test('the bundles wait for the cache and stop once taken', () => {
  assert.equal(bundlesReady({ stones: true }), false)
  assert.equal(bundlesReady({ cache: true }), true)
  assert.equal(bundlesReady({ cache: true, bundles: true }), false)
})

test('the sill tells the next full moons', () => {
  assert.deepEqual(sillSpot(cottage, 2), { x: 9, z: 1 })
  const g = nextGreaterFull(3)
  assert.ok(moons(g).greater.lit > 0.97 && g >= 3 && g < 34)
  assert.match(sillLine(g), /the greater tonight/)
  assert.match(sillLine(3), /lesser moon is full (tonight|tomorrow night|in \d+ nights)/)
})
