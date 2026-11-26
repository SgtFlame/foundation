import assert from 'node:assert/strict'
import { test } from 'node:test'
import { CARD_EFFECTS, cookWith, dayCard, NO_EFFECT, spendWith } from './DayCard.ts'
import { drawCard, LESSER_DECK } from './FirstHourStory.ts'
import { DISHES } from './Hum.ts'

test('every card of the lesser deck has an effect, and the day decides the card', () => {
  for (const c of LESSER_DECK) assert.ok(CARD_EFFECTS[c.id]?.line)
  assert.equal(dayCard(5, 0), null)
  assert.equal(dayCard(5, 2)?.card.id, drawCard(7).id)
})

test('the Frost chills more for a Hum; the Hearth makes food do more', () => {
  const b = { hum: 3, chill: 10, hunger: 50 }
  assert.equal(spendWith(b, NO_EFFECT).chill, 50)
  assert.equal(spendWith(b, CARD_EFFECTS.frost).chill, 70)
  assert.deepEqual(spendWith({ ...b, hum: 0 }, CARD_EFFECTS.frost), { ...b, hum: 0 })
  const d = DISHES[1]
  const plain = cookWith({ hum: 0, chill: 80, hunger: 80 }, d, NO_EFFECT)
  const warm = cookWith({ hum: 0, chill: 80, hunger: 80 }, d, CARD_EFFECTS.hearth)
  assert.ok(warm.chill < plain.chill && warm.hunger <= plain.hunger)
})

test('days in a row draw varied cards', () => {
  for (const seed of [1, 77, 123456]) {
    const week = new Set(Array.from({ length: 7 }, (_, d) => dayCard(seed, d + 1)!.card.id))
    assert.ok(week.size >= 3, `seed ${seed}: ${[...week]}`)
  }
})
