import assert from 'node:assert/strict'
import { test } from 'node:test'
import { Hold, mineralsIn, secondsFor, spotKey, toolPower, workFor } from './Mining.ts'

test('harder ground takes longer, and better tools go faster', () => {
  assert.deepEqual(workFor({ dust: 1, rock: 0, ore: 0 }), { work: 1, main: 'dust' })
  assert.equal(workFor({ dust: 0.2, rock: 1, ore: 0.1 }).main, 'rock')
  assert.equal(workFor({ dust: 0, rock: 0, ore: 0 }).main, null)
  const rock = { dust: 0, rock: 1, ore: 0 }
  assert.equal(secondsFor(rock, {}), 3)
  assert.equal(secondsFor(rock, { drill: true }), 0.5)
  assert.ok(secondsFor({ dust: 0, rock: 0, ore: 1 }, {}) > secondsFor(rock, {}))
  assert.ok(toolPower({ drill: true, powerDrill: true }) > toolPower({ drill: true }))
})

test('a hold fills on one spot, starts over on another, and is done once', () => {
  const h = new Hold()
  assert.equal(h.step(0.5, 'a', 2, 1.5), false)
  assert.equal(h.fraction, 0.375)
  assert.equal(h.step(0.5, 'b', 2, 1.5), false)
  assert.equal(h.fraction, 0.375)
  assert.equal(h.step(1, 'b', 2, 1.5), true)
  assert.equal(h.fraction, 0)
  assert.equal(h.step(1, 'c', 0, 1.5), false)
  assert.equal(spotKey({ x: 1.1, y: 0, z: -0.2 }, 0.5), spotKey({ x: 1.0, y: 0.1, z: -0.1 }, 0.5))
})

test('minerals are mined harder than rock, and the main one names the work (V29)', () => {
  const w = workFor({ dust: 0, rock: 0.2, ore: 0, minerals: { wolframite: 0.5, malachite: 0.1 } })
  assert.equal(w.main, 'wolframite')
  assert.ok(Math.abs(w.work - (0.2 * 3 + 0.6 * 4)) < 1e-9)
  assert.deepEqual(mineralsIn({ dust: 0, rock: 0, ore: 0 }), { total: 0, main: null })
})
