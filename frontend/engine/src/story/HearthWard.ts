// The hearth ward (forge 375 S24): the night the ward was tried (S23)
// taught that a hearthstone is a threshold too. The culvert gives five more
// river stones, and sage has seeded itself on the ditch bank now the water
// runs clean; laid on Hawk's Rest's hearthstone, they ward the hearth as
// the door is warded, and nothing comes up through it again.

export const SAGE_REACH = 1.6 // metres from the sage on the bank
export const HEARTH_REACH = 1.4 // metres from the hearthstone's middle

export type Ward = 'hearthward' | 'farmward' | 'wellward'

// The ward still to lay: Hawk's Rest's hearth (S24), then Wenlan's (S25),
// then, once the well has been looked into, the well (S27)
export function nextHearthWard(done: Readonly<Record<string, boolean>>): Ward | null {
  if (!done.tried) return null
  if (!done.hearthward) return 'hearthward'
  if (!done.farmward) return 'farmward'
  return done.well && !done.wellward ? 'wellward' : null
}

// The gathering spots for each hearth ward (each spot is counted once)
export function wardSpots(ward: Ward): { stones: string; sage: string } {
  if (ward === 'wellward') return { stones: 'culvert-stones-4', sage: 'ditch-sage-3' }
  return ward === 'hearthward' ? { stones: 'culvert-stones-2', sage: 'ditch-sage' } : { stones: 'culvert-stones-3', sage: 'ditch-sage-2' }
}

// Whether the culvert has stones for the next hearth ward
export function hearthStonesReady(done: Readonly<Record<string, boolean>>, items: Readonly<Record<string, number>>): boolean {
  return !!nextHearthWard(done) && !items.riverstones
}

// Whether the sage on the ditch bank waits to be picked for the next hearth ward
export function bankSageReady(done: Readonly<Record<string, boolean>>, items: Readonly<Record<string, number>>, picked: Readonly<Record<string, boolean>>): boolean {
  const w = nextHearthWard(done)
  return !!w && !items.sage && !picked[wardSpots(w).sage]
}

// Whether the hearth ward can be laid: the lore, the stones and a sage
export function hearthWardReady(done: Readonly<Record<string, boolean>>, items: Readonly<Record<string, number>>, ward: Ward = 'hearthward'): boolean {
  return nextHearthWard(done) === ward && (items.riverstones ?? 0) >= 1 && (items.sage ?? 0) >= 1
}

// S25: whether a house's hearth is warded: Hawk's Rest's (a house with
// nobody in it) once the hearth ward is laid, a house with Wenlan in it
// once the farm ward is
export function hearthWarded(done: Readonly<Record<string, boolean>>, house: { npcs?: Readonly<Record<string, { dialogue?: string; kind?: string }>> }): boolean {
  const people = Object.values(house.npcs ?? {}).filter((n) => n.kind !== 'hollowed')
  if (!people.length) return !!done.hearthward
  return !!done.farmward && people.some((n) => n.dialogue === 'farmer' || n.kind === 'watcher')
}

// What is missing for the hearth ward, worst first, or '' when it can be laid
export function hearthWardMissing(items: Readonly<Record<string, number>>, what = 'The hearthstone'): string {
  if (!(items.riverstones ?? 0)) return `${what} wants five river stones, as the door had: the culvert has more`
  if (!(items.sage ?? 0)) return `${what} wants sage: some has seeded itself on the ditch bank by the culvert`
  return ''
}

// Where the sage grows: on the bank a little east of the culvert
export function bankSageSpot(ditch: { x: number; z: number }): { x: number; z: number } {
  return { x: ditch.x + 2.2, z: ditch.z + 0.6 }
}

export const FARM_WARD_LINE = 'Wenlan watches you kneel on his hearthstone, the one they came up through. Sage round its edge, a stone at each corner and its heart. "My grandmother did something like that," he says, very quietly. "I thought it was for luck."'
export const FARM_WARDED_NIGHT = 'The hearthstone stirs once, near midnight, and settles. Nothing comes through a warded hearth.'
export const SAGE_LINE = 'On the ditch bank, where the clean water runs now, a clump of sage has seeded itself. You take a handful.'
export const HEARTH_WARD_LINE = 'You kneel on the hearthstone, lay the sage around its edge and set the five stones at its corners and its heart. The fire leans toward you and steadies. The hearth is a threshold, and it is warded now.'
