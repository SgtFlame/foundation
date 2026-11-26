import assert from 'node:assert/strict'
import { test } from 'node:test'
import { GREATER_PERIOD, moonPhase, moons } from './Moons.ts'

test('a moon is new at 0, full at half its period, and new again after it', () => {
  assert.equal(moonPhase(0, GREATER_PERIOD).lit, 0)
  assert.ok(Math.abs(moonPhase(15, GREATER_PERIOD).lit - 1) < 1e-9)
  assert.ok(moonPhase(30, GREATER_PERIOD).lit < 1e-9)
  assert.equal(moonPhase(7, GREATER_PERIOD).waxing, true)
  assert.equal(moonPhase(22, GREATER_PERIOD).waxing, false)
  assert.ok(Math.abs(moonPhase(-15, GREATER_PERIOD).lit - 1) < 1e-9)
})

test('the two moons are both full on some nights and not on others', () => {
  const both = []
  for (let n = 0; n < 390; n++) {
    const m = moons(n)
    if (m.greater.lit > 0.95 && m.lesser.lit > 0.95) both.push(n)
  }
  assert.ok(both.length > 0 && both.length < 40, `nights both full: ${both.length}`)
})
