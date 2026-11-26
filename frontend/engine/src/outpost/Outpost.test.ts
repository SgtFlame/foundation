import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import type { ShipState } from '../ship/Ship.ts'
import { atOutpost, handInProblem, thrustFor } from './Outpost.ts'

const ship = (over: Partial<ShipState>): ShipState => ({ id: 's1', design: 'skiff', x: 0, y: 110, z: 0, q: [0, 0, 0, 1], placed: [], seq: 1, ...over }) as ShipState

test('a ship counts at the outpost within 40 m of the beacon', () => {
  assert.ok(atOutpost(ship({ x: 30, y: 105 }), 100, 8))
  assert.ok(!atOutpost(ship({ x: 60, y: 90 }), 100, 8))
})

test('what keeps a contract from being handed in', () => {
  const all = Array.from({ length: 27 }, (_, i) => i)
  assert.match(handInProblem('first-flight', null, null, 27), /park a ship/)
  assert.match(handInProblem('first-flight', ship({ placed: all.slice(0, 20) }), null, 27), /7 blocks/)
  assert.equal(handInProblem('first-flight', ship({ placed: all }), null, 27), '')
  assert.match(handInProblem('beacon-iron', ship({ cargo: { dust: 0, rock: 2, iron: 0.5 } }), null, 32), /iron/)
  assert.equal(handInProblem('beacon-iron', ship({ cargo: { dust: 0, rock: 0, iron: 1 } }), null, 32), '')
  assert.equal(handInProblem('beacon-iron', ship({}), { done: { 'beacon-iron': true }, xp: {}, levels: {} }, 32), 'done')
})

test('a job of the job board is never done, needs its Trade level, and the hold to carry it (V11)', () => {
  const contracts = JSON.parse(readFileSync(new URL('../../../../backend/features/game/contracts/contracts.json', import.meta.url), 'utf8')).contracts
  const pad = contracts.find((c: { id: string }) => c.id === 'pad-rock')
  const bulk = contracts.find((c: { id: string }) => c.id === 'bulk-ore')
  const p = { done: { 'pad-rock': true }, xp: { trade: 40 }, levels: { trade: 1 }, jobs: { 'pad-rock': 1 } }
  assert.equal(handInProblem('pad-rock', ship({ cargo: { dust: 0, rock: 3, iron: 0 } }), p, 32, pad), '')
  assert.match(handInProblem('pad-rock', ship({ cargo: { dust: 0, rock: 1, iron: 0 } }), p, 32, pad), /rock/)
  assert.match(handInProblem('bulk-ore', ship({ cargo: { dust: 0, rock: 0, iron: 0, ore: 40 } }), p, 58, bulk), /Trade level 2/)
  assert.equal(handInProblem('bulk-ore', ship({ cargo: { dust: 0, rock: 0, iron: 0, ore: 40 } }), { ...p, levels: { trade: 2 } }, 58, bulk), '')
})

test('each piloting level past the first adds a tenth to thrust', () => {
  assert.equal(thrustFor(undefined), 1)
  assert.ok(Math.abs(thrustFor({ piloting: 3 }) - 1.2) < 1e-9)
})
