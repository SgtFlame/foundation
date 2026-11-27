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

test('an open spaceport tower charges its pads three times as fast (V39)', async () => {
  const { portOf, portPads } = await import('./Pad.ts')
  const mk = (id: string, design: string, x: number, extra = {}) => ({ id, world: 'w', design, blocks: 1, placed: [0], x, y: 0, z: 0, q: [0, 0, 0, 1] as [number, number, number, number], v: [0, 0, 0] as [number, number, number], pilot: '', crew: [], cargo: { dust: 0, rock: 0 }, charge: 1, ...extra })
  const ships = [mk('t', 'tower', 0, { port: ['p1'] }), mk('p1', 'pad', 10), mk('p2', 'pad', 30), mk('p3', 'pad', 60), mk('p4', 'pad', 5, { placed: [] })]
  assert.equal(portOf(ships[1] as never, ships as never)?.id, 't')
  assert.equal(portOf(ships[2] as never, ships as never), null)
  assert.deepEqual(portPads({ x: 0, y: 0, z: 0 }, ships as never).map((s) => s.id), ['p1', 'p2'])
  assert.equal(padCharge(0.5, 2, 3), 0.8)
})
