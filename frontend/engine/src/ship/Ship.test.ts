import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { axisAngle, IDLE_HELM, lookQuat, rotate, ShipBody, type ShipDesign, type ShipTerrain } from './Ship.ts'

const skiff = JSON.parse(readFileSync(new URL('../../../../backend/features/game/designs/skiff.json', import.meta.url), 'utf8')) as ShipDesign
const all = skiff.blocks.map((_, i) => i)

// A flat moon-like plane: solid below y = 0 (density is the height above it), pulled down at 1.62 m/s²
const flat: ShipTerrain = {
  density: (_x, y) => y,
  gravity: (_x, _y, _z, out) => Object.assign(out, { x: 0, y: -1.62, z: 0 }),
}
const run = (b: ShipBody, s: number, helm = IDLE_HELM) => {
  for (let t = 0; t < s; t += 1 / 60) b.step(1 / 60, helm)
}

test('quaternions: lookQuat faces forward with y up, rotate agrees', () => {
  const q = lookQuat({ x: 1, y: 0, z: 0 }, { x: 0, y: 1, z: 0 })
  const f = rotate(q, { x: 0, y: 0, z: -1 })
  assert.ok(Math.abs(f.x - 1) < 1e-6 && Math.abs(f.z) < 1e-6)
  const u = rotate(axisAngle({ x: 0, y: 0, z: 1 }, Math.PI / 2), { x: 1, y: 0, z: 0 })
  assert.ok(Math.abs(u.y - 1) < 1e-6)
})

test('a finished skiff hovers where it is left, against the pull', () => {
  const b = new ShipBody(skiff, all, { x: 0, y: 10, z: 0, q: [0, 0, 0, 1] }, flat)
  assert.ok(b.reach().y[1] > 1.62, 'its lift beats the moon')
  run(b, 5)
  assert.ok(Math.abs(b.y - 10) < 0.5, `holds its height: ${b.y}`)
})

test('with no lift thrusters it falls and rests on the ground', () => {
  const noLift = all.filter((i) => skiff.blocks[i].push !== 'up')
  const b = new ShipBody(skiff, noLift, { x: 0, y: 6, z: 0, q: [0, 0, 0, 1] }, flat)
  run(b, 6)
  assert.ok(b.grounded)
  // its lowest blocks (y = -1 thrusters are gone; the floor is y = 0) sit on the ground
  assert.ok(b.y > 0.3 && b.y < 1, `rests at ${b.y}`)
})

test('forward flies along its nose, and without a gyroscope it cannot turn', () => {
  const b = new ShipBody(skiff, all, { x: 0, y: 10, z: 0, q: [0, 0, 0, 1] }, flat)
  run(b, 2, { ...IDLE_HELM, forward: 1 })
  assert.ok(b.z < -2, `moved forward: ${b.z}`)
  const noGyro = all.filter((i) => skiff.blocks[i].type !== 'gyro')
  const c = new ShipBody(skiff, noGyro, { x: 0, y: 10, z: 0, q: [0, 0, 0, 1] }, flat)
  run(c, 1, { ...IDLE_HELM, yaw: 1 })
  assert.deepEqual(c.q, [0, 0, 0, 1])
  const d = new ShipBody(skiff, all, { x: 0, y: 10, z: 0, q: [0, 0, 0, 1] }, flat)
  run(d, 1, { ...IDLE_HELM, yaw: 1 })
  assert.notDeepEqual(d.q, [0, 0, 0, 1])
})

test('the dampeners stop it when the helm is let go', () => {
  const b = new ShipBody(skiff, all, { x: 0, y: 10, z: 0, q: [0, 0, 0, 1], v: [0, 0, -8] }, flat)
  run(b, 6)
  assert.ok(Math.hypot(b.v.x, b.v.y, b.v.z) < 0.2)
})

const prospector = JSON.parse(readFileSync(new URL('../../../../backend/features/game/designs/prospector.json', import.meta.url), 'utf8')) as ShipDesign
const every = prospector.blocks.map((_, i) => i)

test('a prospector hovers on its battery, which drains, and falls once it is flat', () => {
  const b = new ShipBody(prospector, every, { x: 0, y: 10, z: 0, q: [0, 0, 0, 1], charge: 0.05 }, flat)
  assert.ok(b.powered && b.store > 0 && b.solar > 0)
  run(b, 2)
  assert.ok(b.charge < 0.05, `drains: ${b.charge}`)
  run(b, 30)
  assert.ok(b.charge < 1e-6, `flat: ${b.charge}`)
  assert.ok(b.power < 1, 'the panels alone cannot hold it up')
  run(b, 20)
  assert.ok(b.grounded, `falls: y ${b.y}`)
})

test('without a battery a prospector gets only what its panels give; parked, the panels charge it', () => {
  const noBattery = every.filter((i) => prospector.blocks[i].type !== 'battery')
  const b = new ShipBody(prospector, noBattery, { x: 0, y: 10, z: 0, q: [0, 0, 0, 1] }, flat)
  assert.equal(b.store, 0)
  run(b, 1)
  assert.ok(b.power < 1)
  const c = new ShipBody(prospector, every, { x: 0, y: 0.6, z: 0, q: [0, 0, 0, 1], charge: 0.2 }, flat)
  c.recharge(60)
  assert.ok(Math.abs(c.charge - (0.2 + (c.solar * 60) / c.store)) < 1e-9)
  // the skiff has no power blocks and does not count power
  assert.equal(new ShipBody(skiff, all, { x: 0, y: 10, z: 0, q: [0, 0, 0, 1] }, flat).powered, false)
})
