import { mulberry32 } from '../voxel/Noise.ts'

// Wrecks on the moon (forge 360 V15; the play design's scavenging: "wrecks
// hold containers and salvageable blocks; tearing down a block yields part
// of its materials, so salvage and mining feed the same chain"). Two
// derelict skiffs, k0 and k1, lie half buried a walk from the outpost,
// placed from the world's seed so every player sees them in the same
// place; game.salvage (cmd-game-salvage) tears a block off for scrap.

export const WRECK_IDS = ['k0', 'k1'] as const
export const SALVAGE_ORE = 0.25 // m³ of ore a block's scrap gives (the server's SalvageOre)
// V18: the block of each wreck that holds a manual (the server's ManualBlock)
export const MANUAL_BLOCK = 13
export const SALVAGE_REACH = 2.6 // m from a block's centre

export interface Vec3 {
  x: number
  y: number
  z: number
}

export interface WreckSite {
  id: string
  dir: Vec3 // from the moon's centre, unit length
  spin: number // turn about the ground's up, radians
  tilt: number // lean, radians: it came down hard
}

// Where the wrecks lie: 30° to 45° from the moon's top (where the outpost
// stands), on opposite sides, so a walk of 50 to 80 m reaches each
export function wreckSites(seed: number): WreckSite[] {
  const rand = mulberry32((seed * 40503 + 911) >>> 0)
  const az0 = rand() * Math.PI * 2
  return WRECK_IDS.map((id, i) => {
    const polar = ((30 + rand() * 15) * Math.PI) / 180
    const az = az0 + i * Math.PI + (rand() - 0.5) * 0.8
    const dir = { x: Math.sin(polar) * Math.cos(az), y: Math.cos(polar), z: Math.sin(polar) * Math.sin(az) }
    return { id, dir, spin: rand() * Math.PI * 2, tilt: 0.2 + rand() * 0.3 }
  })
}

export const salvageKey = (wreck: string, index: number): string => `${wreck}.${index}`

// The nearest block still there within reach of a point, from the blocks'
// world centres per wreck
export function nearestSalvage(
  wrecks: readonly { id: string; blocks: readonly (Vec3 | null)[] }[],
  salvaged: Readonly<Record<string, boolean>>,
  p: Vec3,
  reach = SALVAGE_REACH,
): { wreck: string; index: number; dist: number } | null {
  let best: { wreck: string; index: number; dist: number } | null = null
  for (const w of wrecks) {
    w.blocks.forEach((c, index) => {
      if (!c || salvaged[salvageKey(w.id, index)]) return
      const dist = Math.hypot(c.x - p.x, c.y - p.y, c.z - p.z)
      if (dist <= reach && (!best || dist < best.dist)) best = { wreck: w.id, index, dist }
    })
  }
  return best
}
