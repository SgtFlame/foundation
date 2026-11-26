// The engine's loop: a fixed-step simulation (60 Hz by default) driven by
// requestAnimationFrame, with rendering once per frame given how far the
// clock is between two steps (alpha), so motion is smooth at any refresh
// rate and the simulation is reproducible.

export interface LoopCallbacks {
  // One simulation step of dt seconds
  step?(dt: number): void
  // Draw a frame; alpha in [0, 1) is the fraction of a step since the last
  render?(alpha: number, frameDt: number): void
}

export interface LoopStats {
  // Smoothed time between frames and work per frame (ms)
  frameMs: number
  workMs: number
  fps: number
  // Worst frame time over the last second (ms)
  worstMs: number
  steps: number
}

export class Loop {
  readonly stepSeconds: number
  private readonly maxSteps: number
  private acc = 0
  private last = 0
  private handle = 0
  private running = false
  private frameMs = 16.7
  private workMs = 0
  private worst = 0
  private worstWindow: number[] = []
  private steps = 0
  // Wall time of frames over the last second, for the fps count
  private frames: number[] = []

  private readonly callbacks: LoopCallbacks

  constructor(callbacks: LoopCallbacks, hz = 60, maxStepsPerFrame = 5) {
    this.callbacks = callbacks
    this.stepSeconds = 1 / hz
    this.maxSteps = maxStepsPerFrame
  }

  start(): void {
    if (this.running) return
    this.running = true
    this.last = performance.now()
    this.handle = requestAnimationFrame(this.tick)
  }

  stop(): void {
    this.running = false
    cancelAnimationFrame(this.handle)
  }

  private tick = (now: number) => {
    if (!this.running) return
    this.handle = requestAnimationFrame(this.tick)
    const dtMs = Math.min(250, now - this.last)
    this.last = now
    const t0 = performance.now()
    this.acc += dtMs / 1000
    let n = 0
    while (this.acc >= this.stepSeconds && n < this.maxSteps) {
      this.callbacks.step?.(this.stepSeconds)
      this.acc -= this.stepSeconds
      n++
      this.steps++
    }
    // Too far behind: drop the backlog rather than spiral
    if (n === this.maxSteps) this.acc = 0
    this.callbacks.render?.(this.acc / this.stepSeconds, dtMs / 1000)
    const work = performance.now() - t0
    this.frameMs += (dtMs - this.frameMs) * 0.1
    this.workMs += (work - this.workMs) * 0.1
    this.frames.push(now)
    this.worstWindow.push(dtMs)
    while (this.frames.length > 0 && now - this.frames[0] > 1000) {
      this.frames.shift()
      this.worstWindow.shift()
    }
    this.worst = Math.max(...this.worstWindow)
  }

  stats(): LoopStats {
    return { frameMs: this.frameMs, workMs: this.workMs, fps: this.frames.length, worstMs: this.worst, steps: this.steps }
  }
}
