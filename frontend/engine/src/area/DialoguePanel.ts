import type { Dialogue } from './PieceSet.ts'

// The talk panel (forge 375 S3): the speaker's line and the player's
// answers as buttons along the bottom of the view, in the style of a
// classic top-down RPG. Number keys 1 to 9 pick an answer and Escape ends
// the talk. It knows nothing of the server: the viewport gives it a
// dialogue and it calls back as nodes change and when the talk ends.
export class DialoguePanel {
  private readonly el: HTMLDivElement
  private dialogue: Dialogue | null = null
  private node = ''
  private readonly onEnd: () => void

  constructor(container: HTMLElement, onEnd: () => void) {
    this.onEnd = onEnd
    this.el = document.createElement('div')
    this.el.dataset.dialogue = 'true'
    this.el.style.cssText =
      'position:absolute;left:50%;bottom:44px;transform:translateX(-50%);width:min(640px,calc(100% - 32px));z-index:3;display:none;' +
      'background:rgba(16,14,12,0.92);border:1px solid #6b5a43;border-radius:6px;padding:12px 14px;' +
      'font:14px Georgia,serif;color:#eadfc8;box-shadow:0 4px 18px rgba(0,0,0,0.5)'
    container.appendChild(this.el)
    window.addEventListener('keydown', this.onKey)
  }

  get open(): boolean {
    return this.dialogue !== null
  }

  // Where the talk is: the dialogue's id and node, or null
  state(): { dialogue: string; node: string } | null {
    return this.dialogue ? { dialogue: this.dialogue.id, node: this.node } : null
  }

  start(d: Dialogue, name?: string): void {
    this.dialogue = name ? { ...d, speaker: name } : d
    this.show(d.start)
  }

  // Picks the i-th answer of the node shown (0 first)
  choose(i: number): void {
    const d = this.dialogue
    const n = d?.nodes[this.node]
    const c = n?.choices[i]
    if (!d || !c) return
    if (c.next && d.nodes[c.next]) this.show(c.next)
    else this.end()
  }

  end(): void {
    if (!this.dialogue) return
    this.dialogue = null
    this.el.style.display = 'none'
    this.onEnd()
  }

  dispose(): void {
    window.removeEventListener('keydown', this.onKey)
    this.el.remove()
  }

  private show(id: string): void {
    const d = this.dialogue
    const n = d?.nodes[id]
    if (!d || !n) return this.end()
    this.node = id
    this.el.replaceChildren()
    const who = document.createElement('div')
    who.textContent = d.speaker
    who.style.cssText = 'font:600 12px system-ui,sans-serif;letter-spacing:0.06em;text-transform:uppercase;color:#d8a85a;margin-bottom:6px'
    const text = document.createElement('div')
    text.dataset.dialogueText = 'true'
    text.textContent = n.text
    text.style.cssText = 'line-height:1.45;margin-bottom:10px'
    this.el.append(who, text)
    const choices = n.choices.length ? n.choices : [{ text: 'Farewell.' }]
    choices.forEach((c, i) => {
      const b = document.createElement('button')
      b.dataset.choice = String(i)
      b.textContent = `${i + 1}. ${c.text}`
      b.style.cssText =
        'display:block;width:100%;text-align:left;margin:3px 0;padding:5px 8px;border:1px solid #4a3f30;border-radius:4px;' +
        'background:rgba(255,255,255,0.04);color:#f3e7cf;font:14px Georgia,serif;cursor:pointer'
      b.onmouseenter = () => (b.style.background = 'rgba(216,168,90,0.18)')
      b.onmouseleave = () => (b.style.background = 'rgba(255,255,255,0.04)')
      b.onclick = (e) => {
        e.stopPropagation()
        this.choose(i)
      }
      this.el.append(b)
    })
    this.el.style.display = 'block'
  }

  private onKey = (e: KeyboardEvent) => {
    if (!this.dialogue) return
    if (e.key === 'Escape') {
      this.end()
      e.preventDefault()
    } else if (/^[1-9]$/.test(e.key)) {
      this.choose(Number(e.key) - 1)
      e.preventDefault()
    }
  }
}
