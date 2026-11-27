// The hearth ward (forge 375 S24): the night the ward was tried (S23)
// taught that a hearthstone is a threshold too. The culvert gives five more
// river stones, and sage has seeded itself on the ditch bank now the water
// runs clean; laid on Hawk's Rest's hearthstone, they ward the hearth as
// the door is warded, and nothing comes up through it again.

export const SAGE_REACH = 1.6 // metres from the sage on the bank
export const HEARTH_REACH = 1.4 // metres from the hearthstone's middle

// Whether the culvert has stones for the hearth ward
export function hearthStonesReady(done: Readonly<Record<string, boolean>>, items: Readonly<Record<string, number>>): boolean {
  return !!done.tried && !done.hearthward && !items.riverstones
}

// Whether the sage on the ditch bank waits to be picked
export function bankSageReady(done: Readonly<Record<string, boolean>>, items: Readonly<Record<string, number>>, picked: Readonly<Record<string, boolean>>): boolean {
  return !!done.tried && !done.hearthward && !items.sage && !picked['ditch-sage']
}

// Whether the hearth ward can be laid: the lore, the stones and a sage
export function hearthWardReady(done: Readonly<Record<string, boolean>>, items: Readonly<Record<string, number>>): boolean {
  return !!done.tried && !done.hearthward && (items.riverstones ?? 0) >= 1 && (items.sage ?? 0) >= 1
}

// What is missing for the hearth ward, worst first, or '' when it can be laid
export function hearthWardMissing(items: Readonly<Record<string, number>>): string {
  if (!(items.riverstones ?? 0)) return 'The hearthstone wants five river stones, as the door had: the culvert has more'
  if (!(items.sage ?? 0)) return 'The hearthstone wants sage: some has seeded itself on the ditch bank by the culvert'
  return ''
}

// Where the sage grows: on the bank a little east of the culvert
export function bankSageSpot(ditch: { x: number; z: number }): { x: number; z: number } {
  return { x: ditch.x + 2.2, z: ditch.z + 0.6 }
}

export const SAGE_LINE = 'On the ditch bank, where the clean water runs now, a clump of sage has seeded itself. You take a handful.'
export const HEARTH_WARD_LINE = 'You kneel on the hearthstone, lay the sage around its edge and set the five stones at its corners and its heart. The fire leans toward you and steadies. The hearth is a threshold, and it is warded now.'
