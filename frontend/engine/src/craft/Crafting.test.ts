import assert from 'node:assert/strict'
import { test } from 'node:test'
import { digLimits, missing, RECIPES, spend } from './Crafting.ts'

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
