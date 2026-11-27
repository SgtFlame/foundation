// The night the ward is tried (forge 375 S23; the design's hearth raids:
// "the threat comes through the home", and its wards: "so nothing unwelcome
// crosses"). After the ward is laid (S22), R by Hawk's Rest's hearth waits
// for the next night with no moon. Past midnight something tries the door
// and stops at the ward; then the hearthstone moves, and one comes up
// through the hearth instead. Held until dawn, it leaves the hearth lore:
// a hearthstone is a threshold too, for the hearth wards still to come.

import type { NightOptions } from '../defence/HearthNight.ts'

// Whether the ward waits to be tried: laid, and the night not yet kept
export function triedReady(done: Readonly<Record<string, boolean>>): boolean {
  return !!done.ward && !done.tried
}

// The night at Hawk's Rest: one comes through, alone, and dawn comes sooner
// than at the farmhouse (nobody keeps the watch but the character)
export const TRIED_NIGHT: Required<Pick<NightOptions, 'raiders' | 'length'>> = { raiders: 1, length: 70 }

export const TRIED_DOOR_LINE = 'Past midnight, claws at the door. The sage on the sill hisses and smokes, and the scratching stops dead at the ward. Then it is quiet, too quiet, and under the hearth the stone shifts.'
export const TRIED_LINE = 'Dawn greys the shutters. The ward held the door, and the dark went round it, up through the hearth. A hearthstone is a threshold too: you write it in your notes, with the ward\'s pattern beside it.'
export const TRIED_LOST = 'Something dragged the hearth\'s ashes down under the stone. The cottage is cold, but the dark comes round again: by the hearth, press R to wait for the next night with no moon.'
