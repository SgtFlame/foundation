import assert from 'node:assert/strict'
import { test } from 'node:test'
import { edgePoint, senseCues, spread, type Source } from './Senses.ts'

const barn: Source = { id: 'hollowed', x: 30, z: 0, sense: 'smell', text: 'Burnt sugar and old iron', from: 'from the barn', range: 25 }
const herb: Source = { id: 'thyme', x: 0, z: -10, sense: 'smell', text: 'Thyme on the wind', from: 'by the trees', range: 15 }

test('a sense carries only so far, and focus carries it further', () => {
  assert.deepEqual(senseCues(0, 0, [barn], false), [])
  const c = senseCues(0, 0, [barn], true)
  assert.equal(c.length, 1)
  assert.match(c[0].text, /from the barn/)
  assert.ok(Math.abs(c[0].angle) < 1e-9) // toward +x
})

test('strongest first, and nothing at hand', () => {
  const c = senseCues(5, 0, [barn, herb], false)
  assert.deepEqual(c.map((x) => x.id), ['thyme', 'hollowed'])
  assert.ok(c[0].strength > c[1].strength)
  assert.deepEqual(senseCues(0, -9, [herb], false), [])
})

test('cues sit on the screen edge toward their source', () => {
  assert.deepEqual(edgePoint(1, 0, 1000, 600), { x: 880, y: 300 })
  assert.deepEqual(edgePoint(0, -1, 1000, 600), { x: 500, y: 60 })
  assert.deepEqual(edgePoint(0, 1, 1000, 600), { x: 500, y: 470 })
})

test('cues that would overlap move apart', () => {
  const [a, b] = spread([{ x: 500, y: 470 }, { x: 520, y: 470 }], 600)
  assert.deepEqual(a, { x: 500, y: 470 })
  assert.equal(b.y, 436)
})
