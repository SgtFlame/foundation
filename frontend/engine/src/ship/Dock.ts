import type { ShipState } from './Ship.ts'
import { PORT_REACH } from './Pad.ts'
import { BAY_REACH, BAY_SHIPS } from './Bay.ts'

// V40 (forge 360): a ship started on a bay floor is held by its docking
// clamp until it is launched (game.launchShip); the bay floor holds one ship
// at a time. A cutter or hauler launches from its bay anywhere; a freighter or
// barge only through an open spaceport tower within PORT_REACH.
export const LARGE_SHIPS = ['freighter', 'barge']

export type LaunchPlan = { ok: true; tower?: string } | { ok: false; why: string }

// Whether a docking clamp holds the ship
export function docked(ship: ShipState | undefined): boolean {
  return !!ship?.bay
}

// The open tower nearest the ship within reach, if any
export function launchTower(ship: ShipState, ships: readonly ShipState[]): ShipState | null {
  const d = (s: ShipState) => Math.hypot(s.x - ship.x, s.y - ship.y, s.z - ship.z)
  return (
    ships
      .filter((s) => s.design === 'tower' && s.world === ship.world && (s.port ?? []).length > 0 && d(s) <= PORT_REACH)
      .sort((a, b) => d(a) - d(b))[0] ?? null
  )
}

// What launching the ship needs, as game.launchShip decides it
export function launchPlan(ship: ShipState, ships: readonly ShipState[]): LaunchPlan {
  if (!docked(ship)) return { ok: false, why: 'no docking clamp holds this ship' }
  if (LARGE_SHIPS.includes(ship.design)) {
    const tower = launchTower(ship, ships)
    if (!tower) return { ok: false, why: `a ${ship.design} launches only through an open spaceport: open a tower's port within ${PORT_REACH} m` }
    if (ship.placed.length < ship.blocks) return { ok: false, why: 'finish the ship before launching it' }
    return { ok: true, tower: tower.id }
  }
  if (ship.placed.length < ship.blocks) return { ok: false, why: 'finish the ship before launching it' }
  return { ok: true }
}

// Whether a bay floor is free for a new ship
export function bayFree(bay: ShipState): boolean {
  return !bay.holds
}

// V41: a cutter, hauler, freighter or barge flown slowly back over a free,
// finished bay floor parks on it (game.dockShip): the clamp holds it again
export const PARK_SPEED = 2 // m/s: faster than this is refused

export type ParkPlan = { ok: true; bay: string } | { ok: false; why: string }

type V = { x: number; y: number; z: number }

// The nearest free, finished bay floor within reach of a point, if any
export function freeBayNear(at: V, world: string, ships: readonly ShipState[], reach = BAY_REACH): ShipState | null {
  const d = (s: ShipState) => Math.hypot(s.x - at.x, s.y - at.y, s.z - at.z)
  return (
    ships
      .filter((s) => s.design === 'bay' && s.world === world && s.placed.length >= s.blocks && bayFree(s) && d(s) <= reach)
      .sort((a, b) => d(a) - d(b))[0] ?? null
  )
}

// What parking the flown ship needs, as game.dockShip decides it
export function parkPlan(ship: ShipState, at: V, speed: number, ships: readonly ShipState[]): ParkPlan {
  if (!BAY_SHIPS.includes(ship.design)) return { ok: false, why: 'only a cutter, hauler, freighter or barge parks on a bay floor' }
  if (docked(ship)) return { ok: false, why: 'a docking clamp already holds this ship' }
  const bay = freeBayNear(at, ship.world, ships)
  if (!bay) return { ok: false, why: `fly within ${BAY_REACH} m of a free bay floor` }
  if (speed > PARK_SPEED) return { ok: false, why: `slow down to park: under ${PARK_SPEED} m/s` }
  return { ok: true, bay: bay.id }
}
