// The ore scanner (forge 360 V21; the play design's crafting ladder, the
// forge's first use): a hand scanner made at the bench from forged iron.
// K sends a pulse that finds ore within SCAN_RANGE: the ground is sampled on
// a coarse grid, the ore cells are gathered into pockets (SCAN_BUCKET m
// cubes) and the nearest few are marked through the rock for SCAN_SHOW s.
// Engine only: the pulse reads the same voxels the player sees.

export const SCAN_RANGE = 36 // metres
export const SCAN_STEP = 2.5 // metres between samples
export const SCAN_BUCKET = 8 // metres: one pocket per cube
export const SCAN_SHOW = 12 // seconds the marks stay
export const SCAN_COOLDOWN = 6 // seconds between pulses
export const SCAN_MAX = 6 // pockets marked at most

export interface Pocket {
  x: number
  y: number
  z: number
  cells: number // ore samples in it: how rich it looks
  dist: number // metres from where the pulse was sent
}

type V = { x: number; y: number; z: number }

// The ore pockets around a point, nearest first. isOre says whether a
// point is solid ore (the voxel world's sample there).
export function scan(isOre: (x: number, y: number, z: number) => boolean, at: V, range = SCAN_RANGE, step = SCAN_STEP): Pocket[] {
  const buckets = new Map<string, { x: number; y: number; z: number; n: number }>()
  for (let z = at.z - range; z <= at.z + range; z += step)
    for (let y = at.y - range; y <= at.y + range; y += step)
      for (let x = at.x - range; x <= at.x + range; x += step) {
        if ((x - at.x) ** 2 + (y - at.y) ** 2 + (z - at.z) ** 2 > range * range) continue
        if (!isOre(x, y, z)) continue
        const k = `${Math.floor(x / SCAN_BUCKET)},${Math.floor(y / SCAN_BUCKET)},${Math.floor(z / SCAN_BUCKET)}`
        const b = buckets.get(k) ?? { x: 0, y: 0, z: 0, n: 0 }
        b.x += x
        b.y += y
        b.z += z
        b.n++
        buckets.set(k, b)
      }
  return [...buckets.values()]
    .map((b) => {
      const p = { x: b.x / b.n, y: b.y / b.n, z: b.z / b.n }
      return { ...p, cells: b.n, dist: Math.hypot(p.x - at.x, p.y - at.y, p.z - at.z) }
    })
    .sort((a, b) => a.dist - b.dist)
    .slice(0, SCAN_MAX)
}

// What the pulse found, in a line for the HUD
export function scanLine(found: readonly Pocket[]): string {
  if (!found.length) return `The scanner chirps once: no ore within ${SCAN_RANGE} m`
  const near = found[0]
  return `The scanner pings ${found.length} ore pocket${found.length === 1 ? '' : 's'}: the nearest ${Math.round(near.dist)} m away, marked through the rock`
}
