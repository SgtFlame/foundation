import type { ChunkMesh, MoonParams, VoxelEdit } from './Types.ts'

export interface MeshJob {
  id: number
  key: string
  // The chunk's version when the job was sent; older results are dropped
  version: number
  cx: number
  cy: number
  cz: number
  params: MoonParams
  edits: VoxelEdit[]
}

export interface MeshResult {
  id: number
  key: string
  version: number
  mesh: ChunkMesh | null
  sampleMs: number
  meshMs: number
}
