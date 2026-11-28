import { DUSK, FIRST_NIGHT, isDay, nightOf, type Clock } from './Clock.ts'
import { GREATER_PERIOD } from '../sky/Moons.ts'

// The new moon and the king's council (forge 375 S36). After the Steward
// (S35) the player waits in Aldermere: R at the door of the Lantern, an inn
// by the city's south edge, takes a room and lets the days pass to the
// evening of the greater moon's dark. That night the palace gate opens
// again and the clerk takes the player and Corran Vey up to the council.
// Placeholder people and lines (the books never show the capital): the
// council names nobody from the books but Kasamir's raids, unnamed.

type P = { x: number; z: number }

export const INN_REACH = 3 // metres from the inn's door

// The Lantern's door: the middle of the area's south edge, a few metres in
export function innSpot(width: number, depth: number, cell: number): P {
  return { x: (width * cell) / 2, z: depth * cell - 3 }
}

export function atInn(inn: P, p: P): boolean {
  return Math.hypot(inn.x - p.x, inn.z - p.z) <= INN_REACH
}

// Whether a calendar night is the greater moon's dark
export function newMoonNight(night: number): boolean {
  return ((night % GREATER_PERIOD) + GREATER_PERIOD) % GREATER_PERIOD === 0
}

// The player may wait at the inn once the Steward has spoken, until the council
export function waitReady(done: Readonly<Record<string, boolean>>): boolean {
  return !!done.steward && !done.kingscouncil
}

// The clock after waiting at the inn: dusk on the next new-moon night, or
// unchanged when it is that night already (evening or the small hours)
export function waitForNewMoon(c: Clock): Clock {
  const tonight = nightOf(c)
  if (newMoonNight(tonight) && !isDay(c)) return c
  let n = tonight + 1
  if (newMoonNight(tonight) && c.hour < DUSK) n = tonight
  while (!newMoonNight(n)) n++
  return { day: n - FIRST_NIGHT, hour: DUSK }
}

// The council sits at night on the greater moon's dark
export function kingsCouncilReady(done: Readonly<Record<string, boolean>>, c: Clock): boolean {
  return !!done.waited && !done.kingscouncil && newMoonNight(nightOf(c)) && !isDay(c)
}

export const INN_LINE =
  'The Lantern\'s keeper gives you the room under the eaves and asks no questions. The days go by in the noise of the street: carts, bells, a fair setting up and taking down. Each night the greater moon is thinner. On the night it is gone, a boy in palace grey knocks at the door.'

export const KINGS_COUNCIL_LINES: readonly string[] = [
  'The clerk meets you at the gate with a lantern and no word, and takes you and Corran Vey up through the dark yard to a long room where a fire burns at either end. Nine chairs at the table, and the king\'s at its head, empty. The Steward stands behind it.',
  'You tell it in the same words you told the reeve: the furrows in the snow, the empty houses at Ashby, the ford where the tracks end. A councillor in a fur collar says "wolves and bad winters". The Steward lays the four letters on the table, one after another, and the room is quiet.',
  'A door opens behind the empty chair and the king comes in, older than his coins, and sits without ceremony. He asks you one thing: "Were any of them killed?" When you say no, not one that you found, he closes his eyes for a long moment.',
]

export const KINGS_COUNCIL_LINE =
  '"Then they are taken, and what is taken may be found," the king says. "Riders go north at dawn to every village above the ford. And you two go with them, and come back and tell me what they find." Corran Vey bows lower than you have ever seen him bow.'
