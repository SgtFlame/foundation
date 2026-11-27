import { test } from 'node:test'
import assert from 'node:assert/strict'
import { billFor, blockCost, costLine, pay, shortOf } from './Blueprint.ts'

test("V31: a chip ship's blocks cost components; others are free", () => {
  assert.equal(blockCost(undefined, 0), null)
  const b = blockCost(['cockpit', 'thruster'], 1)
  assert.deepEqual(b, { type: 'thruster', cost: { nozzle: 1, plate: 1, wire: 1 } })
  assert.deepEqual(shortOf({ plate: 3, wire: 0 }, b!.cost), ['nozzle', 'wire'])
  assert.deepEqual(shortOf({ plate: 1, wire: 1, nozzle: 1 }, b!.cost), [])
  assert.equal(costLine({ plate: 3, wire: 2 }), '3 steel plates, 2 copper wire')
  assert.deepEqual(pay({ plate: 3, wire: 2 }, { plate: 3, wire: 1 }), { plate: 0, wire: 1 })
  assert.deepEqual(billFor(['armor', 'armor', 'light'], [0]), { plate: 2, wire: 1 })
})
