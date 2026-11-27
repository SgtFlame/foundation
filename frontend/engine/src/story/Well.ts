// The well (forge 375 S26): with both hearths warded, the Hollowed cannot
// come up through Hawk's Rest's or Wenlan's hearthstone. The morning after
// Wenlan's ward is laid, the well in the yard is frost-cold at noon and
// breathes cold air up its shaft: whatever lies below the village runs
// under all of it, and the well is the threshold nobody warded.

export const WELL_REACH = 2.6 // metres from the well's middle

// Where the well stands: in the yard piece, 7 m west and 6 m north of its
// middle (Models.ts draws it there)
export function wellSpot(pieces: readonly { piece: string; x: number; z: number }[], cell: number): { x: number; z: number } | null {
  const yard = pieces.find((p) => p.piece === 'yard')
  return yard ? { x: (yard.x + 1.5) * cell - 7, z: (yard.z + 1.5) * cell - 6 } : null
}

// Whether the well calls now: both hearths warded, the well not yet looked into
export function wellReady(done: Readonly<Record<string, boolean>>): boolean {
  return !!done.hearthward && !!done.farmward && !done.well
}

export const WELL_SENSE = 'Frost, at noon, and a breath of cold air'
export const WELL_LINE = 'Frost furs the well\'s stones though the sun is high. You lean over the rim: the water is skinned with ice, and up the shaft comes a slow breath of cold, in and out, like something asleep. The hearths are shut to them now. This is the way they will come.'

// S27: the well warded as the hearths were, five stones and sage round its rim
export const WELL_WARD_LINE = 'You lay the sage round the well\'s rim and set the five stones on it, one at each quarter and the last on the windlass beam over the shaft. The cold breath stops mid-draw. Below, very far down, something turns over and is still.'
