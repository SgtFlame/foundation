import assert from 'node:assert/strict'
import { test } from 'node:test'
import { WorldGenerator } from '../voxel/Bodies.ts'
import { DEFAULT_MOON } from '../voxel/Types.ts'
import { surfaceBelow, Walker, WALK_SPEED } from './Walker.ts'

const world = new WorldGenerator({ ...DEFAULT_MOON, seed: 7, asteroids: 2 })
const terrain = { density: (x: number, y: number, z: number) => world.density(x, y, z), gravity: world.gravity.bind(world) }
const idle = { forward: 0, right: 0, up: 0, sprint: false }
const run = (w: Walker, seconds: number, intent = idle) => {
  for (let t = 0; t < seconds; t += 1 / 60) w.step(1 / 60, intent)
}

test('a walker dropped on the moon lands and stands still', () => {
  const at = surfaceBelow(terrain, { x: 0, y: 140, z: 0 })!
  const w = new Walker(terrain, { x: at.x, y: at.y + 2, z: at.z })
  run(w, 4)
  assert.equal(w.grounded, true)
  const p = { ...w.pos }
  run(w, 2)
  assert.ok(Math.hypot(w.pos.x - p.x, w.pos.y - p.y, w.pos.z - p.z) < 0.05, 'does not slide')
  assert.ok(world.density(w.pos.x, w.pos.y + 0.35, w.pos.z) > 0.2, 'stands on the ground, not in it')
})

test('walking moves it along the ground at walking pace, and around the moon its up turns', () => {
  const at = surfaceBelow(terrain, { x: 0, y: 140, z: 0 })!
  const w = new Walker(terrain, at)
  run(w, 2)
  const p = { ...w.pos }
  run(w, 5, { ...idle, forward: 1 })
  const moved = Math.hypot(w.pos.x - p.x, w.pos.y - p.y, w.pos.z - p.z)
  assert.ok(moved > WALK_SPEED * 5 * 0.6 && moved < WALK_SPEED * 5 * 1.1, `moved ${moved}`)
  // Walking on the far side: up points away from the centre
  const far = surfaceBelow(terrain, { x: 140, y: 0, z: 0 })!
  const w2 = new Walker(terrain, far)
  run(w2, 2)
  assert.ok(w2.up.x > 0.95, `up ${JSON.stringify(w2.up)}`)
  assert.equal(w2.grounded, true)
})

test('a jump comes back down; the thrust pack lifts it and the dampeners stop it', () => {
  const at = surfaceBelow(terrain, { x: 0, y: 140, z: 0 })!
  const w = new Walker(terrain, at)
  run(w, 2)
  const h0 = Math.hypot(w.pos.x, w.pos.y, w.pos.z)
  w.step(1 / 60, { ...idle, up: 1 })
  run(w, 0.8)
  assert.ok(Math.hypot(w.pos.x, w.pos.y, w.pos.z) > h0 + 1, 'in the air')
  // In the moon's low pull a jump of 3.2 m/s lasts about 4 seconds
  run(w, 7)
  assert.equal(w.grounded, true)
  w.thrustPack = true
  run(w, 3, { ...idle, up: 1 })
  const h1 = Math.hypot(w.pos.x, w.pos.y, w.pos.z)
  assert.ok(h1 > h0 + 15, `thrust lifts: ${h1 - h0}`)
  run(w, 4)
  assert.ok(Math.hypot(w.vel.x, w.vel.y, w.vel.z) < 2, 'dampeners hold it nearly still against the weak pull')
})

test('a walker over an asteroid falls onto it and stands with its feet toward it', () => {
  const rock = world.bodies[1]
  const d = Math.hypot(rock.x, rock.y, rock.z)
  // Over the side facing the moon: the asteroid's pull must win there
  const k = (d - rock.reach - 2) / d
  const w = new Walker(terrain, { x: rock.x * k, y: rock.y * k, z: rock.z * k })
  run(w, 20)
  assert.equal(w.grounded, true)
  const out = { x: w.pos.x - rock.x, y: w.pos.y - rock.y, z: w.pos.z - rock.z }
  const l = Math.hypot(out.x, out.y, out.z)
  assert.ok((w.up.x * out.x + w.up.y * out.y + w.up.z * out.z) / l > 0.8, 'up points away from the asteroid')
})
