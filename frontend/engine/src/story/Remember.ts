import type { Skills } from '../combat/Combat.ts'
import type { Body } from './Hum.ts'

// Remembering (forge 375 S16; the play design's reference rule 8):
// "sleeping in blessed places unlocks a skill or a memory scene outright.
// Dreams replay past-life fights; wounds taken there ache (dream wounds)
// until a warm, dreamless night." The cottage at Hawk's Rest, which
// approved of its guest the first night, is the first blessed place: after
// the village's thanks, a night there brings the dream of a fight at a
// frozen ford. The character wakes with the old hands' feel for the long
// spear (polearm +1) and an ache in the side that keeps a little of the
// Chill until they sleep warm (something hot eaten since waking).

export interface DreamScene {
  title: string
  lines: readonly string[]
}

export const FORD_DREAM: DreamScene = {
  title: 'A dream: the ford',
  lines: [
    'Ice on the river, and torches on the far bank. You are older, broader, and you hold a long spear as if you were born with it.',
    'They come across the ice without a sound. The man beside you breaks and runs; you plant the butt of the spear in the frozen mud and let the first of them come onto the point.',
    'Something rakes your side. You feel it go in, cold, and you do not let go of the spear.',
    'You wake by the cold hearth with your hand closed on nothing, and your side aching where it went in.',
  ],
}

export const WOUND_CHILL = 30 // the Chill a dream wound will not let fall below
export const DREAM_SKILL: keyof Skills = 'polearm'

// The character's skills once the ford is remembered
export function remembered(base: Skills, done: Readonly<Record<string, boolean>>): Skills {
  return done.dream ? { ...base, [DREAM_SKILL]: Math.min(5, base[DREAM_SKILL] + 1) } : base
}

// The body while a dream wound aches: the Chill does not fade below the ache
export function aching(b: Body, wounded: boolean): Body {
  return wounded && b.chill < WOUND_CHILL ? { ...b, chill: WOUND_CHILL } : b
}

// Whether the night's sleep brings the dream: in a blessed place, after the thanks, once
export function dreams(done: Readonly<Record<string, boolean>>, blessed: boolean): boolean {
  return blessed && !!done.thanked && !done.dream
}

// The dream on a dark panel, a line at a time; click or a key goes on, and
// done() runs after the last
export function showDream(container: HTMLElement, scene: DreamScene, done: () => void): HTMLDivElement {
  const el = document.createElement('div')
  el.dataset.dream = ''
  el.style.cssText =
    'position:absolute;inset:0;z-index:4;background:radial-gradient(ellipse at center,#1a2233 0%,#05070b 75%);color:#cfd8e6;display:flex;flex-direction:column;align-items:center;justify-content:center;padding:40px;font:16px/1.6 Georgia,serif;cursor:pointer;text-align:center'
  const title = document.createElement('div')
  title.style.cssText = 'font:italic 13px system-ui,sans-serif;letter-spacing:0.2em;text-transform:uppercase;color:#8fa3c0;margin-bottom:18px'
  title.textContent = scene.title
  const text = document.createElement('div')
  text.style.cssText = 'max-width:560px;min-height:5em;transition:opacity 0.6s'
  const more = document.createElement('div')
  more.style.cssText = 'font:12px system-ui,sans-serif;color:#6f7f99;margin-top:18px'
  el.append(title, text, more)
  let i = 0
  const show = () => {
    text.textContent = scene.lines[i]
    more.textContent = i < scene.lines.length - 1 ? 'Click to go on' : 'Click to wake'
  }
  const next = () => {
    i++
    if (i >= scene.lines.length) {
      el.remove()
      window.removeEventListener('keydown', key)
      done()
      return
    }
    show()
  }
  const key = (e: KeyboardEvent) => {
    if (e.key === ' ' || e.key === 'Enter' || e.key === 'Escape') next()
  }
  el.addEventListener('click', next)
  window.addEventListener('keydown', key)
  show()
  container.appendChild(el)
  return el
}
