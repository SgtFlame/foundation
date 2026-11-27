import assert from 'node:assert/strict'
import { test } from 'node:test'
import { eat, FRESH, hungry, NEEDS, ratesFor, stepNeeds, warning } from './Needs.ts'

const run = (n = FRESH, breathable: boolean, seconds: number) => {
  let s = n
  for (let t = 0; t < seconds; t += 0.5) s = stepNeeds(s, { breathable }, 0.5)
  return s
}

test('a full suit lasts four minutes in vacuum, then health falls fast (V10)', () => {
  const a = run(FRESH, false, NEEDS.airSeconds - 5)
  assert.ok(a.air > 0 && a.health === 100, JSON.stringify(a))
  assert.match(warning(a, { breathable: false })!, /Air low/)
  const b = run(a, false, 25)
  assert.equal(b.air, 0)
  assert.ok(b.health < 100 - 15 * NEEDS.suffocatePerSecond * 0.9, JSON.stringify(b))
  // in air it fills again within seconds, and health comes back slowly
  const c = run(b, true, 5)
  assert.equal(c.air, 100)
  assert.ok(c.health > b.health && c.health < b.health + 5)
})

test('hunger drains over half an hour; a ration answers it, starving hurts slowly', () => {
  const a = run({ ...FRESH, fed: 100 }, true, NEEDS.fedMinutes * 30)
  assert.ok(Math.abs(a.fed - 50) < 1, JSON.stringify(a))
  assert.ok(hungry(a))
  assert.equal(eat(a).fed, a.fed + NEEDS.ration)
  assert.equal(eat({ ...a, fed: 90 }).fed, 100)
  const s = run({ ...FRESH, fed: 0 }, true, 40)
  assert.ok(s.health < 100 && s.health > 85, JSON.stringify(s))
  assert.match(warning(s, { breathable: true })!, /Starving/)
  assert.equal(warning(FRESH, { breathable: false }), null)
})

test('an iron air tank doubles the suit\'s air', () => {
  const plain = stepNeeds(FRESH, { breathable: false }, 60)
  const tank = stepNeeds(FRESH, { breathable: false }, 60, ratesFor({ airTank: true }))
  assert.equal(Math.round(100 - plain.air), 25)
  assert.ok(Math.abs(100 - tank.air - 12.5) < 0.01)
  assert.equal(ratesFor(undefined), NEEDS)
})
