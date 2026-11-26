import assert from 'node:assert/strict'
import { test } from 'node:test'
import { available, Fishing, grazerDir, herdOf, RESPAWN_MS } from './Wildlife.ts'
import { PLANET } from '../voxel/Bodies.ts'

test('a seed gives the same herd, and each grazer roams near its home (V12)', () => {
  const a = herdOf(7)
  assert.equal(a.length, 12)
  assert.deepEqual(a, herdOf(7))
  assert.notDeepEqual(a[0].home, herdOf(8)[0].home)
  for (const g of a) {
    for (const t of [0, 13, 600]) {
      const d = grazerDir(g, t)
      assert.ok(Math.abs(Math.hypot(d.x, d.y, d.z) - 1) < 1e-9)
      const metres = Math.acos(Math.min(1, d.x * g.home.x + d.y * g.home.y + d.z * g.home.z)) * PLANET.radius
      assert.ok(metres <= g.roam + 0.01, `${g.id} ${metres}`)
    }
  }
  assert.ok(available(undefined, 0) && !available(1000, 1000 + RESPAWN_MS - 1) && available(1000, 1000 + RESPAWN_MS))
})

test('a fish bites after a wait and is caught only if struck in the window', () => {
  const f = new Fishing(3)
  f.cast()
  let ev = null
  let t = 0
  while (!ev && t < 10) {
    ev = f.step(0.1)
    t += 0.1
  }
  assert.equal(ev, 'bite')
  assert.ok(t >= 3)
  assert.equal(f.strike(), 'caught')
  f.cast()
  assert.equal(f.strike(), 'missed')
  f.cast()
  for (let i = 0; i < 200 && f.state !== 'idle'; i++) ev = f.step(0.1)
  assert.equal(ev, 'escaped')
})
