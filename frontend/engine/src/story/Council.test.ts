import assert from 'node:assert/strict'
import { test } from 'node:test'
import { COUNCIL_LINES, councilReady, isHawksRest } from './Council.ts'

test('the hearth-watch meets after word of Ashby, at Hawk\'s Rest (S31)', () => {
  assert.equal(councilReady({ ashby: true }), false)
  assert.equal(councilReady({ told: true }), true)
  assert.equal(councilReady({ told: true, council: true }), false)
  assert.equal(isHawksRest("Hawk's Rest"), true)
  assert.equal(isHawksRest('Sheaflands farmstead kitchen'), false)
  assert.equal(COUNCIL_LINES.length, 3)
})

test('the reeve hears of the second village once the ford is reached (S33)', async () => {
  const { reeveReady, REEVE_LINES } = await import('./Council.ts')
  assert.equal(reeveReady({ council: true }), false)
  assert.equal(reeveReady({ northroad: true }), true)
  assert.equal(reeveReady({ northroad: true, reeve: true }), false)
  assert.ok(REEVE_LINES.every((l) => !/under|cave|tunnel|dungeon/i.test(l)))
})
