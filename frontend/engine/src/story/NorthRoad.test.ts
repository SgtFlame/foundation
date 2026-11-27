import { test } from 'node:test'
import assert from 'node:assert/strict'
import { northRoadReady, waymarkAt, waymarkSpots, WAYMARKS } from './NorthRoad.ts'

test('the north road opens once the hearth-watch has met (S32)', () => {
  assert.equal(northRoadReady({ told: true }), false)
  assert.equal(northRoadReady({ council: true }), true)
  assert.equal(northRoadReady({ council: true, northroad: true }), false)
})

test('the waymarks run north from the furrows to the ford', () => {
  const s = waymarkSpots(30, 20, 2)
  assert.equal(s.length, WAYMARKS.length)
  assert.ok(s.every((w, i) => i === 0 || w.z < s[i - 1].z))
  assert.equal(s.at(-1)!.id, 'ford')
  assert.equal(waymarkAt(s, { x: s[1].x + 1, z: s[1].z })?.id, 'post')
  assert.equal(waymarkAt(s, { x: 0, z: 0 }), null)
  // no step on the road leads underground: the lore keeps them on the road
  assert.ok(WAYMARKS.every((w) => !/under|cave|tunnel|dungeon/i.test(w.text)))
})
