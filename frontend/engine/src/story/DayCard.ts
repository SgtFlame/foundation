import { drawCard, type Card } from './FirstHourStory.ts'
import { cook, spendHum, type Body, type Dish } from './Hum.ts'

// The day's card (forge 375 S11; the design's rule 7: "a daily one-card
// draw gives a tone for the day, a light buff or warning"). Each morning's
// card from the lesser deck is the same for a story and a day, so it needs
// no saving: drawCard(seed + day), the card the morning and sleeping show.
// Placeholder effects, light as the design asks.

export interface CardEffect {
  speed: number // walking speed, times
  chill: number // the chill a spent Hum leaves, times
  cook: number // what hot food clears, times
  hum: number // Hum added to the sky's pool
  reveal: boolean // what is hidden nearby glows (herbs, the bough)
  line: string // the HUD's words for it
}

export const NO_EFFECT: CardEffect = { speed: 1, chill: 1, cook: 1, hum: 0, reveal: false, line: '' }

export const CARD_EFFECTS: Readonly<Record<string, CardEffect>> = {
  hearth: { ...NO_EFFECT, cook: 1.5, line: 'hot food does more today' },
  lantern: { ...NO_EFFECT, reveal: true, line: 'what is hidden nearby glows' },
  hare: { ...NO_EFFECT, speed: 1.25, line: 'quick feet today' },
  frost: { ...NO_EFFECT, chill: 1.5, line: 'a warning: spending the Hum chills you more' },
  wren: { ...NO_EFFECT, hum: 1, line: 'a friend speaks for you: one Hum more' },
}

// The card of a story's day, or null before the first morning (day 0)
export function dayCard(seed: number, day: number): { card: Card; effect: CardEffect } | null {
  if (day < 1) return null
  const card = drawCard(seed + day)
  return { card, effect: CARD_EFFECTS[card.id] ?? NO_EFFECT }
}

// A Hum spent under the day's card
export function spendWith(b: Body, e: CardEffect): Body {
  const after = spendHum(b)
  if (after === b || e.chill === 1) return after
  return { ...after, chill: Math.min(100, b.chill + (after.chill - b.chill) * e.chill) }
}

// A dish eaten under the day's card
export function cookWith(b: Body, d: Dish, e: CardEffect): Body {
  return cook(b, { ...d, chill: d.chill * e.cook, hunger: d.hunger * e.cook })
}
