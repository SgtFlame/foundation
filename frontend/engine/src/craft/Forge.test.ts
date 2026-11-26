import { test } from 'node:test'
import assert from 'node:assert/strict'
import { forgeAction, ironFrom, smeltFraction, smeltLeft } from './Forge.ts'

const pack = { dust: 0, rock: 0, ore: 3, iron: 0, capacity: 4 }

test('the forge takes up to 2 m³ of ore, then smelts for 45 s a m³', () => {
  assert.deepEqual(forgeAction(null, pack, 0), { kind: 'smelt', ore: 2 })
  assert.equal(forgeAction(null, { ...pack, ore: 0 }, 0).kind, 'none')
  const f = { ore: 2, at: 1000 }
  assert.equal(smeltLeft(f, 1000), 90_000)
  assert.equal(smeltFraction(f, 46_000), 0.5)
  assert.deepEqual(forgeAction(f, pack, 46_000), { kind: 'wait', seconds: 45 })
  assert.deepEqual(forgeAction(f, { ...pack, ore: 1 }, 91_000), { kind: 'take', iron: 1.6 })
  assert.equal(forgeAction(f, { ...pack, rock: 3 }, 91_000).kind, 'none')
  assert.equal(ironFrom(0.02), 0.016)
})
