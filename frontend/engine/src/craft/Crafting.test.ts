import assert from 'node:assert/strict'
import { test } from 'node:test'
import { digLimits, missing, nextRecipe, RECIPES, spend } from './Crafting.ts'

const drill = RECIPES[0]

test('a drill takes ore and rock, once', () => {
  assert.match(missing(drill, { ore: 1, rock: 3 }, {}), /2 m³ of ore and 1 m³ of rock/)
  assert.equal(missing(drill, { ore: 2, rock: 1 }, {}), '')
  assert.match(missing(drill, { ore: 2, rock: 1 }, { drill: true }), /already/)
  assert.deepEqual(spend(drill, { ore: 2.5, rock: 1.2, dust: 0.3 }), { ore: 0.5, rock: 0.2, dust: 0.3 })
})

test('the drill digs wider and faster than hands', () => {
  const hand = digLimits({})
  const d = digLimits({ drill: true })
  assert.ok(d.maxBrush > hand.maxBrush * 2)
  assert.ok(d.repeat < hand.repeat / 2)
})

test('the bench offers the drill, then the turret, then nothing', () => {
  assert.equal(nextRecipe(undefined)?.id, 'drill')
  assert.equal(nextRecipe({ drill: true })?.id, 'turret')
  assert.equal(nextRecipe({ drill: true, turret: true }), null)
  const t = RECIPES.find((r) => r.id === 'turret')!
  assert.equal(missing(t, { ore: 2, rock: 1 }, { drill: true }).includes('1.5 m³ of rock'), true)
  assert.deepEqual(spend(t, { ore: 2.25, rock: 1.5, dust: 0 }), { ore: 0.25, rock: 0, dust: 0 })
})

test('a manual from a wreck teaches the powered drill, built on the hand drill', () => {
  assert.equal(nextRecipe({ drill: true, turret: true }, {}), null)
  assert.equal(nextRecipe({ drill: true, turret: true }, { powerDrill: true })?.id, 'powerDrill')
  assert.equal(nextRecipe({ turret: true }, { powerDrill: true })?.id, 'drill')
  assert.deepEqual(digLimits({ drill: true, powerDrill: true }), { maxBrush: 6, repeat: 0.07, name: 'powered drill' })
})
