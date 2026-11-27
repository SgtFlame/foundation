// The landing pad (forge 360 V33): a static grid of concrete laid out from
// its blueprint. A ship resting on a finished pad, within PAD_REACH of its
// middle and barely moving, has its battery charged at PAD_RATE a second
// from the pad's mains; the pilot's next flight update carries the charge.

import type { ShipState } from './Ship.ts'

export const PAD_REACH = 4 // metres from the pad's middle, across and above
export const PAD_RATE = 0.05 // battery share a second
export const PAD_STILL = 0.5 // m/s: slower than this counts as resting

type V = { x: number; y: number; z: number }

// The finished pad a ship rests on, if any
export function padUnder(at: V, speed: number, ships: readonly ShipState[]): ShipState | null {
  if (speed > PAD_STILL) return null
  return ships.find((s) => s.design === 'pad' && s.placed.length >= s.blocks && Math.hypot(at.x - s.x, at.y - s.y, at.z - s.z) <= PAD_REACH) ?? null
}

// The battery after dt seconds on a pad
export function padCharge(charge: number, dt: number): number {
  return Math.min(1, charge + PAD_RATE * dt)
}
