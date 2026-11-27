import assert from 'node:assert/strict'
import { test } from 'node:test'
import { PLANET } from '../voxel/Bodies.ts'
import { canJump, JUMP_HEIGHT, jumpTarget } from './Jump.ts'

test('a jump goes to the other body, over its top (V27)', () => {
  const out = jumpTarget({ x: 0, y: 130, z: 0 }, 100)
  assert.equal(out.to, 'planet')
  assert.equal(out.at.y, PLANET.radius + JUMP_HEIGHT)
  assert.ok(Math.abs(out.at.x - PLANET.x) < 10)
  const back = jumpTarget(out.at, 100)
  assert.equal(back.to, 'moon')
  assert.equal(back.at.y, 100 + JUMP_HEIGHT)
})

test('a jump needs a drive and half a battery (V27)', () => {
  assert.equal(canJump({}, 1).ok, false)
  assert.equal(canJump({ jump: true }, 0.4).ok, false)
  assert.deepEqual(canJump({ jump: true }, 0.5), { ok: true })
})
