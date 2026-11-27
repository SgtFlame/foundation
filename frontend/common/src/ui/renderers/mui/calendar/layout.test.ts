// node --test src/**/*.test.ts (yarn test in frontend/common)
import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  at,
  clock,
  inStrip,
  instantOf,
  itemsFrom,
  itemsOn,
  layoutDay,
  layoutStrip,
  moved,
  movedDays,
  partsIn,
  snap,
  step,
  visibleDays,
  visibleRange,
  type Item,
} from './layout.ts'

const item = (id: string, start: string, end: string, more: Partial<Item> = {}): Item => ({ id, title: id, start, end, ...more })

test('wall clock in a zone, with the gap and the overlap', () => {
  assert.deepEqual(partsIn(Date.parse('2026-07-01T08:00:00Z'), 'Europe/London'), { y: 2026, m: 7, d: 1, h: 9, mi: 0 })
  // Spring gap: 01:30 on 29 March 2026 does not exist in London; it moves forward to 02:30 BST (01:30Z)
  assert.equal(new Date(instantOf({ y: 2026, m: 3, d: 29, h: 1, mi: 30 }, 'Europe/London')).toISOString(), '2026-03-29T01:30:00.000Z')
  // Autumn overlap: 01:30 on 25 October 2026 happens twice; the earlier is BST (00:30Z)
  assert.equal(new Date(instantOf({ y: 2026, m: 10, d: 25, h: 1, mi: 30 }, 'Europe/London')).toISOString(), '2026-10-25T00:30:00.000Z')
  assert.equal(new Date(instantOf({ y: 2026, m: 12, d: 1, h: 9, mi: 0 }, 'America/New_York')).toISOString(), '2026-12-01T14:00:00.000Z')
  assert.equal(clock(Date.parse('2026-12-01T14:00:00Z'), 'America/New_York'), '09:00')
})

test('the days and range of each view', () => {
  assert.deepEqual(visibleDays('workWeek', '2026-12-03'), ['2026-11-30', '2026-12-01', '2026-12-02', '2026-12-03', '2026-12-04'])
  assert.equal(visibleDays('week', '2026-12-06')[0], '2026-11-30')
  const month = visibleDays('month', '2026-12-15')
  assert.equal(month.length, 42)
  assert.equal(month[0], '2026-11-30')
  assert.deepEqual(visibleRange('day', '2026-07-01', 'Europe/London'), { from: '2026-06-30T23:00:00.000Z', to: '2026-07-01T23:00:00.000Z' })
  assert.equal(step('month', '2026-01-31', 1), '2026-02-28')
  assert.equal(step('week', '2026-12-01', -1), '2026-11-24')
})

test('overlapping items share their cluster, others take the whole width', () => {
  const items = [
    item('a', '2026-12-01T09:00:00Z', '2026-12-01T10:00:00Z'),
    item('b', '2026-12-01T09:30:00Z', '2026-12-01T10:30:00Z'),
    item('c', '2026-12-01T10:00:00Z', '2026-12-01T11:00:00Z'),
    item('d', '2026-12-01T13:00:00Z', '2026-12-01T14:00:00Z'),
  ]
  const p = Object.fromEntries(layoutDay(items, '2026-12-01', 'UTC').map((x) => [x.id, x]))
  assert.equal(p.a.cols, 2)
  assert.equal(p.a.col, 0)
  assert.equal(p.b.col, 1)
  assert.equal(p.c.col, 0) // a has ended at 10:00
  assert.equal(p.c.cols, 2)
  assert.deepEqual([p.d.col, p.d.cols, p.d.top, p.d.bottom], [0, 1, 780, 840])
})

test('the strip: all-day and multi-day items in lanes', () => {
  const days = visibleDays('week', '2026-12-01')
  const items = [
    item('trip', '2026-11-28', '2026-12-03', { allDay: true }),
    item('holiday', '2026-12-02', '2026-12-03', { allDay: true }),
    item('overnight', '2026-12-04T22:00:00Z', '2026-12-05T02:00:00Z'),
  ]
  assert.equal(inStrip(items[2], 'UTC'), true)
  assert.equal(inStrip(item('x', '2026-12-04T09:00:00Z', '2026-12-04T10:00:00Z'), 'UTC'), false)
  const l = Object.fromEntries(layoutStrip(items, days, 'UTC').map((x) => [x.id, x]))
  assert.deepEqual([l.trip.first, l.trip.last, l.trip.lane, l.trip.clippedStart], [0, 2, 0, true])
  assert.equal(l.holiday.lane, 1) // the trip's last day is Wednesday too
  assert.deepEqual([l.overnight.first, l.overnight.last], [4, 5])
  assert.deepEqual(
    itemsOn(items, '2026-12-02', 'UTC').map((i) => i.id),
    ['trip', 'holiday'],
  )
})

test('snapping and moving keep an item in the zone', () => {
  assert.equal(snap(52, 15), 45)
  assert.equal(snap(53, 15), 60)
  const standup = item('s', '2026-10-23T08:00:00Z', '2026-10-23T08:15:00Z') // 09:00 BST
  // Moved to 09:00 London on the Monday after the clocks change: 09:00Z
  assert.deepEqual(moved(standup, '2026-10-26', 9 * 60, 'Europe/London'), { start: '2026-10-26T09:00:00Z', end: '2026-10-26T09:15:00Z' })
  assert.deepEqual(movedDays(standup, 3, 'Europe/London'), { start: '2026-10-26T09:00:00Z', end: '2026-10-26T09:15:00Z' })
  assert.deepEqual(movedDays(item('h', '2026-12-02', '2026-12-03', { allDay: true }), 2, 'UTC'), { start: '2026-12-04', end: '2026-12-05' })
  assert.equal(at('2026-12-01', 600, 'Europe/London'), '2026-12-01T10:00:00Z')
})

test('items from props drop what is malformed', () => {
  const got = itemsFrom([{ id: 1, title: 'x', start: '2026-12-01' }, { title: 'no id', start: 'x' }, null, { id: 'b' }])
  assert.equal(got.length, 1)
  assert.equal(got[0].id, '1')
  assert.equal(got[0].allDay, true)
})
