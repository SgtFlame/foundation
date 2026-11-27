import assert from 'node:assert/strict'
import { test } from 'node:test'
import { bankSageReady, bankSageSpot, hearthStonesReady, hearthWarded, hearthWardMissing, hearthWardReady, nextHearthWard, wardSpots } from './HearthWard.ts'

test('the hearth ward wants the lore, five stones and a sage (S24)', () => {
  assert.equal(hearthStonesReady({ ward: true }, {}), false)
  assert.equal(hearthStonesReady({ tried: true }, {}), true)
  assert.equal(hearthStonesReady({ tried: true }, { riverstones: 1 }), false)
  assert.equal(hearthStonesReady({ tried: true, hearthward: true, farmward: true }, {}), false)
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

test('then Wenlan\'s hearth, from a third handful of stones and sage; warded hearths (S25)', () => {
  assert.equal(nextHearthWard({ ward: true }), null)
  assert.equal(nextHearthWard({ tried: true }), 'hearthward')
  assert.equal(nextHearthWard({ tried: true, hearthward: true }), 'farmward')
  assert.equal(nextHearthWard({ tried: true, hearthward: true, farmward: true }), null)
  const done = { tried: true, hearthward: true }
  assert.equal(hearthStonesReady(done, {}), true)
  assert.equal(bankSageReady(done, {}, { 'ditch-sage': true }), true)
  assert.equal(bankSageReady(done, {}, { 'ditch-sage-2': true }), false)
  assert.notEqual(wardSpots('farmward').stones, wardSpots('hearthward').stones)
  assert.equal(hearthWardReady(done, { riverstones: 1, sage: 1 }, 'farmward'), true)
  assert.equal(hearthWardReady(done, { riverstones: 1, sage: 1 }, 'hearthward'), false)
  const hawk = { npcs: {} }
  const kitchen = { npcs: { w: { dialogue: 'farmer' }, h: { kind: 'watcher' } } }
  assert.equal(hearthWarded({ hearthward: true }, hawk), true)
  assert.equal(hearthWarded({ hearthward: true }, kitchen), false)
  assert.equal(hearthWarded({ farmward: true }, kitchen), true)
  assert.equal(hearthWarded({}, hawk), false)
})

test('then the well, once looked into, from a fourth handful (S27)', () => {
  const done = { tried: true, hearthward: true, farmward: true }
  assert.equal(nextHearthWard(done), null)
  assert.equal(nextHearthWard({ ...done, well: true }), 'wellward')
  assert.equal(nextHearthWard({ ...done, well: true, wellward: true }), null)
  assert.equal(hearthStonesReady({ ...done, well: true }, {}), true)
  assert.equal(wardSpots('wellward').stones, 'culvert-stones-4')
  assert.equal(hearthWardReady({ ...done, well: true }, { riverstones: 1, sage: 1 }, 'wellward'), true)
  assert.match(hearthWardMissing({}, 'The well'), /^The well wants five river stones/)
})
