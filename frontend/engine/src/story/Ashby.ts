// The Hollowed's road and the village they emptied (forge 375 S28, S29).
// The user's lore (2026-11-27): the Hollowed do not come from the dungeons
// under the capital or the hills. They come from villages other Hollowed
// attacked, where the people were captured, not killed, and turned by
// Kasamir and the Enemy; the player sees only the remnants of a raid in an
// abandoned village. The story never names more than those two.
//
// S28: the morning after the well is warded, a frost track leads from the
// well's rim across the farmstead to the signpost where the road leaves:
// what came up the well went back along the road. At the signpost the
// player learns where it leads: Ashby, whose folk stopped coming to market.
// S29: through the signpost, in the area it leads to, the remnants of a
// raid: four things to look at. Seeing all four reaches the step.

type P = { x: number; z: number }

export const TRACK_REACH = 3 // metres from the signpost
export const REMNANT_REACH = 2.6 // metres from a remnant

// S28: the track calls once the well is warded
export function trackReady(done: Readonly<Record<string, boolean>>): boolean {
  return !!done.wellward && !done.track
}

// S29: the remnants show once the track is followed
export function ashbyReady(done: Readonly<Record<string, boolean>>): boolean {
  return !!done.track && !done.ashby
}

// The frost prints from the well to the signpost, one every `step` metres,
// each a little off the line as a dragging gait would leave them
export function trackPrints(from: P, to: P, step = 1.6): P[] {
  const len = Math.hypot(to.x - from.x, to.z - from.z)
  if (len < 1e-6) return []
  const ux = (to.x - from.x) / len
  const uz = (to.z - from.z) / len
  const out: P[] = []
  for (let d = 2, i = 0; d < len - 1; d += step, i++) {
    const side = (i % 2 === 0 ? 1 : -1) * 0.35
    out.push({ x: from.x + ux * d - uz * side, z: from.z + uz * d + ux * side })
  }
  return out
}

export const TRACK_SENSE = 'Frost on the noon air, and the smell of old iron'
export const TRACK_LINE =
  'The frost prints end at the signpost and go on down the road, east, toward Ashby. Ashby\'s folk have not come to market since the first snow, the farmer said once, and nobody went to ask why. Whatever came up your well went home along this road.'

export interface Remnant {
  id: string
  dx: number // metres from the area's middle
  dz: number
  title: string
  text: string
}

// What is left of the raid, placed round the middle of the area the road leads to
export const REMNANTS: readonly Remnant[] = [
  { id: 'door', dx: -8, dz: -6, title: 'A door left open', text: 'A cottage door hangs open on one hinge. Frost-black handprints climb the frame from the outside, and the bar that should have held it lies in the snow, not broken: lifted.' },
  { id: 'table', dx: 7, dz: -5, title: 'A meal left cold', text: 'A table dragged outside, a pot of stew frozen solid on the ashes beside it, four bowls set out and never filled. Nobody finished this meal, and nobody came back to clear it.' },
  { id: 'shoe', dx: -6, dz: 7, title: 'A child\'s shoe', text: 'One small shoe in the lane, laced. You look for the other, and for the child, and for the blood you were afraid of finding. There is none.' },
  { id: 'furrows', dx: 8, dz: 6, title: 'Furrows to the well', text: 'Many furrows in the frozen mud, side by side, all running the same way: to Ashby\'s well, whose cover has been pushed aside from below. People were walked here. Not carried. Walked.' },
]

// Where each remnant lies in an area of the given size (cells of `cell` m)
export function remnantSpots(width: number, depth: number, cell: number): (Remnant & P)[] {
  const cx = (width * cell) / 2
  const cz = (depth * cell) / 2
  return REMNANTS.map((r) => ({ ...r, x: cx + r.dx, z: cz + r.dz }))
}

// The remnant a character stands by, if any
export function remnantAt(spots: readonly (Remnant & P)[], p: P): (Remnant & P) | null {
  return spots.find((s) => Math.hypot(s.x - p.x, s.z - p.z) <= REMNANT_REACH) ?? null
}

export const ASHBY_SENSE = 'Cold hearths, and no smoke anywhere'
export const ASHBY_LINE =
  'No graves. No bodies. No blood in the snow. Ashby was not burned or slaughtered: it was emptied, every soul in it walked to the well and taken down. Taken, not killed. You think of Wenlan\'s hearthstone, and of what climbed out of it, and of whose faces those might once have been.'

// The area the road leads to is Ashby when its name says so (an author
// makes an area named Ashby and links a signpost to it)
export function isAshby(name: string): boolean {
  return /ashby/i.test(name)
}
