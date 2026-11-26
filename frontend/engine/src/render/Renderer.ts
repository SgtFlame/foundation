import * as THREE from 'three/webgpu'

export type RenderBackend = 'webgpu' | 'webgl2'

export interface RendererOptions {
  // Use WebGL 2 even where WebGPU is available
  forceWebGL?: boolean
  antialias?: boolean
  // Keep the drawing buffer readable after a frame (screenshots)
  preserveDrawingBuffer?: boolean
}

// The engine's renderer adapter over three.js WebGPURenderer, which uses
// WebGPU where the browser has it and WebGL 2 elsewhere. The engine owns
// the scene; three.js is only its view.
export class Renderer {
  readonly three: THREE.WebGPURenderer
  readonly backend: RenderBackend
  private readonly observer: ResizeObserver

  readonly canvas: HTMLCanvasElement

  private constructor(canvas: HTMLCanvasElement, three: THREE.WebGPURenderer) {
    this.canvas = canvas
    this.three = three
    const b = (three.backend as unknown as { isWebGPUBackend?: boolean }).isWebGPUBackend
    this.backend = b ? 'webgpu' : 'webgl2'
    this.observer = new ResizeObserver(() => this.resize())
    this.observer.observe(canvas)
    this.resize()
  }

  static async create(canvas: HTMLCanvasElement, opts: RendererOptions = {}): Promise<Renderer> {
    const three = new THREE.WebGPURenderer({
      canvas,
      antialias: opts.antialias ?? true,
      forceWebGL: opts.forceWebGL ?? false,
      preserveDrawingBuffer: opts.preserveDrawingBuffer ?? true,
    } as ConstructorParameters<typeof THREE.WebGPURenderer>[0])
    three.setPixelRatio(Math.min(window.devicePixelRatio, 2))
    await three.init()
    return new Renderer(canvas, three)
  }

  // Cameras to keep at the canvas's aspect ratio
  private readonly cameras = new Set<THREE.PerspectiveCamera>()

  track(camera: THREE.PerspectiveCamera): void {
    this.cameras.add(camera)
    this.resize()
  }

  resize(): void {
    const w = Math.max(1, this.canvas.clientWidth)
    const h = Math.max(1, this.canvas.clientHeight)
    this.three.setSize(w, h, false)
    for (const c of this.cameras) {
      c.aspect = w / h
      c.updateProjectionMatrix()
    }
  }

  render(scene: THREE.Scene, camera: THREE.Camera): void {
    this.three.render(scene, camera)
  }

  info(): { drawCalls: number; triangles: number } {
    const r = this.three.info.render as unknown as { drawCalls?: number; calls?: number; triangles: number }
    return { drawCalls: r.drawCalls ?? r.calls ?? 0, triangles: r.triangles }
  }

  // A PNG of the last frame
  screenshot(): string {
    return this.canvas.toDataURL('image/png')
  }

  dispose(): void {
    this.observer.disconnect()
    this.three.dispose()
  }
}
