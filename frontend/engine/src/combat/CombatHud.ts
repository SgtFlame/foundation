import type { Stance } from './Combat.ts'

// The fight's panel (forge 375 S4): the character's health, the three
// stances as buttons (keys 1, 2 and 3), the weapon in hand and what just
// happened. Pure DOM, no React; the viewport owns it.
export class CombatHud {
  private readonly el: HTMLDivElement
  private readonly bar: HTMLDivElement
  private readonly hpText: HTMLSpanElement
  private readonly buttons = new Map<Stance, HTMLButtonElement>()
  private readonly weaponEl: HTMLDivElement
  private readonly log: HTMLDivElement

  constructor(container: HTMLElement, onStance: (s: Stance) => void) {
    this.el = document.createElement('div')
    this.el.dataset.combat = 'true'
    this.el.style.cssText =
      'position:absolute;left:10px;bottom:44px;z-index:3;display:none;width:250px;padding:8px 10px;border-radius:6px;' +
      'background:rgba(16,14,12,0.88);border:1px solid #5a3a30;font:12px system-ui,sans-serif;color:#eadfc8'
    const top = document.createElement('div')
    top.style.cssText = 'display:flex;justify-content:space-between;margin-bottom:4px'
    const label = document.createElement('span')
    label.textContent = 'Health'
    this.hpText = document.createElement('span')
    top.append(label, this.hpText)
    const track = document.createElement('div')
    track.style.cssText = 'height:6px;background:#2a1f1a;border-radius:3px;overflow:hidden;margin-bottom:8px'
    this.bar = document.createElement('div')
    this.bar.style.cssText = 'height:100%;background:#c0533a;width:100%;transition:width 0.2s'
    track.append(this.bar)
    const row = document.createElement('div')
    row.style.cssText = 'display:flex;gap:4px;margin-bottom:6px'
    ;(['strike', 'pivot', 'guard'] as Stance[]).forEach((s, i) => {
      const b = document.createElement('button')
      b.dataset.stance = s
      b.textContent = `${i + 1} ${s[0].toUpperCase()}${s.slice(1)}`
      b.style.cssText = 'flex:1;padding:4px 0;border-radius:4px;border:1px solid #4a3f30;background:transparent;color:#f3e7cf;font:12px system-ui,sans-serif;cursor:pointer'
      b.onclick = (e) => {
        e.stopPropagation()
        onStance(s)
      }
      this.buttons.set(s, b)
      row.append(b)
    })
    this.weaponEl = document.createElement('div')
    this.weaponEl.style.cssText = 'color:#b9ad94;margin-bottom:4px'
    this.log = document.createElement('div')
    this.log.dataset.combatLog = 'true'
    this.log.style.cssText = 'min-height:3.6em;line-height:1.2em;color:#d8c9a8;font:12px Georgia,serif'
    this.el.append(top, track, row, this.weaponEl, this.log)
    container.appendChild(this.el)
  }

  show(on: boolean): void {
    this.el.style.display = on ? 'block' : 'none'
    if (!on) this.log.replaceChildren()
  }

  update(hp: number, max: number, stance: Stance, weapon: string, skill: string): void {
    this.hpText.textContent = `${Math.max(0, hp)} / ${max}`
    this.bar.style.width = `${Math.max(0, (hp / max) * 100)}%`
    for (const [s, b] of this.buttons) {
      b.style.background = s === stance ? 'rgba(216,168,90,0.28)' : 'transparent'
      b.style.borderColor = s === stance ? '#d8a85a' : '#4a3f30'
    }
    this.weaponEl.textContent = `${weapon} · ${skill}`
  }

  // A line of what happened, newest last, three kept
  say(text: string): void {
    const line = document.createElement('div')
    line.textContent = text
    this.log.append(line)
    while (this.log.childElementCount > 3) this.log.firstElementChild?.remove()
  }

  dispose(): void {
    this.el.remove()
  }
}
