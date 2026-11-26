import assert from 'node:assert/strict'
import { test } from 'node:test'
import { DROP_HEIGHT, DROP_SECONDS, dropHeight } from './DropPod.ts'

test('the pod falls from its height to the ground, slowing at the end', () => {
  assert.equal(dropHeight(0), DROP_HEIGHT)
  assert.equal(dropHeight(DROP_SECONDS), 0)
  const late = dropHeight(DROP_SECONDS - 0.5) - dropHeight(DROP_SECONDS)
  const early = dropHeight(0) - dropHeight(0.5)
  assert.ok(late < early / 10, `brakes: ${early} then ${late}`)
})
