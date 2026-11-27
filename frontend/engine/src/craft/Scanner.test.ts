import assert from 'node:assert/strict'
import { test } from 'node:test'
import { scan, scanLine } from './Scanner.ts'

test('a pulse gathers ore into pockets, nearest first', () => {
  // two ore balls: one 10 m east, one 25 m north
  const ball = (cx: number, cy: number, cz: number, r: number) => (x: number, y: number, z: number) => (x - cx) ** 2 + (y - cy) ** 2 + (z - cz) ** 2 < r * r
  const a = ball(10, 0, 0, 3)
  const b = ball(0, 0, 25, 3)
  const found = scan((x, y, z) => a(x, y, z) || b(x, y, z), { x: 0, y: 0, z: 0 })
  assert.ok(found.length >= 2)
  assert.ok(Math.abs(found[0].x - 10) < 4 && found[0].dist < 14)
  assert.ok(found.some((p) => Math.abs(p.z - 25) < 4))
  assert.ok(found.every((p, i) => i === 0 || p.dist >= found[i - 1].dist))
  assert.match(scanLine(found), /nearest \d+ m/)
  assert.equal(scan(() => false, { x: 0, y: 0, z: 0 }).length, 0)
  assert.match(scanLine([]), /no ore/)
})
