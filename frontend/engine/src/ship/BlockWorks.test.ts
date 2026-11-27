import { test } from 'node:test'
import assert from 'node:assert/strict'
import { affordable, finishedBlocks, nextAssembly, wantedBlocks } from './BlockWorks.ts'
import { blockCost } from './Blueprint.ts'

test('the assembler makes what the blueprints want and storage pays for', () => {
  assert.equal(affordable({ plate: 5 }, { plate: 2 }), 2)
  assert.equal(affordable({ plate: 5 }, { plate: 2, wire: 1 }), 0)
  assert.deepEqual(nextAssembly({ plate: 7, wire: 2 }, { armor: 2, gyro: 1 }), { block: 'armor', count: 2 })
  assert.deepEqual(nextAssembly({ cement: 9 }, { armor: 2 }), { block: 'concrete', count: 4 })
  assert.deepEqual(nextAssembly({ plate: 40 }), { block: 'armor', count: 10 })
  assert.equal(nextAssembly({ ferrite: 3 }), null)
  assert.deepEqual(wantedBlocks([{ types: ['armor', 'armor', 'light'], placed: [0] }, { placed: [] }]), { armor: 1, light: 1 })
  assert.deepEqual(finishedBlocks({ plate: 2, 'block:armor': 3, 'block:gyro': 0 }), { 'block:armor': 3 })
})

test('a finished block costs nothing to place', () => {
  assert.deepEqual(blockCost(['armor'], 0, { armor: 1 }), { type: 'armor', cost: {}, finished: true })
  assert.deepEqual(blockCost(['armor'], 0, {}), { type: 'armor', cost: { plate: 2 } })
})
