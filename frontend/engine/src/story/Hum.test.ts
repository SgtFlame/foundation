import assert from 'node:assert/strict'
import { test } from 'node:test'
import { BODY_START, breathe, cook, DISHES, humPool, spendHum, stepBody } from './Hum.ts'
import { HERBS } from './FirstHourStory.ts'

test('the sky sets the pool: small in the greater moon\'s dark, larger when it is full (S8)', () => {
  const pools = Array.from({ length: 30 }, (_, n) => humPool(n))
  assert.equal(humPool(0), 1) // greater new
  assert.ok(Math.max(...pools) >= 4 && Math.max(...pools) <= 5)
  assert.ok(pools.every((p) => p >= 1))
})

test('stillness fills the Hum a breath at a time, up to the pool; spending it chills and starves', () => {
  let b = BODY_START
  for (let i = 0; i < 6; i++) b = breathe(b, 3)
  assert.equal(b.hum, 3)
  const s = spendHum(b)
  assert.equal(s.hum, 2)
  assert.ok(s.chill >= 40 && s.hunger > b.hunger)
  assert.deepEqual(spendHum({ ...b, hum: 0 }), { ...b, hum: 0 })
  // chilled, hunger grows faster
  const warm = stepBody({ hum: 0, chill: 0, hunger: 20 }, 60)
  const cold = stepBody({ hum: 0, chill: 100, hunger: 20 }, 60)
  assert.ok(cold.hunger - 20 > 2 * (warm.hunger - 20))
  assert.ok(cold.chill < 100)
})

test('hot food clears it: thyme milk the Chill, nettle soup the Hunger, from the herbal\'s herbs', () => {
  const b = { hum: 1, chill: 80, hunger: 70 }
  const milk = cook(b, DISHES.find((d) => d.id === 'thymeMilk')!)
  assert.equal(milk.chill, 0)
  const soup = cook(b, DISHES.find((d) => d.id === 'nettleSoup')!)
  assert.ok(soup.hunger <= 10)
  for (const d of DISHES) assert.ok(HERBS.some((h) => h.id === d.herb), d.herb)
})
