// The north road past Ashby (forge 375 S32). The hearth-watch sent the
// player north (S31), so once it has met, the furrows out of Ashby lead on
// up the north road: four places the taken were walked past, the last a
// ford where new snow has covered everything. The trail goes cold there,
// on purpose: the user's lore (2026-11-27) keeps where the Hollowed are
// made off the map for now. The captives were walked, taken not killed,
// by road, never underground; nothing here names more than Kasamir and the
// Enemy, and this step names neither.

type P = { x: number; z: number }

export const WAYMARK_REACH = 2.6 // metres from a waymark

// The waymarks show once the hearth-watch has met
export function northRoadReady(done: Readonly<Record<string, boolean>>): boolean {
  return !!done.council && !done.northroad
}

export interface Waymark {
  id: string
  along: number // 0 at the furrows' end, 1 at the area's north edge
  side: number // metres east (+) or west (-) of the road
  title: string
  text: string
}

export const WAYMARKS: readonly Waymark[] = [
  { id: 'camp', along: 0.2, side: -2, title: 'A night camp with no tents', text: 'A clearing beside the road, trampled flat. Dozens of hollows pressed into the snow where people lay down close together for warmth, and three fire rings, cold, that nobody sat near: the hollows keep well back from them.' },
  { id: 'post', along: 0.45, side: 2.5, title: 'Rope on a post', text: 'A waymark post, rope wound round it many times and cut, not untied. The ends are frayed where hands worked at them all night. Whoever was tied here was meant to be walked on in the morning.' },
  { id: 'joined', along: 0.7, side: -2.5, title: 'Other furrows join', text: 'Here a second set of furrows comes in from the east, as many again, and runs on beside the first. Ashby was not the only village emptied this winter. Someone is gathering them.' },
  { id: 'ford', along: 0.95, side: 0, title: 'The ford, and new snow', text: 'The road goes down into a ford, the river iced at its edges. On the far bank the snow lies smooth and new. Whatever was walked across here, last night\'s fall has covered every print.' },
]

// Where each waymark lies: north from the furrows (at fromZ, x) toward the
// area's north edge (z = edge), off the road by its side
export function waymarkSpots(x: number, fromZ: number, edge: number): (Waymark & P)[] {
  return WAYMARKS.map((w) => ({ ...w, x: x + w.side, z: fromZ + (edge - fromZ) * w.along }))
}

export function waymarkAt(spots: readonly (Waymark & P)[], p: P): (Waymark & P) | null {
  return spots.find((s) => Math.hypot(s.x - p.x, s.z - p.z) <= WAYMARK_REACH) ?? null
}

export const NORTH_SENSE = 'Woodsmoke long gone cold, and many feet'
export const NORTH_LINE =
  'Corran Vey crouches at the water and says nothing for a while. "Two villages walked north in a winter, and nobody heard," he says. "They crossed here, and the snow did the rest." He stands. "Back to the Rest. The reeve will want to know there are two."'
