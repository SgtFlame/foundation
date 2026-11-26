// The first companion (forge 375 S17; the play design's party: "the
// player's character plus up to three companions controlled by AI that
// follow the player's character", one at the start (the user's rule).
// Companions come from kinds of people the reference describes, never the
// book's cast: this one is a veteran from the Drum. Placeholder name,
// invented for the slice. Out of a fight they follow a step behind; in one
// they go for the character's target, or the nearest foe, from its flank,
// and strike with a pike on their own time; their skill decides how well.

export interface CompanionDef {
  id: string
  name: string
  title: string
  color: number
  polearm: number // skill, 0 to 5
  aim: number // the pike's reach to the heart
  reach: number // m
  every: number // seconds between strikes
  speed: number // m/s
}

export const VETERAN: CompanionDef = { id: 'veteran', name: 'Corran Vey', title: 'a veteran of the Drum', color: 0x6e5a3c, polearm: 3, aim: 0.15, reach: 2.3, every: 2.6, speed: 3.6 }

export interface Pt {
  x: number
  z: number
}

export interface CompanionState {
  at: Pt
  cooldown: number
}

// Where the companion heads, and whom it strikes this frame, if anyone:
// out of a fight a step behind the character; in one, to the side of its
// foe away from the character (the flank), striking when in reach
export function companionStep(
  def: CompanionDef,
  st: CompanionState,
  hero: Pt & { facing: number },
  foes: readonly (Pt & { id: string; alive: boolean })[],
  target: string | null,
  dt: number,
): { at: Pt; cooldown: number; strike: string | null; facing: number } {
  const alive = foes.filter((f) => f.alive)
  const foe = alive.find((f) => f.id === target) ?? nearest(alive, st.at)
  let goal: Pt
  if (foe) {
    const ax = foe.x - hero.x
    const az = foe.z - hero.z
    const l = Math.hypot(ax, az) || 1
    // its flank: beside the foe, square to the line from the character
    goal = { x: foe.x - (az / l) * def.reach * 0.8, z: foe.z + (ax / l) * def.reach * 0.8 }
  } else goal = { x: hero.x - Math.sin(hero.facing) * 1.8, z: hero.z - Math.cos(hero.facing) * 1.8 }
  const dx = goal.x - st.at.x
  const dz = goal.z - st.at.z
  const d = Math.hypot(dx, dz)
  const stepLen = Math.min(d, def.speed * dt)
  const at = d > 0.05 ? { x: st.at.x + (dx / d) * stepLen, z: st.at.z + (dz / d) * stepLen } : st.at
  const cooldown = Math.max(0, st.cooldown - dt)
  const inReach = foe ? Math.hypot(foe.x - at.x, foe.z - at.z) <= def.reach + 0.2 : false
  const facing = foe ? Math.atan2(foe.x - at.x, foe.z - at.z) : d > 0.05 ? Math.atan2(dx, dz) : hero.facing
  if (foe && inReach && cooldown === 0) return { at, cooldown: def.every, strike: foe.id, facing }
  return { at, cooldown, strike: null, facing }
}

function nearest<T extends Pt>(list: readonly T[], p: Pt): T | undefined {
  let best: T | undefined
  let bd = Infinity
  for (const f of list) {
    const d = Math.hypot(f.x - p.x, f.z - p.z)
    if (d < bd) {
      bd = d
      best = f
    }
  }
  return best
}
