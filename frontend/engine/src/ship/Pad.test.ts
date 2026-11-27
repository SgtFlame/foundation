import { test } from 'node:test'
import assert from 'node:assert/strict'
import { padCharge, padUnder } from './Pad.ts'
import type { ShipState } from './Ship.ts'

const pad = { id: 'p', world: 'w', design: 'pad', blocks: 29, placed: Array.from({ length: 29 }, (_, i) => i), x: 10, y: 0, z: 0, q: [0, 0, 0, 1], v: [0, 0, 0], pilot: '', seq: 1 } as ShipState

test('V33: a ship resting on a finished pad charges', () => {
  assert.equal(padUnder({ x: 10, y: 1.5, z: 1 }, 0.1, [pad])?.id, 'p')
  assert.equal(padUnder({ x: 10, y: 1.5, z: 1 }, 3, [pad]), null) // still flying
  assert.equal(padUnder({ x: 20, y: 1.5, z: 1 }, 0.1, [pad]), null) // off the pad
  assert.equal(padUnder({ x: 10, y: 1.5, z: 1 }, 0.1, [{ ...pad, placed: [0] }]), null) // unfinished
  assert.equal(padCharge(0.5, 2), 0.6)
  assert.equal(padCharge(0.99, 2), 1)
})
