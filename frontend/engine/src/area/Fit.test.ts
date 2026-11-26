import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { fit, pieceOf, turned, type PieceSet, type Placement } from './PieceSet.ts'

// The server's built-in set, read where the server embeds it
const set = JSON.parse(readFileSync(new URL('../../../../backend/features/game/piecesets/farmland.json', import.meta.url), 'utf8')) as PieceSet

test('turning a piece turns its footprint and edges', () => {
  const barn = pieceOf(set, 'barn')!
  const t = turned(barn, 1)
  assert.equal(t.w, 3)
  assert.equal(t.d, 2)
  assert.deepEqual(t.edges.w, ['road', 'grass'])
  assert.equal(t.edges.s[0], 'grass')
  assert.deepEqual(turned(barn, 4).edges, barn.edges)
})

test('fit agrees with the server on the same cases', () => {
  const others: Record<string, Placement> = { house: { id: 'house', piece: 'farmhouse', x: 2, z: 2, rot: 0 } }
  const cases: [Placement, string][] = [
    [{ id: 'r', piece: 'road', x: 3, z: 4, rot: 0 }, ''],
    [{ id: 'r', piece: 'road', x: 2, z: 4, rot: 0 }, 'does not meet'],
    [{ id: 'r', piece: 'grass', x: 3, z: 4, rot: 0 }, 'does not meet'],
    [{ id: 'r', piece: 'tree', x: 3, z: 3, rot: 0 }, 'would cover the Farmhouse'],
    [{ id: 'r', piece: 'barn', x: 9, z: 0, rot: 0 }, 'does not fit inside'],
    [{ id: 'r', piece: 'roadEnd', x: 3, z: 4, rot: 0 }, ''],
    [{ id: 'r', piece: 'roadEnd', x: 3, z: 4, rot: 2 }, 'does not meet'],
    [{ id: 'house', piece: 'farmhouse', x: 2, z: 2, rot: 1 }, ''],
    [{ id: 'r', piece: 'castle', x: 0, z: 0, rot: 0 }, 'there is no piece'],
  ]
  for (const [pl, want] of cases) {
    const got = fit(set, 10, 10, others, pl)
    assert.ok(want === '' ? got === '' : got.includes(want), `${JSON.stringify(pl)}: "${got}", want "${want}"`)
  }
  // The exact message, as the Go check words it
  assert.equal(
    fit(set, 10, 10, others, { id: 'r', piece: 'road', x: 2, z: 4, rot: 0 }),
    "the Road's north edge (road) at cell 2, 4 does not meet the Farmhouse's south edge (grass)",
  )
})

test('the farmstead sample fits, piece by piece', async () => {
  const { farmstead } = await import('./Samples.ts')
  const placed: Record<string, Placement> = {}
  farmstead.pieces.forEach((p, i) => {
    const pl = { ...p, id: `p${i}` }
    assert.equal(fit(set, farmstead.width, farmstead.depth, placed, pl), '', `${JSON.stringify(pl)}`)
    placed[pl.id] = pl
  })
})

test('an interior piece marked boundary stands on the edge', () => {
  const kitchen = JSON.parse(readFileSync(new URL('../../../../backend/features/game/piecesets/kitchen.json', import.meta.url), 'utf8')) as PieceSet
  assert.equal(kitchen.interior, true)
  assert.equal(fit(kitchen, 6, 5, {}, { id: 'h', piece: 'hearth', x: 2, z: 0, rot: 0 }), '')
  assert.match(fit(kitchen, 6, 5, {}, { id: 'h', piece: 'hearth', x: 2, z: 2, rot: 0 }), /must stand on the area's edge/)
  assert.equal(fit(kitchen, 6, 5, {}, { id: 't', piece: 'table', x: 2, z: 2, rot: 0 }), '')
  assert.equal(pieceOf(kitchen, 'door')?.door, true)
})

test('the kitchen sample fits, piece by piece, and its people stand on the floor', async () => {
  const { kitchen } = await import('./Samples.ts')
  const ks = JSON.parse(readFileSync(new URL('../../../../backend/features/game/piecesets/kitchen.json', import.meta.url), 'utf8')) as PieceSet
  const placed: Record<string, Placement> = {}
  kitchen.pieces.forEach((p, i) => {
    const pl = { ...p, id: p.id ?? `p${i}` }
    assert.equal(fit(ks, kitchen.width, kitchen.depth, placed, pl), '', `${JSON.stringify(pl)}`)
    placed[pl.id] = pl
  })
  for (const n of kitchen.npcs) {
    const cx = Math.floor(n.x / ks.cell)
    const cz = Math.floor(n.z / ks.cell)
    const under = Object.values(placed).find((pl) => {
      const t = turned(pieceOf(ks, pl.piece)!, pl.rot)
      return cx >= pl.x && cx < pl.x + t.w && cz >= pl.z && cz < pl.z + t.d
    })
    assert.equal(under, undefined, `${n.name} stands on ${under?.piece}`)
  }
})
