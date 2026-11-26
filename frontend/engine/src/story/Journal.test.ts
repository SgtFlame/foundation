import assert from 'node:assert/strict'
import { test } from 'node:test'
import { moons } from '../sky/Moons.ts'
import { journalEntries, link, nextNoMoon } from './Journal.ts'

test('the notes hold what the story reached', () => {
  assert.deepEqual(journalEntries({ farmer: true }).map((e) => e.id), ['wenlan'])
  assert.deepEqual(journalEntries({ farmer: true, road: true, signature: true, fight: true, almanac: true, reading: true }).map((e) => e.id), ['wenlan', 'hawks-rest', 'signature', 'eyes', 'almanac', 'ash'])
})

test('the next night with no moon', () => {
  const n = nextNoMoon(12)
  assert.ok(n > 12)
  assert.ok(moons(n).greater.lit < 0.1 && moons(n).lesser.lit < 0.3)
  for (let k = 13; k < n; k++) assert.ok(!(moons(k).greater.lit < 0.1 && moons(k).lesser.lit < 0.3))
})

test('the Ash card and the almanac name the night; other pairs do not fit', () => {
  const c = link('ash', 'almanac', 12)
  assert.equal(c?.step, 'foretold')
  assert.match(c!.text, new RegExp(`Day ${nextNoMoon(12) - 11 + 1}\\b`))
  assert.equal(link('almanac', 'ash', 12)?.id, 'foretold')
  assert.equal(link('signature', 'eyes', 12)?.id, 'raiders')
  assert.equal(link('wenlan', 'ash', 12), null)
})
