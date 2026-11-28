// The letter is read (forge 375 S35). In Aldermere (S34) the palace gate
// stands at the area's north edge, in the middle. Walking up to it with the
// reeve's letter plays the scene a caption at a time: the gate guard, a
// clerk, and the King's Steward, who reads it and has seen two like it this
// month. The king's council will hear it; until then the player waits in the
// city. Placeholder people and lines (the books never show the capital):
// nothing names more than Kasamir and the Enemy, and this step names neither.

type P = { x: number; z: number }

export const GATE_REACH = 4 // metres from the gate

export function palaceReady(done: Readonly<Record<string, boolean>>): boolean {
  return !!done.aldermere && !done.steward
}

// The palace gate: the middle of the area's north edge, a few metres in
export function gateSpot(width: number, cell: number): P {
  return { x: (width * cell) / 2, z: 3 }
}

export function atGate(gate: P, p: P): boolean {
  return Math.hypot(gate.x - p.x, gate.z - p.z) <= GATE_REACH
}

export const PALACE_LINES: readonly string[] = [
  'The gate guard looks at your boots before your face. "Petitions at the Lantern Court, after the spring fair," he says, and is already turning away when Corran holds up the letter so the reeve\'s seal catches the light. The guard stops, reads the wax, and sends a boy running.',
  'The clerk who comes down is young and ink-stained and in a hurry, until he sees the seal too. He does not take the letter. "The Steward will want it from your hand," he says, and leads you in under the arch, across a yard of wet flagstones, up a stair that smells of beeswax.',
  'The King\'s Steward is a grey, narrow man at a table heaped with folded paper. He breaks the seal and reads it twice, and then he lifts two more letters off the heap and lays them beside it. Different seals. "Millbrook," he says. "Carrow Fell. And now Ashby and Thornwick."',
]

export const PALACE_LINE =
  '"The king\'s council sits at the new moon," the Steward says. "You will tell them what you told your reeve, in the same words. Until then, stay in the city, and say nothing in the taverns." Corran Vey looks at the heap of paper and lets out a long breath. "Four villages," he says, under it.'
