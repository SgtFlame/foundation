import { test } from 'node:test'
import assert from 'node:assert/strict'
import { forgeAction, ironFrom, limeFor, partsFrom, smeltFraction, smeltLeft } from './Forge.ts'

const pack = { dust: 0, rock: 0, ore: 3, iron: 0, capacity: 4 }

test('the forge takes up to 2 m³ of ore, then smelts for 45 s a m³', () => {
  assert.deepEqual(forgeAction(null, pack, 0), { kind: 'smelt', ore: 2, what: 'ore', fuel: false })
  assert.equal(forgeAction(null, { ...pack, ore: 0 }, 0).kind, 'none')
  const f = { ore: 2, at: 1000 }
  assert.equal(smeltLeft(f, 1000), 90_000)
  assert.equal(smeltFraction(f, 46_000), 0.5)
  assert.deepEqual(forgeAction(f, pack, 46_000), { kind: 'wait', seconds: 45 })
  assert.deepEqual(forgeAction(f, { ...pack, ore: 1 }, 91_000), { kind: 'take', iron: 1.6 })
  assert.equal(forgeAction(f, { ...pack, rock: 3 }, 91_000).kind, 'none')
  assert.equal(ironFrom(0.02), 0.016)
})

test('V30: the forge refines a mineral into components, and rock into cement', () => {
  const p = { dust: 0, rock: 1, ore: 0, iron: 0, minerals: { ferrite: 0.5, silica: 1.2, ice: 3 }, capacity: 4 }
  // ice is not refined; silica is the mineral held most
  assert.deepEqual(forgeAction(null, p, 0), { kind: 'smelt', ore: 1.2, what: 'silica', fuel: false })
  assert.deepEqual(forgeAction(null, { ...p, minerals: {}, parts: { lime: 2 } }, 0), { kind: 'smelt', ore: 1, what: 'rock', fuel: false })
  assert.equal(forgeAction(null, { ...p, minerals: {} }, 0).kind, 'none') // V33: no lime, no cement
  assert.equal(forgeAction(null, { ...p, rock: 0.1, minerals: {} }, 0).kind, 'none')
  const f = { ore: 1.2, at: 0, kind: 'silica' as const }
  assert.equal(smeltLeft(f, 0), 72_000)
  assert.deepEqual(forgeAction(f, p, 72_000), { kind: 'takeParts', part: 'solarCell', count: 4 })
  assert.equal(partsFrom('ferrite', 0.5), 4)
  assert.equal(partsFrom('rock', 1), 6)
})

test('V33: limestone burns into lime, cement takes lime, and coal fuels the forge', () => {
  const p = { dust: 0, rock: 3, ore: 0, iron: 0, minerals: { limestone: 1, coal: 0.3 }, parts: { lime: 1 }, capacity: 4 }
  // coal is fuel, not a load; limestone is the mineral to burn
  assert.deepEqual(forgeAction(null, p, 0), { kind: 'smelt', ore: 1, what: 'limestone', fuel: true })
  assert.equal(partsFrom('limestone', 1), 6)
  // one sack of lime makes cement of half a m³ of rock
  assert.deepEqual(forgeAction(null, { ...p, minerals: {} }, 0), { kind: 'smelt', ore: 0.5, what: 'rock', fuel: false })
  assert.equal(limeFor(0.5), 1)
  assert.equal(limeFor(1.2), 3)
  // a fuelled load is done twice as fast
  assert.equal(smeltLeft({ ore: 1, at: 0, kind: 'limestone', rate: 2 }, 0), 15_000)
})
