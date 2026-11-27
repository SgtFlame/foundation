// The hearth-watch meets (forge 375 S31). Once Wenlan has sent word of
// Ashby (S30), R by Hawk's Rest's hearth calls the hearth-watch together:
// the reeve, the miller, the woman from the well and Corran Vey. They hear
// what the player saw, and decide: double watches on dark-moon nights, the
// wells capped with iron, and someone to follow the north road past Ashby
// to see where the taken are walked. Placeholder people and lines, kept to
// the user's lore: the captives were walked away by road, taken not killed.

export function councilReady(done: Readonly<Record<string, boolean>>): boolean {
  return !!done.told && !done.council
}

export function isHawksRest(name: string): boolean {
  return /hawk'?s rest/i.test(name)
}

export const COUNCIL_LINES: readonly string[] = [
  "They come in out of the dark one by one and pull the benches round the hearth: Reeve Aldous with his ledger, Mira from the well, the miller still white to the elbows, and Corran Vey, who stays standing. The Brannocks' boy sits on the woodbox and says nothing at all.",
  '"Two hundred souls," the reeve says, when you have told it. "Walked off in the snow and nobody heard." Mira says what nobody else will: "Then the ones at our doors were someone\'s neighbours once." The fire spits. Nobody answers her.',
  'They settle it before the candle is half gone. Double watches on every dark-moon night. Iron grates over the wells, the smith to start at dawn. And someone to follow the north road past Ashby, to see where the taken are walked. Every face at the hearth turns to you.',
]

export const COUNCIL_LINE = 'Corran Vey picks up his pike. "North, then," he says. "I\'ll want a good pair of boots."'

// S33: back from the ford (S32), R by Hawk's Rest's hearth tells the reeve
// there are two villages emptied, not one. The hearth-watch cannot answer
// that alone: the reeve writes to the capital and asks the player and Corran
// Vey to carry it. Placeholder lines; the road to the capital is not built.
export function reeveReady(done: Readonly<Record<string, boolean>>): boolean {
  return !!done.northroad && !done.reeve
}

export const REEVE_LINES: readonly string[] = [
  'The reeve hears you out with his ledger shut. When Corran says "two villages", he opens it and turns the pages back to autumn, to the market tallies, running his finger down a column. "Ashby," he says. "And Thornwick, up past the ford. Neither came to the last autumn market. I wrote it down and thought nothing of it."',
  '"A hearth-watch can bar its own doors," he says, "but it cannot follow a road through other men\'s country." He writes for a long time, sands the page and folds it twice, and presses his seal into the wax by candlelight.',
]

export const REEVE_LINE = 'He holds the letter out to you, not to Corran. "To the capital, to whoever will read it. You have seen it with your own eyes; they will believe you before a reeve." Corran Vey shoulders his pike. "I\'ll still want those boots."'
