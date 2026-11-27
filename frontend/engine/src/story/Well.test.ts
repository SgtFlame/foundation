import assert from 'node:assert/strict'
import { test } from 'node:test'
import { wellReady, wellSpot } from './Well.ts'

test('the well calls once both hearths are warded (S26)', () => {
  assert.equal(wellReady({ hearthward: true }), false)
  assert.equal(wellReady({ hearthward: true, farmward: true }), true)
  assert.equal(wellReady({ hearthward: true, farmward: true, well: true }), false)
})

test('the well stands in the yard (S26)', () => {
  assert.deepEqual(wellSpot([{ piece: 'yard', x: 2, z: 4 }], 4), { x: 7, z: 16 })
  assert.equal(wellSpot([{ piece: 'barn', x: 2, z: 4 }], 4), null)
})
