// The threshold ward (forge 375 S22; the design's hedge-craft: "Wards.
// Arrangements of herbs and stones on a threshold so nothing unwelcome
// crosses", and its blessed places: "Sleep there is dreamless and
// healing"). The true dream (S21) taught the pattern: sage along the sill
// of the door and five river stones in their order. The stones come from
// the culvert Wenlan's ditch runs through, washed pale once the willow
// bough was cleared; with them and a sage, walking to Hawk's Rest's door
// lays the ward. A night slept in a warded place is dreamless and healing:
// the Chill gone by morning and a dream's wound closed, warm food or not.

export const STONES_REACH = 2.2 // metres from the culvert
export const WARD_REACH = 1.8 // metres from the door's cell

// Whether the river stones wait at the culvert: the pattern known, not yet taken
export function riverStonesReady(done: Readonly<Record<string, boolean>>, items: Readonly<Record<string, number>>): boolean {
  return !!done.truedream && !items.riverstones && !done.ward
}

// Whether the ward can be laid: the pattern, the stones and a sage
export function wardReady(done: Readonly<Record<string, boolean>>, items: Readonly<Record<string, number>>): boolean {
  return !!done.truedream && !done.ward && (items.riverstones ?? 0) >= 1 && (items.sage ?? 0) >= 1
}

// What is missing for the ward, worst first, or '' when it can be laid
export function wardMissing(items: Readonly<Record<string, number>>): string {
  if (!(items.riverstones ?? 0)) return 'The ward wants five river stones: the culvert by the east field has them'
  if (!(items.sage ?? 0)) return 'The ward wants sage to lay along the sill'
  return ''
}

// The threshold: the middle of the door's cell, inside the cottage
export function thresholdSpot(pieces: readonly { piece: string; x: number; z: number }[], cell: number): { x: number; z: number } | null {
  const d = pieces.find((p) => p.piece === 'door')
  return d ? { x: (d.x + 0.5) * cell, z: (d.z + 0.5) * cell } : null
}

// A warded night: no Chill by morning, and the wound closed
export function wardedNight<T extends { chill: number }>(b: T): T {
  return { ...b, chill: 0 }
}

export const STONES_LINE = 'In the culvert, where the bough was, five river stones washed pale. You weigh them in your hand: these are the ones.'
export const WARD_LINE = 'You lay the sage along the sill and set the five stones in their order, naming each. Something in the cottage settles, like a held breath let go. Nothing unwelcome crosses here now.'
