import { test } from 'node:test'
import assert from 'node:assert/strict'
import { atInn, innSpot, kingsCouncilReady, newMoonNight, waitForNewMoon, waitReady } from './NewMoon.ts'
import { FIRST_NIGHT, nightOf } from './Clock.ts'

test('the inn stands at the south edge in the middle (S36)', () => {
  const inn = innSpot(20, 16, 2)
  assert.deepEqual(inn, { x: 20, z: 29 })
  assert.ok(atInn(inn, { x: 21, z: 28 }))
  assert.ok(!atInn(inn, { x: 20, z: 20 }))
})

test('waiting at the inn passes to dusk on the next new-moon night (S36)', () => {
  assert.ok(newMoonNight(30) && newMoonNight(60) && !newMoonNight(31))
  const c = waitForNewMoon({ day: 5, hour: 12 })
  assert.equal(nightOf(c), 30)
  assert.equal(c.hour, 19)
  assert.deepEqual(waitForNewMoon(c), c) // already that night
  // the day of a new-moon night waits only for its evening
  assert.deepEqual(waitForNewMoon({ day: 30 - FIRST_NIGHT, hour: 10 }), { day: 30 - FIRST_NIGHT, hour: 19 })
  // the morning after waits a whole month
  assert.equal(nightOf(waitForNewMoon({ day: 31 - FIRST_NIGHT, hour: 9 })), 60)
})

test('the council sits on the new-moon night once the player waited (S36)', () => {
  assert.ok(waitReady({ steward: true }))
  assert.ok(!waitReady({ steward: true, kingscouncil: true }))
  const night = waitForNewMoon({ day: 5, hour: 12 })
  assert.ok(kingsCouncilReady({ waited: true }, night))
  assert.ok(!kingsCouncilReady({ waited: true }, { day: 5, hour: 12 }))
  assert.ok(!kingsCouncilReady({}, night))
  assert.ok(!kingsCouncilReady({ waited: true, kingscouncil: true }, night))
})
