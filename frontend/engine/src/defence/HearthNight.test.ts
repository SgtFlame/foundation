import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { AreaDef } from '../area/PieceSet.ts'
import { HearthNight, hearthOf, WATCHER_HP, type NightEvent } from './HearthNight.ts'

const kitchen = (barrel: boolean, watcher = true): AreaDef => ({
  id: 'kitchen',
  name: 'Kitchen',
  pieceSet: 'kitchen',
  width: 6,
  depth: 5,
  version: 1,
  pieces: {
    h: { id: 'h', piece: 'hearth', x: 2, z: 0, rot: 0 },
    b: { id: 'b', piece: 'barrel', x: barrel ? 2 : 0, z: barrel ? 1 : 4, rot: 0 },
  },
  npcs: watcher ? { w: { id: 'w', name: 'Watcher', kind: 'watcher', x: 3, z: 6 } } : {},
})

const run = (n: HearthNight, until: number, present = () => false) => {
  const out: (NightEvent & { at: number })[] = []
  while (!n.outcome && n.time < until) for (const e of n.step(0.1, present())) out.push({ ...e, at: n.time })
  return out
}

test('the hearthstone is the cell in front of the hearth, and a barrel there counts', () => {
  assert.deepEqual(hearthOf(kitchen(false), 2), { stone: { x: 6, z: 3 }, barrel: false })
  assert.equal(hearthOf(kitchen(true), 2)?.barrel, true)
})

test('left alone, the watcher whistles one, two, three, and the family is taken', () => {
  const n = new HearthNight(kitchen(false), { seed: 3 })
  const ev = run(n, 999)
  const blasts = ev.filter((e) => e.kind === 'whistle').map((e) => (e as { blasts: number }).blasts)
  assert.deepEqual(blasts, [1, 2, 3])
  assert.equal(n.outcome, 'taken')
  assert.ok(ev.some((e) => e.kind === 'through'))
})

test('a barrel on the hearthstone holds it down longer', () => {
  const through = (barrel: boolean) => run(new HearthNight(kitchen(barrel), { seed: 3 }), 999).find((e) => e.kind === 'through')!.at
  assert.ok(through(true) - through(false) > 20)
})

test('while the character is in the house the watcher is not clawed, and dawn finds the house held', () => {
  const n = new HearthNight(kitchen(false), { seed: 5, length: 120 })
  run(n, 999, () => n.inside.size > 0)
  assert.equal(n.watcherHp, WATCHER_HP)
  assert.equal(n.outcome, 'held')
})

test('slaying what came through leaves the house quiet once none are left to come', () => {
  const n = new HearthNight(kitchen(false), { seed: 5, raiders: 2 })
  for (const e of run(n, 120, () => true)) if (e.kind === 'through') n.slay(e.foe.id)
  assert.ok(n.quietForGood)
})
