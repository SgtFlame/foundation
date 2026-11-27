import type { Step } from '../objectives/FirstHour.ts'

// The story game's first hour (forge 375 S6, from 375-StoryRPGPlay.md ›
// First hour): the Sheaflands farmstead on the night of the first snow, the
// first howl, the raid signature and the drag marks, the first fight at
// the barn, and the road west to Hawk's Rest, where the character rests
// and draws the first card. The steps are played in the area viewport;
// the server keeps which were reached (game.reachStory, game.story), so
// the hour resumes where the player stopped. The ids are the server's
// (features/game/Story.go StorySteps); a test checks they agree.

export interface StoryStepDef {
  id: string
  title: string
  how: string
}

export const STORY_STEPS: readonly StoryStepDef[] = [
  { id: 'arrive', title: 'The night of the first snow', how: 'Click the ground to walk. The farmhouse is warm: the hearth, hot cakes, a cup of milk on the sill.' },
  { id: 'farmer', title: 'Talk to Wenlan Harrow by the hearth', how: 'Click the farmhouse door to go in, then click Wenlan to talk.' },
  { id: 'howl', title: 'The first howl in moondark', how: 'Listen. Something howled out by the well.' },
  { id: 'signature', title: 'Find the raid signature', how: 'Go out to the well in the yard and look where the snow is scorched.' },
  { id: 'trail', title: 'Follow the drag marks', how: 'Three furrows lead west through the snow. Follow them toward the barn.' },
  { id: 'fight', title: 'The first fight', how: 'Click a Hollowed to fight it. 1 Strike, 2 Pivot, 3 Guard. Only the neck or the heart ends them.' },
  { id: 'road', title: "The road west to Hawk's Rest", how: "Follow the north road to the signpost and click it to take the road to Hawk's Rest." },
  { id: 'rest', title: 'Rest by a cold hearth', how: 'The empty cottage approves of its guest. Stand by the hearth and press R to rest and draw the first card.' },
  // S7: the first day
  { id: 'morning', title: 'The first morning', how: 'Morning comes to Hawk\'s Rest. The day\'s card is drawn.' },
  { id: 'forage', title: 'Forage for the herbal', how: 'Take the road back to the farmstead and gather the three herbs growing by the trees: thyme, nettle and yarrow. Walk up to each.' },
  { id: 'herbal', title: 'Write them in the herbal', how: 'Press K to open the herbal and read what each herb is for.' },
  // S8: the Hum, Chill and Hunger
  { id: 'stillness', title: 'Stillness', how: "Stand by a hearth (Hawk's Rest's, or the farmhouse's) and press B. Breathe in for four, out for six: each breath adds to the Hum." },
  { id: 'cook', title: 'Something hot', how: 'By a hearth, press C and cook thyme in hot milk or nettle soup from your herbs. Spending the Hum leaves your hands cold and you hungry; hot food clears it.' },
  // S9: the moon clock
  { id: 'almanac', title: 'The moon almanac', how: 'Press M to read the moon almanac: the coming nights, which are dark, and how much Hum each allows.' },
  { id: 'sleep', title: "A day's end", how: 'The day passes (a game hour each minute). When night falls at 19:00, stand by a hearth and press R to sleep until morning.' },
  // S10: a three-card reading for Wenlan
  { id: 'reading', title: 'A reading for Wenlan', how: "Wenlan Harrow's winter wheat is drowning. Go into the farmhouse kitchen by day and click Wenlan to read three cards for him." },
  { id: 'ditch', title: 'Clear the ditch', how: 'Find the willow bough the cards showed, at the low corner of the east field, and walk up to it to haul it out.' },
  { id: 'foretold', title: 'What the cards foretold', how: 'Press N to open your notes, then click the Ash card and the moon almanac: together they name the night the eyeless ones come back.' },
  { id: 'watch', title: 'The night with no moon', how: "Stand by a hearth and press R to wait for the night the notes foretold. Then keep the watch at Wenlan's farmhouse: go in by its door and listen for the hearth-watch's whistle." },
  { id: 'defended', title: 'Hold the house until dawn', how: 'One blast: the stone moved. Two: something came through. Three: the watch is about to die. Be by the hearth when they come up out of the dark.' },
  // S15: the village remembers
  { id: 'thanked', title: 'The village remembers', how: "Morning after the foretold night. Go into the farmhouse kitchen and click Wenlan: he has something for you." },
  // S16: remembering
  { id: 'dream', title: 'A dream at the ford', how: "Sleep in the empty cottage at Hawk's Rest: stand by its hearth after 19:00 and press R." },
  // S17: the first companion
  { id: 'companion', title: 'Someone to take the flank', how: "The morning after the dream, someone knocks at the cottage door at Hawk's Rest. Answer, and ask them to walk with you." },
  // S18: the Nine Sisters
  { id: 'stones', title: 'The Nine Sisters', how: 'On a night of the lesser full moon (the almanac, M, shows when), stand at the heart of the ring of nine stones by the signpost.' },
  // S19: the hedge-witch's cache
  { id: 'cache', title: "The hedge-witch's cache", how: "Back at Hawk's Rest, listen for a draught (hold F to focus your senses) and look by the box bed." },
]

// S7: the herbs a character gathers for the herbal (placeholders in the
// book's spirit: the herbal of Hawk's Rest)
export interface Herb {
  id: string
  title: string
  note: string
  color: number
}

export const HERBS: readonly Herb[] = [
  { id: 'thyme', title: 'Wild thyme', note: 'Steeped in hot milk, it warms a chilled body: good against the Chill after spending the Hum.', color: 0x9b7fc4 },
  { id: 'nettle', title: 'Nettle', note: 'Gloves first. Boiled, it makes a soup that keeps hunger off through a long watch.', color: 0x4e8a3a },
  { id: 'yarrow', title: 'Yarrow', note: 'Pressed into a cut, it stops the bleeding. The old name is soldier\'s woundwort.', color: 0xe9e4c8 },
]

// Where the herbs grow in an area: by its trees and copses, one herb at
// each of the first three, a few metres south of the tree, in the order
// HERBS lists them. Spot ids are stable for a layout (the piece's id).
export function herbSpots(pieces: Readonly<Record<string, { id: string; piece: string; x: number; z: number }>>, cell: number): { spot: string; herb: Herb; x: number; z: number }[] {
  const trees = Object.values(pieces)
    .filter((p) => p.piece === 'tree' || p.piece === 'copse')
    .sort((a, b) => a.z - b.z || a.x - b.x)
  return trees.slice(0, HERBS.length).map((t, i) => ({ spot: t.id, herb: HERBS[i], x: (t.x + 0.5) * cell, z: (t.z + 1) * cell + 1.5 }))
}

// The herbal (S7): the herbs gathered, with what each is for. Pure DOM.
export function showHerbal(container: HTMLElement, items: Readonly<Record<string, number>>): HTMLDivElement {
  const el = document.createElement('div')
  el.dataset.herbal = ''
  el.style.cssText =
    'position:absolute;left:50%;top:50%;transform:translate(-50%,-50%);z-index:4;width:min(420px,calc(100% - 32px));padding:16px 18px;border-radius:8px;cursor:pointer;' +
    'background:#efe4c9;border:1px solid #8a6d43;box-shadow:0 6px 30px rgba(0,0,0,0.6);font:14px Georgia,serif;color:#3b2c1a'
  const h = document.createElement('div')
  h.style.cssText = 'font:600 17px Georgia,serif;margin-bottom:8px'
  h.textContent = "The herbal of Hawk's Rest"
  el.append(h)
  for (const herb of HERBS) {
    const n = items[herb.id] ?? 0
    const row = document.createElement('div')
    row.dataset.herb = herb.id
    row.style.cssText = `margin:6px 0;opacity:${n ? 1 : 0.45}`
    row.innerHTML = ''
    const t = document.createElement('div')
    t.style.cssText = 'font-weight:600'
    t.textContent = `${herb.title}${n ? ` (${n})` : ' (not found yet)'}`
    const d = document.createElement('div')
    d.style.cssText = 'font-style:italic;line-height:1.35'
    d.textContent = n ? herb.note : '…'
    row.append(t, d)
    el.append(row)
  }
  const foot = document.createElement('div')
  foot.style.cssText = 'margin-top:10px;font:11px system-ui,sans-serif;opacity:0.6'
  foot.textContent = 'Click or press K to close.'
  el.append(foot)
  el.addEventListener('click', () => el.remove())
  container.appendChild(el)
  return el
}

// The steps with which are done, a later one counting the earlier
export function storySteps(done: Readonly<Record<string, boolean>>): Step[] {
  const steps = STORY_STEPS.map((s) => ({ ...s, done: !!done[s.id] }))
  for (let k = steps.length - 1, later = false; k >= 0; k--) {
    if (later) steps[k].done = true
    later ||= steps[k].done
  }
  return steps
}

// The step to play next, or null when the hour is over
export function nextStep(done: Readonly<Record<string, boolean>>): string | null {
  return storySteps(done).find((s) => !s.done)?.id ?? null
}

// What the raid signature tells a character who looks (the senses layer, thin)
export const SIGNATURE_TEXT =
  'Burnt sugar and old iron. The snow by the well is scorched in a ring, frost-black handprints on the stones, ' +
  "and the Brannocks' lantern lies where it fell. Three furrows lead west, toward the barn."

// The lesser deck the cottage keeps (the user: the book's artifacts stay
// with the cast; the player finds lesser decks). Placeholder cards; the
// first draw sets the tone for the next day.
export interface Card {
  id: string
  title: string
  meaning: string
}

export const LESSER_DECK: readonly Card[] = [
  { id: 'hearth', title: 'The Hearth', meaning: 'Warmth kept is warmth given. Tomorrow, food sits better and the Chill lifts sooner.' },
  { id: 'lantern', title: 'The Lantern', meaning: 'A light carried into the dark. Tomorrow, the first thing hidden near you shows itself.' },
  { id: 'hare', title: 'The Hare', meaning: 'Quick feet, quick fear. Tomorrow, you outrun what you cannot fight.' },
  { id: 'frost', title: 'The Frost', meaning: 'A warning: the cold comes in by the door you leave open.' },
  { id: 'wren', title: 'The Wren', meaning: 'Small and loud. Tomorrow, someone you helped speaks for you.' },
]

// The first card drawn, the same for the same seed
export function drawCard(seed: number): Card {
  // a full integer mix, so seeds a day apart draw unrelated cards (S11)
  let h = (seed | 0) ^ 0x9e3779b9
  h = Math.imul(h ^ (h >>> 16), 0x85ebca6b)
  h = Math.imul(h ^ (h >>> 13), 0xc2b2ae35)
  h ^= h >>> 16
  const i = (h >>> 0) % LESSER_DECK.length
  return LESSER_DECK[i]
}

// The marks the raiders left: a ring at the signature and drag marks from
// it to a point (the barn's door), every 1.5 m, wavering a little
export function dragMarks(from: { x: number; z: number }, to: { x: number; z: number }, seed = 1): { x: number; z: number; heading: number }[] {
  const dx = to.x - from.x
  const dz = to.z - from.z
  const len = Math.hypot(dx, dz)
  if (len < 1) return []
  const heading = Math.atan2(dx, dz)
  const out: { x: number; z: number; heading: number }[] = []
  for (let d = 2; d < len - 2; d += 1.5) {
    const t = d / len
    const wobble = Math.sin(d * 0.7 + seed) * 0.35
    out.push({ x: from.x + dx * t + Math.cos(heading) * wobble, z: from.z + dz * t - Math.sin(heading) * wobble, heading })
  }
  return out
}

// A howl in the distance (the first howl in moondark), made with Web Audio
// like the whistle: a low tone rising and falling, through a little noise
let ctx: AudioContext | null = null

export function howl(volume = 0.1): void {
  try {
    ctx ??= new AudioContext()
    if (ctx.state === 'suspended') void ctx.resume()
    const t0 = ctx.currentTime + 0.05
    const osc = ctx.createOscillator()
    const gain = ctx.createGain()
    osc.type = 'sawtooth'
    osc.frequency.setValueAtTime(220, t0)
    osc.frequency.linearRampToValueAtTime(410, t0 + 0.9)
    osc.frequency.linearRampToValueAtTime(380, t0 + 1.8)
    osc.frequency.linearRampToValueAtTime(190, t0 + 2.6)
    const filter = ctx.createBiquadFilter()
    filter.type = 'lowpass'
    filter.frequency.value = 900
    gain.gain.setValueAtTime(0, t0)
    gain.gain.linearRampToValueAtTime(volume, t0 + 0.4)
    gain.gain.setValueAtTime(volume, t0 + 1.9)
    gain.gain.linearRampToValueAtTime(0, t0 + 2.7)
    osc.connect(filter).connect(gain).connect(ctx.destination)
    osc.start(t0)
    osc.stop(t0 + 2.8)
  } catch {
    // no audio here
  }
}

// The card drawn at the cottage's hearth, shown over the view until clicked
export function showCard(container: HTMLElement, card: Card): HTMLDivElement {
  const el = document.createElement('div')
  el.dataset.card = card.id
  el.style.cssText =
    'position:absolute;left:50%;top:50%;transform:translate(-50%,-50%);z-index:4;width:220px;padding:18px 16px;border-radius:10px;cursor:pointer;' +
    'background:linear-gradient(#2a2218,#18130d);border:2px solid #b08a4a;box-shadow:0 6px 30px rgba(0,0,0,0.7);' +
    'font:14px Georgia,serif;color:#efe2c4;text-align:center'
  const title = document.createElement('div')
  title.style.cssText = 'font:600 18px Georgia,serif;color:#e7bf6a;margin:6px 0 12px'
  title.textContent = card.title
  const meaning = document.createElement('div')
  meaning.style.cssText = 'line-height:1.45'
  meaning.textContent = card.meaning
  const foot = document.createElement('div')
  foot.style.cssText = 'margin-top:14px;font:11px system-ui,sans-serif;opacity:0.6'
  foot.textContent = 'The first card. Click to put it away.'
  el.append(title, meaning, foot)
  el.addEventListener('click', () => el.remove())
  container.appendChild(el)
  return el
}
