import { moons } from '../sky/Moons.ts'

// A three-card reading for Wenlan Harrow (forge 375 S10; the design's
// rule 7 and the play design's card readings): stillness first, then three
// cards laid face down and turned in order, past, present and future. How
// clearly a card speaks follows the lesser moon (readings are clearest at
// its full); a good reading gives a concrete clue that opens a quest (his
// winter wheat drowning, a willow bough blocking the ditch, from the
// design's small and human quests). The last card shows a truth he does
// not want, and the player decides whether to tell it: "You can't give
// them back once you've given them." Placeholder cards from the lesser
// deck the player carries (the book's own decks stay with the cast).

export interface ReadingCard {
  id: string
  place: 'past' | 'present' | 'future'
  title: string
  dim: string // what a reader sees when the card speaks faintly
  clear: string // what a reader sees when it speaks clearly
}

export const WENLAN_READING: readonly ReadingCard[] = [
  {
    id: 'plough',
    place: 'past',
    title: 'The Plough',
    dim: 'Work, years of it, in the same ground.',
    clear: "His father's hands cutting the drains along the east field, forty winters ago. The field has never flooded since.",
  },
  {
    id: 'willow',
    place: 'present',
    title: 'The Willow',
    dim: 'Water where it should not stand, and something fallen across a channel at the low corner of a field.',
    clear: 'A willow bough, brought down by the snow, wedged across the culvert under the lane at the low corner of the east field. The water backs up behind it into the wheat.',
  },
  {
    id: 'ash',
    place: 'future',
    title: 'The Ash',
    dim: 'Cold, and something coming before the thaw.',
    clear: 'Ash on snow, and faces with no eyes in them. They come back to this farm before the thaw, on a night with no moon.',
  },
]

// How clearly the cards speak tonight, 0 to 1: the lesser moon's light, a
// half at least for a reader who has learned stillness
export function clarity(night: number, stillness: boolean): number {
  const lit = moons(night).lesser.lit
  return Math.min(1, (stillness ? 0.5 : 0.2) + lit * 0.5)
}

// What a card shows at a clarity: the clear vision from a half up
export function vision(card: ReadingCard, c: number): string {
  return c >= 0.5 ? card.clear : card.dim
}

export type Told = 'all' | 'ditch'

// What Wenlan says to what he is told
export function wenlanAnswer(told: Told): string {
  return told === 'all'
    ? 'Wenlan goes quiet. "My father said the eyeless ones were a tale for children." He looks at the dark window a long while. "I\'ll bar the byre. Clear the ditch for me, will you? There\'s a silver in it."'
    : 'Wenlan laughs with relief. "A bough in the culvert, is it? I\'d have dug the whole field up looking. Clear it for me and there\'s a silver in it."'
}

// The reading's scene: the three cards face down, turned in order by a click,
// then the choice of what to tell. Pure DOM; the viewport owns it.
export class ReadingScene {
  readonly el: HTMLDivElement
  turned = 0
  private readonly cards: HTMLDivElement[] = []
  private readonly choices: HTMLDivElement
  private readonly onTold: (t: Told) => void
  private readonly clarityNow: number

  constructor(container: HTMLElement, who: string, c: number, onTold: (t: Told) => void) {
    this.onTold = onTold
    this.clarityNow = c
    this.el = document.createElement('div')
    this.el.dataset.reading = ''
    this.el.style.cssText =
      'position:absolute;left:50%;top:50%;transform:translate(-50%,-50%);z-index:4;width:min(700px,calc(100% - 32px));padding:16px 18px;border-radius:10px;' +
      'background:rgba(20,16,11,0.94);border:1px solid #8a6d43;box-shadow:0 6px 30px rgba(0,0,0,0.7);font:14px Georgia,serif;color:#efe2c4'
    const h = document.createElement('div')
    h.style.cssText = 'font:600 17px Georgia,serif;color:#e7bf6a;margin-bottom:4px'
    h.textContent = `A reading for ${who}`
    const sub = document.createElement('div')
    sub.style.cssText = 'font-style:italic;opacity:0.85;margin-bottom:12px;line-height:1.4'
    sub.textContent = `"My winter wheat is drowning, and I can't find where the water comes in." You breathe in for four, out for six, and lay three cards face down. ${c >= 0.5 ? 'The lesser moon is kind tonight; the cards feel warm.' : 'The lesser moon is thin; the cards feel distant.'}`
    const row = document.createElement('div')
    row.style.cssText = 'display:flex;gap:10px;justify-content:center;flex-wrap:wrap'
    WENLAN_READING.forEach((card, i) => {
      const el = document.createElement('div')
      el.dataset.readingCard = card.id
      el.style.cssText =
        'flex:1 1 180px;max-width:210px;min-height:190px;padding:12px;border-radius:8px;cursor:pointer;text-align:center;' +
        'background:repeating-linear-gradient(45deg,#2b2117,#2b2117 6px,#33281b 6px,#33281b 12px);border:2px solid #b08a4a'
      el.innerHTML = ''
      const place = document.createElement('div')
      place.style.cssText = 'font:11px system-ui,sans-serif;letter-spacing:0.08em;text-transform:uppercase;opacity:0.7'
      place.textContent = card.place
      const back = document.createElement('div')
      back.style.cssText = 'margin-top:60px;font-size:26px;color:#b08a4a'
      back.textContent = '☾'
      el.append(place, back)
      el.addEventListener('click', () => this.turn(i))
      this.cards.push(el)
      row.append(el)
    })
    this.choices = document.createElement('div')
    this.choices.style.cssText = 'margin-top:14px;display:none'
    const q = document.createElement('div')
    q.style.cssText = 'margin-bottom:8px;line-height:1.4'
    q.textContent = 'What do you tell him? True things cannot be given back once they are given.'
    this.choices.append(q)
    const options: [Told, string][] = [
      ['all', 'Tell him all of it: the ditch, and what the last card showed.'],
      ['ditch', 'Tell him of the ditch, and keep the last card to yourself.'],
    ]
    for (const [t, label] of options) {
      const b = document.createElement('div')
      b.dataset.told = t
      b.style.cssText = 'margin:4px 0;padding:6px 10px;border-radius:6px;cursor:pointer;background:#3a2e1f;border:1px solid #6d5532'
      b.textContent = label
      b.addEventListener('click', () => this.choose(t))
      this.choices.append(b)
    }
    const foot = document.createElement('div')
    foot.style.cssText = 'margin-top:10px;font:11px system-ui,sans-serif;opacity:0.6'
    foot.textContent = 'Click each card to turn it, left to right.'
    this.el.append(h, sub, row, this.choices, foot)
    container.appendChild(this.el)
  }

  // Turns the next card (only in order); true when one was turned
  turn(i: number): boolean {
    if (i !== this.turned || i >= WENLAN_READING.length) return false
    const card = WENLAN_READING[i]
    const el = this.cards[i]
    el.style.background = 'linear-gradient(#2a2218,#18130d)'
    el.style.cursor = 'default'
    el.replaceChildren()
    const place = document.createElement('div')
    place.style.cssText = 'font:11px system-ui,sans-serif;letter-spacing:0.08em;text-transform:uppercase;opacity:0.7'
    place.textContent = card.place
    const t = document.createElement('div')
    t.style.cssText = 'font:600 16px Georgia,serif;color:#e7bf6a;margin:8px 0'
    t.textContent = card.title
    const v = document.createElement('div')
    v.style.cssText = 'line-height:1.4;font-size:13px'
    v.textContent = vision(card, this.clarityNow)
    el.append(place, t, v)
    this.turned++
    if (this.turned === WENLAN_READING.length) this.choices.style.display = 'block'
    return true
  }

  // Chooses what to tell, once every card is turned
  choose(t: Told): boolean {
    if (this.turned < WENLAN_READING.length) return false
    this.el.remove()
    this.onTold(t)
    return true
  }
}

// Where the willow bough lies: the low (south-east) corner of the field
// piece furthest east, in metres, and the field's middle; null without a
// field. size is the field piece's cells, east and south (farmland's 1 × 2).
export function ditchSpot(pieces: Readonly<Record<string, { piece: string; x: number; z: number }>>, cell: number, size: readonly [number, number] = [1, 2]): { x: number; z: number; field: { x: number; z: number; w: number; d: number } } | null {
  const fields = Object.values(pieces).filter((p) => p.piece === 'field')
  if (!fields.length) return null
  const f = fields.reduce((a, b) => (b.x > a.x || (b.x === a.x && b.z > a.z) ? b : a))
  const [w, d] = size
  return { x: (f.x + w) * cell - 0.6, z: (f.z + d) * cell + 0.4, field: { x: (f.x + w / 2) * cell, z: (f.z + d / 2) * cell, w: w * cell, d: d * cell } }
}
