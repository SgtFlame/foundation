import assert from 'node:assert/strict'
import { test } from 'node:test'
import { moons } from '../sky/Moons.ts'
import { nextWaking } from './Stones.ts'
import { tinctureReady } from './Tincture.ts'

test('the tincture is made only at the lesser full moon, with both herbs and the notes', () => {
  const full = nextWaking(3)
  const dark = full + 6
  assert.ok(moons(dark).lesser.lit < 0.5)
  const items = { hedgecraft: 1, mugwort: 1, lavender: 1 }
  assert.equal(tinctureReady(full, items, {}), true)
  assert.equal(tinctureReady(dark, items, {}), false)
  assert.equal(tinctureReady(full, { ...items, lavender: 0 }, {}), false)
  assert.equal(tinctureReady(full, { mugwort: 1, lavender: 1 }, {}), false)
  assert.equal(tinctureReady(full, items, { truedream: true }), false)
})
