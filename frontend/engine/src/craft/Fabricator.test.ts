import assert from 'node:assert/strict'
import { test } from 'node:test'
import { FAB_MS, fabAction } from './Fabricator.ts'

test('the fabricator needs power, then iron and ore, then time', () => {
  assert.deepEqual(fabAction(null, { iron: 1, ore: 1 }, false, 0), { kind: 'none', why: 'no power: place the solar panel on the roof' })
  assert.equal(fabAction(null, { iron: 0.4, ore: 1 }, true, 0).kind, 'none')
  assert.deepEqual(fabAction(null, { iron: 0.5, ore: 0.25 }, true, 0), { kind: 'make' })
  assert.deepEqual(fabAction({ item: 'circuit', at: 1000 }, null, true, 1000 + FAB_MS - 2500), { kind: 'wait', seconds: 3 })
  assert.deepEqual(fabAction({ item: 'circuit', at: 1000 }, null, true, 1000 + FAB_MS), { kind: 'take' })
  assert.equal(fabAction({ item: 'circuit', at: 0 }, { circuits: 20 }, true, FAB_MS).kind, 'none')
})
