// A tin whistle's blasts (forge 375 S5), made with Web Audio so no sound
// file is needed: a high, slightly wavering tone, one short blast per
// signal with a breath between. Quiet when the browser has no audio or has
// not been clicked yet.

let ctx: AudioContext | null = null

export function whistle(blasts: number, volume = 0.12): void {
  try {
    ctx ??= new AudioContext()
    if (ctx.state === 'suspended') void ctx.resume()
    const t0 = ctx.currentTime + 0.05
    for (let i = 0; i < blasts; i++) {
      const at = t0 + i * 0.55
      const osc = ctx.createOscillator()
      const wobble = ctx.createOscillator()
      const depth = ctx.createGain()
      const gain = ctx.createGain()
      osc.type = 'triangle'
      osc.frequency.value = 1760
      wobble.frequency.value = 6
      depth.gain.value = 18
      wobble.connect(depth).connect(osc.frequency)
      gain.gain.setValueAtTime(0, at)
      gain.gain.linearRampToValueAtTime(volume, at + 0.03)
      gain.gain.setValueAtTime(volume, at + 0.32)
      gain.gain.linearRampToValueAtTime(0, at + 0.4)
      osc.connect(gain).connect(ctx.destination)
      osc.start(at)
      wobble.start(at)
      osc.stop(at + 0.42)
      wobble.stop(at + 0.42)
    }
  } catch {
    // no audio here
  }
}
