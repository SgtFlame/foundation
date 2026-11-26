import assert from 'node:assert/strict'
import { test } from 'node:test'
import { almanac, canSleep, CLOCK_START, clockText, isDay, nightOf, skyNote, sleep, stepClock } from './Clock.ts'
import { moons } from '../sky/Moons.ts'

test('the clock starts on the night of the first snow and a game hour is a real minute (S9)', () => {
  assert.equal(nightOf(CLOCK_START), 11)
  assert.ok(!isDay(CLOCK_START))
  const later = stepClock(CLOCK_START, 4 * 60) // four real minutes: one in the morning
  assert.equal(later.day, 1)
  assert.ok(Math.abs(later.hour - 1) < 1e-9)
  assert.equal(nightOf(later), 11) // the small hours are still the first night
  assert.equal(clockText({ day: 1, hour: 14.5 }), 'Day 2 · 14:30')
})

test('sleeping by a hearth wakes at seven the next morning, or the same one in the small hours', () => {
  assert.deepEqual(sleep({ day: 0, hour: 21 }), { day: 1, hour: 7 })
  assert.deepEqual(sleep({ day: 1, hour: 2 }), { day: 1, hour: 7 })
  assert.ok(canSleep({ day: 0, hour: 21 }) && !canSleep({ day: 1, hour: 12 }))
})

test('the almanac lists the coming nights with both moons, the Hum allowed and moondark', () => {
  const rows = almanac(11, 30)
  assert.equal(rows.length, 30)
  assert.equal(rows[0].night, 11)
  const dark = rows.find((r) => moons(r.night).greater.lit < 0.1)!
  assert.match(skyNote(dark.night), /Dark|Moondark/)
  assert.equal(dark.hum, 1)
  assert.ok(rows.some((r) => r.hum >= 4))
  // the Doubling is rare: a night or two in a year of the two cycles
  const year = almanac(0, 390).filter((r) => /Doubling/.test(r.note))
  assert.ok(year.length >= 1 && year.length <= 2, String(year.length))
})
