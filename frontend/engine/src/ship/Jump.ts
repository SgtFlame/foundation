import { JUMP_CHARGE } from '../craft/Fabricator.ts'
import { PLANET } from '../voxel/Bodies.ts'

// The jump drive (forge 360 V27; the play design's road off the moon: jump
// drives need what the moon alone does not give). A ship fitted with one
// (game.fitDrive, Ship.jump) jumps from its cockpit between the moon and the
// planet with air, spending JUMP_CHARGE of its battery: it comes out JUMP_HEIGHT
// above the other body's top, where the outpost and the cabin stand, and the
// pilot saves the new place with game.flyShip as any flight does.

export const JUMP_HEIGHT = 60 // m above the other body's surface

export interface Vec3 {
  x: number
  y: number
  z: number
}

// Where a ship at p comes out: over the planet when it is nearer the moon,
// over the moon when nearer the planet
export function jumpTarget(p: Vec3, moonRadius: number): { at: Vec3; to: 'planet' | 'moon' } {
  const toMoon = Math.hypot(p.x, p.y, p.z)
  const toPlanet = Math.hypot(p.x - PLANET.x, p.y - PLANET.y, p.z - PLANET.z)
  if (toMoon <= toPlanet) return { at: { x: PLANET.x + 6, y: PLANET.y + PLANET.radius + JUMP_HEIGHT, z: PLANET.z + 6 }, to: 'planet' }
  return { at: { x: 6, y: moonRadius + JUMP_HEIGHT, z: 6 }, to: 'moon' }
}

export type JumpCheck = { ok: true } | { ok: false; why: string }

// Whether a ship may jump now: a drive fitted and enough charge
export function canJump(s: { jump?: boolean } | null | undefined, charge: number): JumpCheck {
  if (!s?.jump) return { ok: false, why: 'no jump drive is fitted' }
  if (charge < JUMP_CHARGE - 1e-9) return { ok: false, why: `the jump drive needs ${Math.round(JUMP_CHARGE * 100)}% charge: land in the sun and let the panels charge it` }
  return { ok: true }
}
