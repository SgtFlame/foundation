import type { MeshJob, MeshResult } from './MeshProtocol.ts'

// A pool of meshing workers. Jobs go to the least busy worker; the caller
// keeps its own queue and asks for more only while capacity() > 0.
export class WorkerPool {
  private readonly workers: Worker[] = []
  private readonly busy: number[] = []
  private nextId = 1
  private readonly perWorker: number

  private readonly onResult: (r: MeshResult) => void

  constructor(
    onResult: (r: MeshResult) => void,
    size = Math.max(2, Math.min(8, (navigator.hardwareConcurrency || 4) - 1)),
    perWorker = 2,
  ) {
    this.perWorker = perWorker
    this.onResult = onResult
    for (let i = 0; i < size; i++) {
      const w = new Worker(new URL('./MeshWorker.ts', import.meta.url), { type: 'module' })
      const n = i
      w.onmessage = (ev: MessageEvent<MeshResult>) => {
        this.busy[n]--
        this.onResult(ev.data)
      }
      this.workers.push(w)
      this.busy.push(0)
    }
  }

  get size(): number {
    return this.workers.length
  }

  // Jobs in flight
  get inFlight(): number {
    return this.busy.reduce((a, b) => a + b, 0)
  }

  capacity(): number {
    return this.workers.length * this.perWorker - this.inFlight
  }

  submit(job: Omit<MeshJob, 'id'>): number {
    let best = 0
    for (let i = 1; i < this.busy.length; i++) if (this.busy[i] < this.busy[best]) best = i
    const id = this.nextId++
    this.busy[best]++
    this.workers[best].postMessage({ ...job, id } satisfies MeshJob)
    return id
  }

  dispose(): void {
    for (const w of this.workers) w.terminate()
    this.workers.length = 0
  }
}
