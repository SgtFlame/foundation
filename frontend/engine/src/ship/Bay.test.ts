import assert from 'node:assert/strict'
import { test } from 'node:test'
import { bayNear, clamped, needsBay } from './Bay.ts'
import { IDLE_HELM } from './Ship.ts'

const bay = { id: 'b', world: 'w', design: 'bay', blocks: 39, placed: Array.from({ length: 39 }, (_, i) => i), x: 10, y: 0, z: 0, q: [0, 0, 0, 1], v: [0, 0, 0], pilot: '', seq: 1 } as any
const half = { ...bay, id: 'h', x: 0, placed: [0, 1] }

test('chip ships need a finished bay floor near (V34)', () => {
  assert.equal(needsBay('cutter'), true)
  assert.equal(needsBay('prospector'), false)
  assert.equal(bayNear({ x: 12, y: 0, z: 0 }, [bay, half])?.id, 'b')
  assert.equal(bayNear({ x: 0, y: 0, z: 0 }, [half]), null)
  assert.equal(bayNear({ x: 40, y: 0, z: 0 }, [bay]), null)
})

test('the clamp holds a resting ship with its helm idle (V34)', () => {
  assert.equal(clamped({ x: 10, y: 2.5, z: 0 }, 0.1, IDLE_HELM, [bay])?.id, 'b')
  assert.equal(clamped({ x: 10, y: 2.5, z: 0 }, 0.1, { ...IDLE_HELM, up: 1 }, [bay]), null)
  assert.equal(clamped({ x: 10, y: 2.5, z: 0 }, 3, IDLE_HELM, [bay]), null)
})
