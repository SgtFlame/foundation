// The hearth-defence panel (forge 375 S5): time to dawn, the hearth-watch's
// health, whether a barrel weighs the stone down, and the last signal heard.
// Pure DOM; the viewport owns it.
export class NightHud {
  private readonly el: HTMLDivElement
  private readonly clock: HTMLDivElement
  private readonly watch: HTMLDivElement
  private readonly signal: HTMLDivElement

  constructor(container: HTMLElement) {
    this.el = document.createElement('div')
    this.el.dataset.nightHud = 'true'
    this.el.style.cssText =
      'position:absolute;top:64px;left:50%;transform:translateX(-50%);z-index:3;display:none;min-width:280px;max-width:440px;padding:6px 12px;border-radius:6px;' +
      'background:rgba(12,14,22,0.88);border:1px solid #3a4a6a;font:12px system-ui,sans-serif;color:#dfe6f3;text-align:center;pointer-events:none'
    this.clock = document.createElement('div')
    this.clock.style.cssText = 'font:600 13px Georgia,serif;letter-spacing:0.02em'
    this.watch = document.createElement('div')
    this.watch.style.cssText = 'opacity:0.85;margin-top:2px'
    this.signal = document.createElement('div')
    this.signal.dataset.signal = ''
    this.signal.style.cssText = 'margin-top:4px;color:#ffd27a;font:600 13px Georgia,serif'
    this.el.append(this.clock, this.watch, this.signal)
    container.appendChild(this.el)
  }

  show(on: boolean): void {
    this.el.style.display = on ? 'block' : 'none'
    if (!on) this.signal.textContent = ''
  }

  update(left: number, watcher: { hp: number; max: number } | null, barrel: boolean, inside: number): void {
    const m = Math.floor(left / 60)
    const s = Math.floor(left % 60)
    this.clock.textContent = left > 0 ? `Hearth defence · ${m}:${String(s).padStart(2, '0')} to dawn` : 'Hearth defence · dawn'
    const parts = [watcher ? `Hearth-watch ${Math.ceil(watcher.hp)} / ${watcher.max}` : 'No hearth-watch in the house']
    parts.push(barrel ? 'a barrel on the hearthstone' : 'nothing on the hearthstone')
    if (inside) parts.push(`${inside} came through`)
    this.watch.textContent = parts.join(' · ')
  }

  say(text: string): void {
    this.signal.textContent = text
  }

  dispose(): void {
    this.el.remove()
  }
}
