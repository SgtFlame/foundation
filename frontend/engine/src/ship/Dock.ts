import type { ShipState } from './Ship.ts'
import { PORT_REACH } from './Pad.ts'

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
