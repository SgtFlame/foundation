import assert from 'node:assert/strict'
import { test } from 'node:test'
import { DRIVE_MS, FAB_MS, fabAction, KIT_MS } from './Fabricator.ts'

test('the fabricator needs power, then iron and ore, then time', () => {
  assert.deepEqual(fabAction(null, { iron: 1, ore: 1 }, false, 0), { kind: 'none', why: 'no power: place the solar panel on the roof' })
  assert.equal(fabAction(null, { iron: 0.4, ore: 1 }, true, 0).kind, 'none')
  assert.deepEqual(fabAction(null, { iron: 0.5, ore: 0.25 }, true, 0), { kind: 'make' })
  assert.deepEqual(fabAction({ item: 'circuit', at: 1000 }, null, true, 1000 + FAB_MS - 2500), { kind: 'wait', seconds: 3 })
  assert.deepEqual(fabAction({ item: 'circuit', at: 1000 }, null, true, 1000 + FAB_MS), { kind: 'take' })
  assert.equal(fabAction({ item: 'circuit', at: 0 }, { circuits: 20 }, true, FAB_MS).kind, 'none')
})

test('an ion thruster kit takes iron, two boards and a minute', () => {
  assert.equal(fabAction(null, { iron: 1, circuits: 1 }, true, 0, 'ionKit').kind, 'none')
  assert.deepEqual(fabAction(null, { iron: 1, circuits: 2 }, true, 0, 'ionKit'), { kind: 'make' })
  assert.equal(fabAction({ item: 'ionKit', at: 0 }, null, true, FAB_MS).kind, 'wait')
  assert.deepEqual(fabAction({ item: 'ionKit', at: 0 }, { circuits: 20 }, true, KIT_MS), { kind: 'take' })
})

test('a jump drive needs a jump coil, then iron, ore and boards, and takes the longest (V27)', () => {
  assert.match((fabAction(null, { iron: 5, ore: 5, circuits: 5 }, true, 0, 'jumpDrive') as { why: string }).why, /jump coil/)
  assert.equal(fabAction(null, { coils: 1, iron: 1, ore: 0.5, circuits: 2 }, true, 0, 'jumpDrive').kind, 'none')
  assert.deepEqual(fabAction(null, { coils: 1, iron: 1, ore: 1, circuits: 2 }, true, 0, 'jumpDrive'), { kind: 'make' })
  assert.deepEqual(fabAction({ item: 'jumpDrive', at: 0 }, null, true, KIT_MS), { kind: 'wait', seconds: (DRIVE_MS - KIT_MS) / 1000 })
  // a finished drive is taken even with a pack full of boards
  assert.deepEqual(fabAction({ item: 'jumpDrive', at: 0 }, { circuits: 20 }, true, DRIVE_MS), { kind: 'take' })
})
