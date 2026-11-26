/// <reference lib="webworker" />
// A meshing worker: samples a chunk from the generator plus its edits and
// meshes it with surface nets. Keeps one generator per parameter set.
import { sampleChunk } from './ChunkSampler.ts'
import { MoonGenerator } from './MoonGenerator.ts'
import { surfaceNets } from './SurfaceNets.ts'
import type { MeshJob, MeshResult } from './MeshProtocol.ts'

let gen: MoonGenerator | null = null
let genKey = ''

self.onmessage = (ev: MessageEvent<MeshJob>) => {
  const job = ev.data
  const key = JSON.stringify(job.params)
  if (!gen || key !== genKey) {
    gen = new MoonGenerator(job.params)
    genKey = key
  }
  const t0 = performance.now()
  const samples = sampleChunk(gen, job.cx, job.cy, job.cz, job.edits)
  const t1 = performance.now()
  const mesh = samples.uniform ? null : surfaceNets(samples.density, samples.material, job.params.voxelSize)
  const t2 = performance.now()
  const result: MeshResult = { id: job.id, key: job.key, version: job.version, mesh, sampleMs: t1 - t0, meshMs: t2 - t1 }
  const transfer: Transferable[] = mesh ? [mesh.positions.buffer, mesh.normals.buffer, mesh.rock.buffer, mesh.indices.buffer] : []
  ;(self as unknown as DedicatedWorkerGlobalScope).postMessage(result, transfer)
}
