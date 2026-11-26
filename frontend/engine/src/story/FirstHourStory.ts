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
]

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
  const i = Math.abs(Math.imul(seed | 0, 2654435761) >>> 0) % LESSER_DECK.length
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
