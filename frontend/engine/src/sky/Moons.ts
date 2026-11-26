// The two moons of the story game (forge 375 S2.1): the greater moon waxes
// and wanes over 30 nights, the lesser over 13 (375-StoryRPGDesign › the
// world). A night is a whole number counted from the calendar's start, so
// the phases follow from the date alone and every client agrees.

export interface MoonPhase {
  // 0 new, 0.5 full, back to 1 (= 0)
  phase: number
  // the lit share of the disc, 0 to 1
  lit: number
  // waxing: lit on the right as seen from the ground
  waxing: boolean
}

export const GREATER_PERIOD = 30
export const LESSER_PERIOD = 13

export function moonPhase(night: number, period: number, offset = 0): MoonPhase {
  const p = ((((night + offset) % period) + period) % period) / period
  return { phase: p, lit: (1 - Math.cos(2 * Math.PI * p)) / 2, waxing: p < 0.5 }
}

// Both moons on a night: the lesser starts its count 4 nights in, so the
// two are full together only now and then (a night the story can use)
export function moons(night: number): { greater: MoonPhase; lesser: MoonPhase } {
  return { greater: moonPhase(night, GREATER_PERIOD), lesser: moonPhase(night, LESSER_PERIOD, 4) }
}

// How bright a moon lights the ground: a new moon still leaves starlight
export function moonLight(m: MoonPhase): number {
  return 0.12 + 0.88 * m.lit
}
