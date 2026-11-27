import assert from 'node:assert/strict'
import { test } from 'node:test'
import { bankSageReady, bankSageSpot, hearthStonesReady, hearthWardMissing, hearthWardReady } from './HearthWard.ts'

test('the hearth ward wants the lore, five stones and a sage (S24)', () => {
  assert.equal(hearthStonesReady({ ward: true }, {}), false)
  assert.equal(hearthStonesReady({ tried: true }, {}), true)
  assert.equal(hearthStonesReady({ tried: true }, { riverstones: 1 }), false)
  assert.equal(hearthStonesReady({ tried: true, hearthward: true }, {}), false)
  assert.equal(bankSageReady({ tried: true }, {}, {}), true)
  assert.equal(bankSageReady({ tried: true }, { sage: 1 }, {}), false)
  assert.equal(bankSageReady({ tried: true }, {}, { 'ditch-sage': true }), false)
  assert.equal(hearthWardReady({ tried: true }, { riverstones: 1 }), false)
  assert.equal(hearthWardReady({ tried: true }, { riverstones: 1, sage: 1 }), true)
  assert.match(hearthWardMissing({}), /river stones/)
  assert.match(hearthWardMissing({ riverstones: 1 }), /sage/)
  assert.equal(hearthWardMissing({ riverstones: 1, sage: 1 }), '')
  assert.deepEqual(bankSageSpot({ x: 10, z: 4 }), { x: 12.2, z: 4.6 })
})
