// Keyboard and mouse for the engine. Holds what is down, gathers mouse
// motion while the pointer is locked to the canvas, and records presses
// so a fixed step sees each press once (pressed / clicked), however many
// frames pass between steps.

export class Input {
  private readonly down = new Set<string>()
  private readonly pressedSet = new Set<string>()
  private readonly buttons = new Set<number>()
  private readonly clickedSet = new Set<number>()
  private dx = 0
  private dy = 0
  private wheelDelta = 0
  private readonly off: (() => void)[] = []
  // Pointer position over the element, in CSS pixels
  pointerX = 0
  pointerY = 0

  private readonly element: HTMLElement

  constructor(element: HTMLElement) {
    this.element = element
    const on = <K extends keyof WindowEventMap>(t: EventTarget, type: K, fn: (e: WindowEventMap[K]) => void, opts?: AddEventListenerOptions) => {
      t.addEventListener(type, fn as EventListener, opts)
      this.off.push(() => t.removeEventListener(type, fn as EventListener, opts))
    }
    on(window, 'keydown', (e) => {
      if (!this.active()) return
      if (!e.repeat) this.pressedSet.add(e.code)
      this.down.add(e.code)
      if (this.locked) e.preventDefault()
    })
    on(window, 'keyup', (e) => this.down.delete(e.code))
    on(window, 'blur', () => {
      this.down.clear()
      this.buttons.clear()
    })
    on(element, 'mousedown', (e) => {
      this.buttons.add(e.button)
      this.clickedSet.add(e.button)
    })
    on(window, 'mouseup', (e) => this.buttons.delete(e.button))
    on(element, 'contextmenu', (e) => e.preventDefault())
    on(window, 'mousemove', (e) => {
      if (this.locked) {
        this.dx += e.movementX
        this.dy += e.movementY
      }
      const r = element.getBoundingClientRect()
      this.pointerX = e.clientX - r.left
      this.pointerY = e.clientY - r.top
    })
    on(element, 'wheel', (e) => {
      this.wheelDelta += Math.sign(e.deltaY)
      e.preventDefault()
    }, { passive: false })
  }

  // Keys count only while the pointer is locked or the canvas has focus,
  // so typing in a panel beside the viewport does not fly the camera
  private active(): boolean {
    return this.locked || document.activeElement === this.element
  }

  get locked(): boolean {
    return document.pointerLockElement === this.element
  }

  lock(): void {
    if (!this.locked) void this.element.requestPointerLock()
  }

  unlock(): void {
    if (this.locked) document.exitPointerLock()
  }

  isDown(code: string): boolean {
    return this.down.has(code)
  }

  // Pressed since the last endStep()
  pressed(code: string): boolean {
    return this.pressedSet.has(code)
  }

  isButtonDown(button: number): boolean {
    return this.buttons.has(button)
  }

  clicked(button: number): boolean {
    return this.clickedSet.has(button)
  }

  // Mouse motion and wheel steps since the last call
  takeMouse(): { dx: number; dy: number } {
    const r = { dx: this.dx, dy: this.dy }
    this.dx = 0
    this.dy = 0
    return r
  }

  takeWheel(): number {
    const w = this.wheelDelta
    this.wheelDelta = 0
    return w
  }

  // Call after each fixed step that read pressed() and clicked()
  endStep(): void {
    this.pressedSet.clear()
    this.clickedSet.clear()
  }

  dispose(): void {
    for (const f of this.off) f()
  }
}
