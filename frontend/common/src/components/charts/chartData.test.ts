// node --test src/**/*.test.ts (yarn test in frontend/common)
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { foldOther, formatValue, otherKey, seriesFrom, slicesOf, tableOf, xsOf, type Series } from './chartData.ts'

test('rows from records.aggregate become one series by dotted paths', () => {
  const groups = [
    { key: { stage: 'lead' }, values: { amount: '1200.50' } },
    { key: { stage: 'won' }, values: { amount: 300 } },
    { key: { stage: 'lead' }, values: { amount: 100 } },
  ]
  const s = seriesFrom({ rows: { groups }, xField: 'key.stage', yField: 'values.amount', labels: { lead: 'Lead' } })
  assert.equal(s.length, 1)
  assert.equal(s[0].label, 'amount')
  assert.deepEqual(s[0].points, [
    { x: 'lead', y: 1300.5 },
    { x: 'won', y: 300 },
  ])
})

test('a series field splits rows into labelled series; expanded records read by id', () => {
  const rows = [
    { key: { stage: { _id: 's1', _title: 'Lead' }, owner: '7' }, values: { n: 2 } },
    { key: { stage: { _id: 's2' }, owner: '8' }, values: { n: 1 } },
    { key: { stage: { _id: 's1' }, owner: '8' }, values: { n: 4 } },
  ]
  const s = seriesFrom({ rows, xField: 'key.stage', yField: 'values.n', seriesField: 'key.owner', labels: { '7': 'Ada' } })
  assert.deepEqual(s.map((x) => [x.key, x.label]), [['7', 'Ada'], ['8', '8']])
  assert.deepEqual(xsOf(s, ['s2', 'missing', 's1']), ['s2', 's1'])
  assert.deepEqual(xsOf(s, ['s2', 'missing', 's1'], true), ['s2', 'missing', 's1'])
  assert.deepEqual(tableOf(s, ['s1', 's2']), [
    { x: 's1', s0: 2, s1: 4 },
    { x: 's2', s0: 0, s1: 1 },
  ])
})

test('the series prop is taken as given', () => {
  const s = seriesFrom({ series: [{ key: 'a.b', label: 'A', points: [{ x: 1, y: 2 }] }] })
  assert.deepEqual(s, [{ key: 'a.b', label: 'A', points: [{ x: '1', y: 2 }] }])
})

test('more than seven series fold the smallest into Other', () => {
  const series: Series[] = Array.from({ length: 10 }, (_, i) => ({ key: `k${i}`, label: `K${i}`, points: [{ x: 'a', y: i + 1 }] }))
  const f = foldOther(series)
  assert.equal(f.length, 8)
  assert.deepEqual(f.slice(0, 7).map((s) => s.key), ['k3', 'k4', 'k5', 'k6', 'k7', 'k8', 'k9'])
  assert.equal(f[7].key, otherKey)
  assert.equal(f[7].other, true)
  assert.deepEqual(f[7].points, [{ x: 'a', y: 1 + 2 + 3 }])
  assert.equal(foldOther(series.slice(0, 7)).length, 7)
})

test('a pie folds small slices; a funnel keeps every stage in order', () => {
  const s: Series[] = [{ key: 'v', label: 'v', points: Array.from({ length: 9 }, (_, i) => ({ x: `x${i}`, y: 9 - i })) }]
  const xs = xsOf(s)
  const pie = slicesOf(s, xs, 'pie')
  assert.equal(pie.length, 8)
  assert.deepEqual(pie[7], { x: otherKey, y: 2 + 1, other: true })
  assert.equal(slicesOf(s, xs, 'funnel').length, 9)
})

test('formats', () => {
  assert.equal(formatValue(1234.5, 'number', 'USD', 'en-US'), '1,234.5')
  assert.equal(formatValue(1234.5, 'money', 'USD', 'en-US'), '$1,235')
  assert.equal(formatValue(12.5, 'money', 'EUR', 'en-US'), '€12.50')
  assert.equal(formatValue(0.255, 'percent', 'USD', 'en-US'), '25.5%')
  assert.equal(formatValue(3725, 'duration'), '1h 2m')
  assert.equal(formatValue(90000, 'duration'), '1d 1h')
  assert.equal(formatValue(60, 'duration'), '1m')
})

test('the change from a compare value', async () => {
  const { deltaOf } = await import('./chartData.ts')
  assert.deepEqual(deltaOf('150.00', 100), { fraction: 0.5, direction: 'up' })
  assert.deepEqual(deltaOf(50, 100), { fraction: -0.5, direction: 'down' })
  assert.deepEqual(deltaOf(5, 0), { fraction: null, direction: 'up' })
  assert.equal(deltaOf(5, undefined), null)
})
