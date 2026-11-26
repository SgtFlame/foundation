import { mulberry32 } from '../voxel/Noise.ts'

// Swarm surges (forge 360 V16; the play design's base defence: "threats
// come on a schedule the player can see coming: a swarm surge when the
// Silent Fleet's drones wake on a cycle, shown in the HUD ahead"). Every
// SURGE_PERIOD of real time, for SURGE_LENGTH, drones come down out of the
// moon's sky and dive at a player in the open; Q shoots one down, and each
// downed gives a little scrap (game.downDrone, counted once per player and
// drone). The schedule is the clock's, so every player sees it alike.

export const SURGE_PERIOD = 12 * 60_000 // ms between surges
export const SURGE_LENGTH = 75_000 // ms a surge lasts
export const DRONES = 8 // per surge
export const DRONE_SCRAP = 0.1 // m³ of ore a downed drone gives
export const DRONE_HIT = 8 // health a drone takes when it reaches you
export const SHOT_RANGE = 45 // m
export const SHOT_WIDTH = 1.1 // m from the line of fire that still hits

export interface SurgeState {
  index: number // which surge: floor(time / period)
  active: boolean
  startsIn: number // ms until the next surge starts (0 while one is on)
  into: number // ms into the surge while one is on
}

export function surgeAt(now: number): SurgeState {
  const index = Math.floor(now / SURGE_PERIOD)
  const into = now - index * SURGE_PERIOD
  const active = into < SURGE_LENGTH
  return { index, active, startsIn: active ? 0 : SURGE_PERIOD - into, into: active ? into : 0 }
}

export function countdown(ms: number): string {
  const s = Math.ceil(ms / 1000)
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}

export interface Drone {
  id: number
  wakes: number // ms into the surge when it comes down
  azimuth: number // radians: the side of the sky it comes from
  speed: number // m/s
}

// A surge's drones, the same for every player (from the surge's index)
export function dronesOf(index: number): Drone[] {
  const rand = mulberry32((index * 2246822519 + 3) >>> 0)
  return Array.from({ length: DRONES }, (_, id) => ({ id, wakes: (id / DRONES) * (SURGE_LENGTH - 25_000) + rand() * 4000, azimuth: rand() * Math.PI * 2, speed: 6 + rand() * 3 }))
}

export interface Vec3 {
  x: number
  y: number
  z: number
}

// Whether a shot from an eye along a unit direction hits a point: within
// SHOT_WIDTH of the line, ahead, inside SHOT_RANGE; the distance along it
export function shotHits(eye: Vec3, dir: Vec3, p: Vec3): number | null {
  const v = { x: p.x - eye.x, y: p.y - eye.y, z: p.z - eye.z }
  const along = v.x * dir.x + v.y * dir.y + v.z * dir.z
  if (along <= 0 || along > SHOT_RANGE) return null
  const off = Math.hypot(v.x - dir.x * along, v.y - dir.y * along, v.z - dir.z * along)
  return off <= SHOT_WIDTH ? along : null
}

export const downedKey = (surge: number, drone: number): string => `${surge}.${drone}`

// Sentry turrets (forge 360 V17; the play design's base defence, its
// second step: "a turret block that holds the line while you work"). A
// player sets their one turret down on the moon (game.placeTurret); while
// a surge is on, every turret shoots the nearest drone in reach, one shot
// per TURRET_RELOAD seconds, and a drone it downs is counted for the
// player who sees it fall, as their own shot would be.
export const TURRET_RANGE = 28 // m
export const TURRET_RELOAD = 2.2 // s between shots

// The drone a turret at a point shoots next: the nearest within reach
export function turretPick(turret: Vec3, drones: readonly { id: number; at: Vec3 | null; gone: boolean }[]): number | null {
  let best: { id: number; d: number } | null = null
  for (const x of drones) {
    if (!x.at || x.gone) continue
    const d = Math.hypot(x.at.x - turret.x, x.at.y - turret.y, x.at.z - turret.z)
    if (d <= TURRET_RANGE && (!best || d < best.d)) best = { id: x.id, d }
  }
  return best?.id ?? null
}
