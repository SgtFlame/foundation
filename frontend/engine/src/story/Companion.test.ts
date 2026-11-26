import assert from 'node:assert/strict'
import { test } from 'node:test'
import { companionStep, VETERAN } from './Companion.ts'

test('out of a fight the companion follows a step behind', () => {
  let st = { at: { x: 10, z: 10 }, cooldown: 0 }
  for (let i = 0; i < 100; i++) {
    const r = companionStep(VETERAN, st, { x: 0, z: 0, facing: 0 }, [], null, 0.1)
    assert.equal(r.strike, null)
    st = { at: r.at, cooldown: r.cooldown }
  }
  assert.ok(Math.hypot(st.at.x, st.at.z + 1.8) < 0.1)
})

test("in a fight it takes the foe's flank and strikes on its own time", () => {
  let st = { at: { x: 0, z: -2 }, cooldown: 0 }
  const foes = [{ id: 'h1', x: 0, z: 3, alive: true }, { id: 'h2', x: 8, z: 8, alive: true }]
  const strikes: string[] = []
  for (let i = 0; i < 60; i++) {
    const r = companionStep(VETERAN, st, { x: 0, z: 1.5, facing: 0 }, foes, 'h1', 0.1)
    if (r.strike) strikes.push(r.strike)
    st = { at: r.at, cooldown: r.cooldown }
  }
  assert.ok(strikes.length >= 2 && strikes.length <= 3, String(strikes.length))
  assert.ok(strikes.every((s) => s === 'h1'))
  assert.ok(Math.abs(st.at.x) > 1, 'beside the foe, not behind the character')
})
