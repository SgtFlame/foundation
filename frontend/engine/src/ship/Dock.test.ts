import { test } from 'node:test'
import assert from 'node:assert/strict'
import type { ShipState } from './Ship.ts'
import { bayFree, docked, launchPlan, launchTower } from './Dock.ts'

const mk = (id: string, design: string, z: number, more: Partial<ShipState> = {}): ShipState => ({
  id, world: 'w', design, blocks: 2, placed: [0, 1], x: 0, y: 0, z, q: [0, 0, 0, 1], v: [0, 0, 0], pilot: '', seq: 1, ...more,
})

test('a cutter launches from its bay once finished', () => {
  const k = mk('k', 'cutter', 0, { bay: 'b' })
  assert.equal(docked(k), true)
  assert.deepEqual(launchPlan(k, [k]), { ok: true })
  assert.equal(launchPlan({ ...k, placed: [0] }, [k]).ok, false)
  assert.equal(launchPlan(mk('s', 'skiff', 0), []).ok, false)
})

test('a freighter needs an open tower within reach', () => {
  const f = mk('f', 'freighter', 0, { bay: 'b' })
  const shut = mk('t1', 'tower', 10)
  const far = mk('t2', 'tower', 60, { port: ['p'] })
  const open = mk('t3', 'tower', 30, { port: ['p'] })
  assert.equal(launchPlan(f, [f, shut, far]).ok, false)
  assert.equal(launchTower(f, [shut, far, open])?.id, 't3')
  assert.deepEqual(launchPlan(f, [f, shut, far, open]), { ok: true, tower: 't3' })
})

test('a bay floor holds one ship', () => {
  assert.equal(bayFree(mk('b', 'bay', 0)), true)
  assert.equal(bayFree(mk('b', 'bay', 0, { holds: 'k' })), false)
})
