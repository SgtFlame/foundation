import assert from 'node:assert/strict'
import { test } from 'node:test'
import { sampleChunk } from './ChunkSampler.ts'
import { MoonGenerator } from './MoonGenerator.ts'
import { surfaceNets } from './SurfaceNets.ts'
import { CHUNK_SIZE, DEFAULT_MOON, type ChunkMesh } from './Types.ts'

const moon = (seed = 1) => new MoonGenerator({ ...DEFAULT_MOON, seed })

// A chunk on the moon's surface, on the +y axis
function surfaceChunk(gen: MoonGenerator) {
  const size = CHUNK_SIZE * gen.params.voxelSize
  return { cx: 0, cy: Math.floor(gen.params.radius / size), cz: 0 }
}

test('the generator is deterministic for a seed', () => {
  const a = moon(7)
  const b = moon(7)
  const c = moon(8)
  for (const [x, y, z] of [[0, 100, 0], [12.5, -98, 3], [70, 70, 1]]) {
    assert.equal(a.density(x, y, z), b.density(x, y, z))
  }
  assert.notEqual(a.density(12.5, 98, 3), c.density(12.5, 98, 3))
})

test('a surface chunk meshes, with faces pointing out of the ground', () => {
  const gen = moon()
  const { cx, cy, cz } = surfaceChunk(gen)
  const s = sampleChunk(gen, cx, cy, cz, [])
  assert.equal(s.uniform, false)
  const mesh = surfaceNets(s.density, s.material, gen.params.voxelSize) as ChunkMesh
  assert.ok(mesh, 'mesh')
  assert.ok(mesh.indices.length > 300, `triangles: ${mesh.indices.length / 3}`)
  // The geometric normal of each triangle agrees with the density gradient
  let agree = 0
  const p = mesh.positions
  const n = mesh.normals
  for (let t = 0; t < mesh.indices.length; t += 3) {
    const [a, b, c] = [mesh.indices[t] * 3, mesh.indices[t + 1] * 3, mesh.indices[t + 2] * 3]
    const ux = p[b] - p[a], uy = p[b + 1] - p[a + 1], uz = p[b + 2] - p[a + 2]
    const vx = p[c] - p[a], vy = p[c + 1] - p[a + 1], vz = p[c + 2] - p[a + 2]
    const fx = uy * vz - uz * vy, fy = uz * vx - ux * vz, fz = ux * vy - uy * vx
    if (fx * n[a] + fy * n[a + 1] + fz * n[a + 2] > 0) agree++
  }
  assert.ok(agree / (mesh.indices.length / 3) > 0.98, `agree ${agree}`)
})

test('neighbouring chunks share the vertices on their seam', () => {
  const gen = moon()
  const { cx, cy, cz } = surfaceChunk(gen)
  const vs = gen.params.voxelSize
  const size = CHUNK_SIZE * vs
  const key = (x: number, y: number, z: number) => `${x.toFixed(4)},${y.toFixed(4)},${z.toFixed(4)}`
  const verts = (ox: number) => {
    const s = sampleChunk(gen, cx + ox, cy, cz, [])
    const m = surfaceNets(s.density, s.material, vs) as ChunkMesh
    const set = new Set<string>()
    for (let i = 0; i < m.positions.length; i += 3) {
      set.add(key(m.positions[i] + (cx + ox) * size, m.positions[i + 1] + cy * size, m.positions[i + 2] + cz * size))
    }
    return set
  }
  const a = verts(0)
  const b = verts(1)
  // Cell -1 of chunk 1 is cell 31 of chunk 0: their vertices coincide
  let shared = 0
  for (const v of b) if (a.has(v)) shared++
  assert.ok(shared > 20, `shared ${shared}`)
})

test('digging removes ground and filling adds it', () => {
  const gen = moon()
  const { cx, cy, cz } = surfaceChunk(gen)
  const size = CHUNK_SIZE * gen.params.voxelSize
  const x = cx * size + size / 2
  const z = cz * size + size / 2
  // Find the surface height above the chunk's middle
  let y = cy * size
  while (gen.density(x, y, z) < 0) y += 0.1
  const solidCount = (d: Float32Array) => d.reduce((n, v) => n + (v < 0 ? 1 : 0), 0)
  const base = solidCount(sampleChunk(gen, cx, cy, cz, []).density)
  const dug = solidCount(sampleChunk(gen, cx, cy, cz, [{ seq: 1, kind: 'dig', x, y, z, radius: 3 }]).density)
  const filled = solidCount(sampleChunk(gen, cx, cy, cz, [{ seq: 1, kind: 'fill', x, y, z, radius: 3 }]).density)
  assert.ok(dug < base - 100, `dug ${dug} base ${base}`)
  assert.ok(filled > base + 100, `filled ${filled} base ${base}`)
})

test('a chunk samples the same densities as the point function', () => {
  const gen = moon(3)
  const { cx, cy, cz } = surfaceChunk(gen)
  const s = sampleChunk(gen, cx, cy, cz, [])
  const vs = gen.params.voxelSize
  for (const [i, j, k] of [[0, 0, 0], [5, 17, 30], [33, 33, 33], [12, 1, 20]]) {
    const x = (cx * CHUNK_SIZE - 1 + i) * vs
    const y = (cy * CHUNK_SIZE - 1 + j) * vs
    const z = (cz * CHUNK_SIZE - 1 + k) * vs
    assert.equal(s.density[i + 34 * (j + 34 * k)], Math.fround(gen.density(x, y, z)))
  }
})
