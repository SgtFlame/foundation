// node --test src/**/*.test.ts (yarn test in frontend/common)
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { ago, countOf, fresh, type Notification } from './notificationText.ts'

test('only notifications new since the last look are fresh', () => {
  const a: Notification = { _id: 'a', created: '2026-11-27T10:00:00Z' }
  const b: Notification = { _id: 'b', created: '2026-11-27T11:00:00Z' }
  const c: Notification = { _id: 'c', created: '2026-11-27T10:30:00Z' }
  const first = fresh([a], undefined)
  assert.deepEqual(first.fresh, []) // what was waiting at open is not toasted
  const second = fresh([b, c, a], first.seen)
  assert.deepEqual(second.fresh.map((n) => n._id), ['c', 'b'])
  assert.deepEqual(fresh([b, c, a], second.seen).fresh, [])
  assert.deepEqual(fresh([{ _id: 'd', read: true }], second.seen).fresh, [])
})

test('times are said shortly', () => {
  const now = Date.parse('2026-11-27T12:00:00Z')
  assert.equal(ago('2026-11-27T11:59:30Z', now), 'now')
  assert.equal(ago('2026-11-27T11:55:00Z', now), '5m')
  assert.equal(ago('2026-11-27T09:00:00Z', now), '3h')
  assert.equal(ago('2026-11-25T12:00:00Z', now), '2d')
  assert.equal(ago(undefined, now), '')
})

test('the count is read from the aggregate', () => {
  assert.equal(countOf({ groups: [{ key: null, values: { count: 3 } }] }), 3)
  assert.equal(countOf({ groups: [] }), 0)
  assert.equal(countOf(undefined), 0)
})
