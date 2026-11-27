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

test('the shipyard arms place what storage holds, in block order (V38)', async () => {
  const { armPicks, yardShip } = await import('./BlockWorks.ts')
  const ship = { types: ['cockpit', 'light', 'armor', 'armor', 'armor'], placed: [3] }
  assert.deepEqual(armPicks(ship, { 'block:armor': 5, 'block:light': 1, plate: 9 }), [1, 2, 4])
  assert.deepEqual(armPicks(ship, { 'block:armor': 1 }), [2])
  assert.deepEqual(armPicks({ types: Array(12).fill('armor'), placed: [] }, { 'block:armor': 20 }).length, 8)
  const s = (id: string, design: string, z: number, placed: number[] = []) => ({ id, design, placed, blocks: 2, x: 0, y: 0, z })
  assert.equal(yardShip({ x: 0, y: 0, z: 0 }, [s('a', 'skiff', 1), s('b', 'cutter', 30), s('c', 'cutter', 10), s('d', 'cutter', 2, [0, 1])])?.id, 'c')
})
