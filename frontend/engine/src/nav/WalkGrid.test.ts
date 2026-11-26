import assert from 'node:assert/strict'
import { test } from 'node:test'
import { WalkGrid } from './WalkGrid.ts'

test('a path goes round a wall and is straightened', () => {
  const g = new WalkGrid(40, 40)
  g.block(18, 0, 4, 30, 0) // a wall from the north edge, open at the south
  const p = g.findPath({ x: 5, z: 5 }, { x: 35, z: 5 })
  assert.ok(p, 'a path')
  for (let i = 1; i < p.length; i++) assert.ok(g.clear(p[i - 1], p[i]), `leg ${i} is clear`)
  assert.ok(p.some((q) => q.z >= 30), 'it goes round the wall')
  assert.ok(p.length <= 5, `straightened to ${p.length} points`)
  assert.deepEqual(p[p.length - 1], { x: 35, z: 5 })
})

test('no path when walled in, and a blocked goal moves to the nearest walkable cell', () => {
  const g = new WalkGrid(20, 20)
  g.block(8, 0, 1, 20, 0)
  assert.equal(g.findPath({ x: 2, z: 2 }, { x: 15, z: 2 }), null)
  const h = new WalkGrid(20, 20)
  h.block(10, 10, 3, 3, 0)
  const p = h.findPath({ x: 2, z: 2 }, { x: 11.5, z: 11.5 })
  assert.ok(p)
  const end = p[p.length - 1]
  assert.ok(h.walkable(...h.cellOf(end)))
})
