// A small frame-time overlay in the corner of the viewport: plain DOM, no
// React, so the engine can show it anywhere. Rows are set by name.
export class StatsOverlay {
  readonly element: HTMLDivElement
  private readonly rows = new Map<string, HTMLDivElement>()

  constructor(parent: HTMLElement) {
    const el = document.createElement('div')
    el.style.cssText =
      'position:absolute;top:8px;left:8px;padding:6px 8px;font:12px/1.35 ui-monospace,monospace;' +
      'color:#d8f3dc;background:rgba(0,0,0,0.55);border-radius:4px;pointer-events:none;white-space:pre;z-index:2'
    parent.appendChild(el)
    this.element = el
  }

  set(name: string, value: string): void {
    let row = this.rows.get(name)
    if (!row) {
      row = document.createElement('div')
      this.element.appendChild(row)
      this.rows.set(name, row)
    }
    const text = `${name.padEnd(9)} ${value}`
    if (row.textContent !== text) row.textContent = text
  }

  dispose(): void {
    this.element.remove()
  }
}
