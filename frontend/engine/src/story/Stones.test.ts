import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { farmstead, SIGNPOST } from '../area/Samples.ts'
import { pieceOf, turned, type PieceSet } from '../area/PieceSet.ts'
import { moons } from '../sky/Moons.ts'
import { nextWaking, ringSpot, starlorePool, stonePoints, stonesWake } from './Stones.ts'

const set = JSON.parse(readFileSync(new URL('../../../../backend/features/game/piecesets/farmland.json', import.meta.url), 'utf8')) as PieceSet

test('the ring stands on free ground near the signpost', () => {
  const pieces = [...farmstead.pieces, SIGNPOST]
  const sizeOf = (p: { piece: string; rot: number }) => turned(pieceOf(set, p.piece)!, p.rot)
  const h = ringSpot(pieces, sizeOf, farmstead.width, farmstead.depth, set.cell, SIGNPOST)
  assert.ok(h)
  const cx = h.x / set.cell - 1.5
  const cz = h.z / set.cell - 1.5
  for (const p of pieces) {
    const { w, d } = sizeOf(p)
    assert.ok(p.x + w <= cx || p.x >= cx + 3 || p.z + d <= cz || p.z >= cz + 3, `${p.piece} at ${p.x},${p.z} overlaps the ring at ${cx},${cz}`)
  }
  assert.equal(stonePoints(h).length, 9)
})

test('the stones wake on a night of the lesser full moon', () => {
  const n = nextWaking(11)
  assert.ok(n >= 11 && n < 25)
  assert.ok(moons(n).lesser.lit > 0.9)
  assert.ok(stonesWake(n, false))
  assert.ok(!stonesWake(n, true))
  assert.equal(starlorePool(n, true), 1)
  assert.equal(starlorePool(n, false), 0)
})
