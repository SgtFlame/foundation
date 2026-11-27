// The road to Aldermere (forge 375 S34). The reeve's letter (S33) goes to
// the capital, which the books never name: the user (2026-11-27) asked for a
// made-up name, Aldermere, and said it is days on foot from Hawk's Rest with
// several towns on the way. Travelling into an area named Aldermere with the
// letter plays the journey a day at a time (placeholder towns and lines),
// moves the clock on JOURNEY_DAYS days and reaches step `aldermere`.

export const CAPITAL = 'Aldermere'
export const JOURNEY_DAYS = 4

export function isAldermere(name: string): boolean {
  return /aldermere/i.test(name)
}

export function journeyReady(done: Readonly<Record<string, boolean>>): boolean {
  return !!done.reeve && !done.aldermere
}

export interface Leg {
  town: string
  line: string
}

export const LEGS: readonly Leg[] = [
  { town: 'Fallowford', line: 'Day one. The road south out of Hawk\'s Rest, the snow thinning to slush. Fallowford by dark: a mill town, its wheel locked in ice. The innkeeper has heard of no emptied villages, and does not want to.' },
  { town: 'Harrow Cross', line: 'Day two. Carters and a priest on a mule. At Harrow Cross the waymark has a new iron grate bolted over the well beside it, and nobody in the square will say who ordered it.' },
  { town: 'Wexley', line: 'Day three. Rain, then fog off the river. In Wexley a woman at the ford asks Corran if they have come about her brother\'s village, up in the hills. Corran gives her the reeve\'s name, and nothing else.' },
]

export const ARRIVE_LINE =
  'Day four. The fog lifts off the river at noon, and there it is: Aldermere, its walls pale on the hill, smoke from a thousand chimneys, the king\'s banners stiff in the wind over the gate. Corran Vey whistles low. "Well," he says. "Let us find someone who reads letters."'

// The clock after the journey: JOURNEY_DAYS on, arriving at noon
export function afterJourney(clock: { day: number; hour: number }): { day: number; hour: number } {
  return { day: clock.day + JOURNEY_DAYS, hour: 12 }
}
