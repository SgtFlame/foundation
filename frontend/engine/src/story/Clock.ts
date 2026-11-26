import { moons, type MoonPhase } from '../sky/Moons.ts'
import { humPool } from './Hum.ts'

// The story's clock and the moon almanac (forge 375 S9; the design's rule 1
// and the play design's day and night loop): a day of about 24 real
// minutes, a game hour a real minute. The story starts on the night of the
// first snow, calendar night 11, at nine in the evening; a night belongs to
// the evening it starts in, so the small hours count as the night before.

export interface Clock {
  day: number // days since the night of the first snow
  hour: number // 0 to 24
}

export const FIRST_NIGHT = 11
export const CLOCK_START: Clock = { day: 0, hour: 21 }
export const SECONDS_PER_HOUR = 60
export const DAWN = 6
export const DUSK = 19
export const WAKE = 7

export function stepClock(c: Clock, dt: number): Clock {
  let hour = c.hour + dt / SECONDS_PER_HOUR
  let day = c.day
  while (hour >= 24) {
    hour -= 24
    day++
  }
  return { day, hour }
}

export function isDay(c: Clock): boolean {
  return c.hour >= DAWN && c.hour < DUSK
}

// The calendar night the moons follow
export function nightOf(c: Clock): number {
  return FIRST_NIGHT + c.day - (c.hour < DAWN ? 1 : 0)
}

// Sleeping by a hearth: wake at seven the next morning (the same morning in the small hours)
export function sleep(c: Clock): Clock {
  return { day: c.hour < DAWN ? c.day : c.day + 1, hour: WAKE }
}

// Night falls or is falling: a hearth will let you sleep
export function canSleep(c: Clock): boolean {
  return c.hour >= DUSK - 1 || c.hour < DAWN
}

export function clockText(c: Clock): string {
  const h = Math.floor(c.hour)
  const m = Math.floor((c.hour - h) * 60)
  return `Day ${c.day + 1} · ${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}${isDay(c) ? '' : ' · night'}`
}

// A moon's glyph for its phase
export function moonGlyph(m: MoonPhase): string {
  const glyphs = ['🌑', '🌒', '🌓', '🌔', '🌕', '🌖', '🌗', '🌘']
  return glyphs[Math.round(m.phase * 8) % 8]
}

export interface AlmanacRow {
  night: number
  greater: MoonPhase
  lesser: MoonPhase
  hum: number
  note: string
}

// What a night's sky means (the design's rule 1, the rows the game uses so far)
export function skyNote(night: number): string {
  const m = moons(night)
  // both full (or dark) within half a night: rare, as the book has it
  const near = (p: number, at: number, period: number) => Math.abs(p - at) <= 0.5 / period + 1e-9 || Math.abs(p - at - 1) <= 0.5 / period + 1e-9
  if (near(m.greater.phase, 0.5, 30) && near(m.lesser.phase, 0.5, 13)) return 'The Doubling: peak power'
  if (near(m.greater.phase, 0, 30) && near(m.lesser.phase, 0, 13)) return 'The Double Dark'
  if (m.greater.lit < 0.1) return 'Moondark: the Hollowed roam openly'
  if (m.lesser.lit > 0.9) return 'Lesser full: cards read clearest'
  if (m.lesser.lit < 0.1) return 'Lesser dark: a cold place under the breastbone'
  if (!m.greater.waxing && m.greater.lit < 0.4) return 'Greater waning: howls come nearer'
  if (m.greater.waxing && m.greater.lit > 0.6) return 'Greater waxing: the Hollowed keep away'
  return ''
}

// The next n nights from a night
export function almanac(from: number, n = 14): AlmanacRow[] {
  return Array.from({ length: n }, (_, i) => {
    const night = from + i
    const m = moons(night)
    return { night, greater: m.greater, lesser: m.lesser, hum: humPool(night), note: skyNote(night) }
  })
}

// The almanac's page, styled as the herbal is
export function showAlmanac(container: HTMLElement, clock: Clock): HTMLDivElement {
  const el = document.createElement('div')
  el.dataset.almanac = ''
  el.style.cssText = 'position:absolute;left:50%;top:50%;transform:translate(-50%,-50%);width:440px;max-height:calc(100% - 40px);overflow-y:auto;padding:16px 18px;border-radius:6px;background:#efe3c8;color:#3a2a18;font:13px Georgia,serif;box-shadow:0 8px 30px rgba(0,0,0,.6);z-index:4;box-sizing:border-box'
  const h = document.createElement('div')
  h.textContent = 'The moon almanac'
  h.style.cssText = 'font-size:16px;font-weight:bold'
  const sub = document.createElement('div')
  sub.textContent = `${clockText(clock)}. The greater moon runs 30 nights, the lesser 13. Plan journeys and watches by them.`
  sub.style.cssText = 'font-style:italic;margin:4px 0 8px'
  el.append(h, sub)
  const tonight = nightOf(clock)
  for (const r of almanac(tonight)) {
    const row = document.createElement('div')
    row.dataset.night = String(r.night)
    row.style.cssText = `display:flex;gap:8px;align-items:center;padding:3px 0;border-top:1px solid #d8c8a4${r.night === tonight ? ';font-weight:bold' : ''}`
    const when = document.createElement('span')
    when.style.width = '74px'
    when.textContent = r.night === tonight ? 'Tonight' : r.night === tonight + 1 ? 'Tomorrow' : `In ${r.night - tonight} nights`
    const sky = document.createElement('span')
    sky.style.cssText = 'width:52px;font-size:15px'
    sky.textContent = moonGlyph(r.greater) + moonGlyph(r.lesser)
    sky.title = `Greater ${Math.round(r.greater.lit * 100)}% lit, lesser ${Math.round(r.lesser.lit * 100)}%`
    const hum = document.createElement('span')
    hum.style.cssText = 'width:56px;color:#7a5a10'
    hum.textContent = '◆'.repeat(r.hum)
    const note = document.createElement('span')
    note.style.cssText = 'flex:1;font-style:italic'
    note.textContent = r.note
    row.append(when, sky, hum, note)
    el.appendChild(row)
  }
  const close = document.createElement('div')
  close.textContent = '◆ the Hum the sky allows that night · press M to close'
  close.style.cssText = 'margin-top:8px;font-size:11px;opacity:.7'
  el.appendChild(close)
  container.appendChild(el)
  return el
}
