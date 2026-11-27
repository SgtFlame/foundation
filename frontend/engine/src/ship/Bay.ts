// The bay floor (forge 360 V34; the user, 2026-11-27): a static deck round
// a docking clamp, an early building blueprint. Ships laid out from a
// design chip are hand-built only on a finished bay floor (startShip names
// it as `bay`), and a ship resting on one with its helm idle is held by the
// clamp, so ships need no wheels.

import type { Helm, ShipState } from './Ship.ts'

export const BAY_REACH = 12 // metres from the bay's middle (cmd-game-startShip agrees)
export const CLAMP_REACH = 4 // metres from the clamp a resting ship is held
export const CLAMP_STILL = 0.5 // m/s: slower than this counts as resting
export const BAY_SHIPS: readonly string[] = ['cutter', 'hauler', 'freighter', 'barge']
export const BAY_TOP = 0.5 // metres from the bay's origin to its deck's top

type V = { x: number; y: number; z: number }

export function needsBay(design: string): boolean {
  return BAY_SHIPS.includes(design)
}

function finished(s: ShipState): boolean {
  return s.design === 'bay' && s.placed.length >= s.blocks
}

// The nearest finished bay floor within reach of a point, if any
export function bayNear(at: V, ships: readonly ShipState[], reach = BAY_REACH): ShipState | null {
  let best: ShipState | null = null
  let bestD = reach
  for (const s of ships) {
    if (!finished(s)) continue
    const d = Math.hypot(at.x - s.x, at.y - s.y, at.z - s.z)
    if (d <= bestD) {
      best = s
      bestD = d
    }
  }
  return best
}

// Whether the clamp holds a flown ship: on a finished bay, resting, and not
// being pushed off it
export function clamped(at: V, speed: number, helm: Helm, ships: readonly ShipState[]): ShipState | null {
  if (speed > CLAMP_STILL) return null
  if (helm.forward !== 0 || helm.right !== 0 || helm.up !== 0) return null
  return bayNear(at, ships, CLAMP_REACH)
}
