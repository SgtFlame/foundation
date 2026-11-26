import assert from 'node:assert/strict'
import { test } from 'node:test'
import { nearestSalvage, wreckSites } from './Wreck.ts'

test('two wrecks, the same for every player, a walk from the top', () => {
  const a = wreckSites(7)
  assert.deepEqual(a, wreckSites(7))
  assert.deepEqual(a.map((w) => w.id), ['k0', 'k1'])
  for (const w of a) {
    assert.ok(Math.abs(Math.hypot(w.dir.x, w.dir.y, w.dir.z) - 1) < 1e-9)
    const polar = (Math.acos(w.dir.y) * 180) / Math.PI
    assert.ok(polar >= 30 && polar <= 45, `polar ${polar}`)
  }
  assert.notDeepEqual(wreckSites(8)[0].dir, a[0].dir)
})

test('the nearest block still there, within reach', () => {
  const w = [{ id: 'k0', blocks: [{ x: 0, y: 0, z: 0 }, { x: 1, y: 0, z: 0 }, null] }]
  assert.deepEqual(nearestSalvage(w, {}, { x: 1.2, y: 0, z: 0 })?.index, 1)
  assert.deepEqual(nearestSalvage(w, { 'k0.1': true }, { x: 1.2, y: 0, z: 0 })?.index, 0)
  assert.equal(nearestSalvage(w, {}, { x: 9, y: 0, z: 0 }), null)
})
