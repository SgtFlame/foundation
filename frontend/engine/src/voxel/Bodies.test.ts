import assert from 'node:assert/strict'
import { test } from 'node:test'
import { asteroidsOf, MOON_GRAVITY, WorldGenerator } from './Bodies.ts'
import { sampleChunk } from './ChunkSampler.ts'
import { CHUNK_SIZE, DEFAULT_MOON, MATERIAL_ORE, mineralOf } from './Types.ts'

const params = { ...DEFAULT_MOON, seed: 7, asteroids: 3 }

test('a seed places the same asteroids, above the moon and clear of it', () => {
  const a = asteroidsOf(params)
  const b = asteroidsOf(params)
  assert.equal(a.length, 3)
  assert.deepEqual(
    a.map((x) => [x.x, x.y, x.z, x.radius]),
    b.map((x) => [x.x, x.y, x.z, x.radius]),
  )
  for (const s of a) {
    const rho = Math.hypot(s.x, s.y, s.z)
    assert.ok(rho - s.reach > params.radius + params.amplitude + 10, `asteroid at ${rho} with reach ${s.reach}`)
  }
  // The first is over the north pole, in sight of a player starting there
  assert.ok(a[0].y / Math.hypot(a[0].x, a[0].y, a[0].z) > 0.9)
})

test('the world is solid inside each body and empty between them', () => {
  const w = new WorldGenerator(params)
  for (const b of w.bodies) assert.ok(w.density(b.x, b.y, b.z) < 0, `${b.kind} centre is solid`)
  const [moon, rock] = w.bodies
  const mid = { x: (moon.x + rock.x) / 2, y: (moon.radius + Math.hypot(rock.x, rock.y, rock.z) - rock.radius) / 2, z: 0 }
  const dir = Math.hypot(rock.x, rock.y, rock.z)
  const gap = (params.radius + params.amplitude + (dir - rock.reach)) / 2
  assert.ok(w.density((rock.x / dir) * gap, (rock.y / dir) * gap, (rock.z / dir) * gap) > 0, `empty between, ${JSON.stringify(mid)}`)
})

test('gravity is the moon\'s at its surface and points down', () => {
  const w = new WorldGenerator({ ...params, asteroids: 0 })
  const g = w.gravity(0, 100, 0, { x: 0, y: 0, z: 0 })
  // the planet 1.6 km away adds a pull of under a millionth of a m/s² (V9)
  assert.ok(Math.abs(g.y + MOON_GRAVITY) < 1e-6 && Math.abs(g.x) < 1e-5)
})

test('a chunk across an asteroid samples the same densities as the point function', () => {
  const w = new WorldGenerator(params)
  const rock = w.bodies[1]
  const size = CHUNK_SIZE * params.voxelSize
  const cx = Math.floor((rock.x + rock.radius) / size)
  const cy = Math.floor(rock.y / size)
  const cz = Math.floor(rock.z / size)
  const s = sampleChunk(w, cx, cy, cz, [])
  const vs = params.voxelSize
  for (const [i, j, k] of [[0, 0, 0], [5, 17, 30], [33, 33, 33], [12, 1, 20]]) {
    const x = (cx * CHUNK_SIZE - 1 + i) * vs
    const y = (cy * CHUNK_SIZE - 1 + j) * vs
    const z = (cz * CHUNK_SIZE - 1 + k) * vs
    assert.equal(s.density[i + 34 * (j + 34 * k)], Math.fround(w.density(x, y, z)))
  }
})

test('standing on an asteroid, its own pull wins over the moon\'s', () => {
  const w = new WorldGenerator(params)
  for (const rock of w.bodies.slice(1)) {
    const dist = Math.hypot(rock.x, rock.y, rock.z)
    // On the far side and on the side facing the moon
    for (const side of [1, -1]) {
      const k = (dist + side * rock.radius) / dist
      const g = w.gravity(rock.x * k, rock.y * k, rock.z * k, { x: 0, y: 0, z: 0 })
      const toward = -side * ((g.x * rock.x + g.y * rock.y + g.z * rock.z) / dist)
      assert.ok(toward > 0.2, `pull toward the asteroid on side ${side}: ${toward}`)
    }
  }
})

test('ore lies in the asteroids and not in the moon (V8)', () => {
  const w = new WorldGenerator(params)
  const count = (cx: number, cy: number, cz: number, r: number) => {
    let ore = 0
    let solid = 0
    for (let i = 0; i < 4000; i++) {
      const x = cx + (Math.random() * 2 - 1) * r
      const y = cy + (Math.random() * 2 - 1) * r
      const z = cz + (Math.random() * 2 - 1) * r
      const d = w.density(x, y, z)
      if (d >= 0) continue
      solid++
      if (w.material(x, y, z, d) === MATERIAL_ORE) ore++
    }
    return { ore, solid }
  }
  const a = asteroidsOf(params)[0]
  const rock = count(a.x, a.y, a.z, a.radius * 0.8)
  assert.ok(rock.solid > 100 && rock.ore / rock.solid > 0.2, `asteroid ore ${rock.ore} of ${rock.solid}`)
  const moon = count(0, params.radius - 10, 0, 8)
  assert.ok(moon.solid > 100 && moon.ore === 0, `moon ore ${moon.ore} of ${moon.solid}`)
})

test('the planet with air lies a short flight away, its ground soil and its pull its own (V9)', async () => {
  const { PLANET } = await import('./Bodies.ts')
  const { MATERIAL_SOIL } = await import('./Types.ts')
  const w = new WorldGenerator(params)
  const planet = w.bodies.find((b) => b.kind === 'planet')!
  assert.equal(planet.x, PLANET.x)
  // the ground just under the surface at its north pole is soil
  const top = PLANET.radius + planet.gen.maxOutward
  let y = top
  while (w.density(PLANET.x, y, 0) > 0) y -= 0.25
  assert.equal(w.material(PLANET.x, y - 0.3, 0, w.density(PLANET.x, y - 0.3, 0)), MATERIAL_SOIL)
  // standing on it, the planet pulls far harder than the moon
  const g = w.gravity(PLANET.x, y + 1, 0, { x: 0, y: 0, z: 0 })
  assert.ok(g.y < -1.5 && Math.abs(g.x) < 0.05, JSON.stringify(g))
  // and halfway there nothing much pulls
  const mid = w.gravity(PLANET.x / 2, 0, 0, { x: 0, y: 0, z: 0 })
  assert.ok(Math.hypot(mid.x, mid.y, mid.z) < 0.01)
})

test('the moon veins ferrite, silica and ice; the asteroids malachite, wolframite and spodumene (V29)', () => {
  const w = new WorldGenerator({ ...DEFAULT_MOON })
  const seen = (b: (typeof w.bodies)[number]) => {
    const out = new Set<string>()
    for (let i = 0; i < 4000; i++) {
      const t = i * 2.399
      const r = b.radius * 0.6
      const x = b.x + r * Math.cos(t) * Math.sin(i * 0.37)
      const y = b.y + r * Math.cos(i * 0.37)
      const z = b.z + r * Math.sin(t) * Math.sin(i * 0.37)
      const m = mineralOf(b.gen.material(x, y, z, -10))
      if (m) out.add(m)
    }
    return out
  }
  assert.deepEqual([...seen(w.bodies[0])].sort(), ['ferrite', 'ice', 'silica'])
  assert.deepEqual([...seen(w.bodies.find((b) => b.kind === 'asteroid')!)].sort(), ['malachite', 'spodumene', 'wolframite'])
})
