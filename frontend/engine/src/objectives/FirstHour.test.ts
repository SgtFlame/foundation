import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { ShipState } from '../ship/Ship.ts'
import { firstHour, type FirstHourInput } from './FirstHour.ts'

const blocks = { skiff: 27, prospector: 32, hauler: 58 }
const ship = (design: string, placed: number, cargo?: ShipState['cargo']) =>
  ({ id: design, world: 'w', design, blocks: blocks[design as 'skiff'], placed: Array.from({ length: placed }, (_, i) => i), x: 0, y: 0, z: 0, q: [0, 0, 0, 1], v: [0, 0, 0], pilot: '', cargo, seq: 1 }) as ShipState
const none: FirstHourInput = { walked: false, flew: false, pack: null, ships: [], blocks, progress: null }
const done = (i: FirstHourInput) => firstHour(i).filter((s) => s.done).map((s) => s.id)

test('a new player has every step ahead', () => {
  assert.deepEqual(done(none), [])
})

test('steps follow what the server keeps', () => {
  assert.deepEqual(done({ ...none, walked: true, pack: { dust: 0.3, rock: 0 }, ships: [ship('skiff', 10)] }), ['walk', 'dig', 'skiff'])
  assert.deepEqual(done({ ...none, walked: true, pack: { dust: 0, rock: 0 }, ships: [ship('skiff', 27)] }), ['walk', 'dig', 'skiff', 'build'])
})

test('a later step done counts the earlier ones', () => {
  const p = { done: { 'first-flight': true }, xp: {}, levels: {} }
  assert.deepEqual(done({ ...none, progress: p }), ['walk', 'dig', 'skiff', 'build', 'fly', 'first-flight'])
  assert.equal(done({ ...none, ships: [ship('prospector', 32, { dust: 0, rock: 0, iron: 0.5 })] }).at(-1), 'iron')
})
