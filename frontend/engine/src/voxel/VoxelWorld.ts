import * as THREE from 'three/webgpu'
import { editTouchesChunk } from './ChunkSampler.ts'
import type { MeshResult } from './MeshProtocol.ts'
import { MoonGenerator } from './MoonGenerator.ts'
import { terrainMaterial } from './TerrainMaterial.ts'
import { CHUNK_SIZE, chunkKey, type ChunkMesh, type MoonParams, type VoxelEdit } from './Types.ts'
import { WorkerPool } from './WorkerPool.ts'

interface Chunk {
  key: string
  cx: number
  cy: number
  cz: number
  mesh: THREE.Mesh | null
  // Bumped on every change; a result for an older version is dropped
  version: number
  meshedVersion: number
  // The version of the job in flight, or -1
  pendingVersion: number
  // Distance from the camera at the last update, for ordering
  dist: number
}

export interface VoxelWorldOptions {
  params: MoonParams
  // Chunks whose centre is within this many metres of the camera load
  viewDistance?: number
  workers?: number
}

export interface VoxelWorldStats {
  loaded: number
  meshed: number
  triangles: number
  queued: number
  inFlight: number
  workers: number
  // Mean worker times per chunk over the last 100 jobs (ms)
  sampleMs: number
  meshMs: number
  // Approximate bytes of mesh data held on the GPU
  meshBytes: number
  // Jobs completed since the last params change, and when they all finished
  jobsDone: number
  firstFullLoadMs: number | null
  edits: number
}

// The voxel terrain around a camera: decides which chunks to load, has the
// worker pool mesh them, keeps their three.js meshes, and re-meshes chunks
// when edits arrive. The world holds no density itself: chunks are
// regenerated from the generator plus their edits, in the server's order.
export class VoxelWorld {
  readonly group = new THREE.Group()
  private params: MoonParams
  private gen: MoonGenerator
  private readonly material = terrainMaterial()
  private readonly pool: WorkerPool
  private readonly chunks = new Map<string, Chunk>()
  private readonly editsByChunk = new Map<string, VoxelEdit[]>()
  private editCount = 0
  private viewDistance: number
  private lastCenter = new THREE.Vector3(Infinity, 0, 0)
  private queue: Chunk[] = []
  private recent: { sample: number; mesh: number }[] = []
  private jobsDone = 0
  private loadStart = performance.now()
  private firstFullLoadMs: number | null = null
  private dirtyQueue = true

  constructor(opts: VoxelWorldOptions) {
    this.params = opts.params
    this.gen = new MoonGenerator(opts.params)
    this.viewDistance = opts.viewDistance ?? 160
    this.pool = new WorkerPool((r) => this.onResult(r), opts.workers)
    this.group.name = 'voxel-world'
  }

  get chunkMetres(): number {
    return CHUNK_SIZE * this.params.voxelSize
  }

  get generator(): MoonGenerator {
    return this.gen
  }

  // New generator parameters regenerate every chunk (old meshes stay until
  // their replacements arrive, so the moon never disappears)
  setParams(params: MoonParams): void {
    if (JSON.stringify(params) === JSON.stringify(this.params)) return
    const resized = params.voxelSize !== this.params.voxelSize
    this.params = params
    this.gen = new MoonGenerator(params)
    if (resized) {
      // Chunk coordinates change meaning: drop everything
      for (const c of this.chunks.values()) this.dropMesh(c)
      this.chunks.clear()
      this.rebuildEditIndex([...this.allEdits()])
    } else {
      for (const c of this.chunks.values()) c.version++
    }
    this.lastCenter.set(Infinity, 0, 0)
    this.jobsDone = 0
    this.firstFullLoadMs = null
    this.loadStart = performance.now()
    this.dirtyQueue = true
  }

  setViewDistance(m: number): void {
    this.viewDistance = m
    this.lastCenter.set(Infinity, 0, 0)
  }

  // Replace every edit (a fresh subscription or a reload)
  setEdits(edits: readonly VoxelEdit[]): void {
    const before = new Set(this.editsByChunk.keys())
    this.rebuildEditIndex(edits)
    for (const k of new Set([...before, ...this.editsByChunk.keys()])) this.touch(k)
  }

  // Apply new edits after the ones already held
  addEdits(edits: readonly VoxelEdit[]): void {
    for (const e of edits) {
      for (const k of this.indexEdit(e)) this.touch(k)
    }
  }

  private *allEdits(): Iterable<VoxelEdit> {
    const seen = new Set<number>()
    for (const list of this.editsByChunk.values()) {
      for (const e of list) {
        if (!seen.has(e.seq)) {
          seen.add(e.seq)
          yield e
        }
      }
    }
  }

  private rebuildEditIndex(edits: readonly VoxelEdit[]): void {
    this.editsByChunk.clear()
    this.editCount = 0
    for (const e of [...edits].sort((a, b) => a.seq - b.seq)) this.indexEdit(e)
  }

  private indexEdit(e: VoxelEdit): string[] {
    this.editCount++
    const size = this.chunkMetres
    const pad = e.radius + 1 + this.params.voxelSize
    const keys: string[] = []
    for (let cz = Math.floor((e.z - pad) / size); cz <= Math.floor((e.z + pad) / size); cz++) {
      for (let cy = Math.floor((e.y - pad) / size); cy <= Math.floor((e.y + pad) / size); cy++) {
        for (let cx = Math.floor((e.x - pad) / size); cx <= Math.floor((e.x + pad) / size); cx++) {
          if (!editTouchesChunk(e, cx, cy, cz, this.params.voxelSize)) continue
          const k = chunkKey(cx, cy, cz)
          const list = this.editsByChunk.get(k) ?? []
          // Edits apply in the server's order: one that arrives late goes in place.
          let i = list.length
          while (i > 0 && list[i - 1].seq > e.seq) i--
          if (list[i - 1]?.seq !== e.seq) list.splice(i, 0, e)
          this.editsByChunk.set(k, list)
          keys.push(k)
        }
      }
    }
    return keys
  }

  private touch(key: string): void {
    const c = this.chunks.get(key)
    if (c) {
      c.version++
      this.dirtyQueue = true
    }
  }

  // Could this chunk hold part of the surface? (edits aside)
  private mayHaveSurface(cx: number, cy: number, cz: number): boolean {
    const size = this.chunkMetres
    const x = (cx + 0.5) * size
    const y = (cy + 0.5) * size
    const z = (cz + 0.5) * size
    const rho = Math.sqrt(x * x + y * y + z * z)
    const half = (size * Math.sqrt(3)) / 2 + this.params.voxelSize * 2
    const R = this.params.radius
    if (rho - half > R + this.gen.maxOutward) return false
    if (rho + half < R - this.gen.maxInward) return false
    return true
  }

  // Call each frame with the camera's position
  update(camera: THREE.Vector3): void {
    if (camera.distanceTo(this.lastCenter) > this.chunkMetres / 4) {
      this.lastCenter.copy(camera)
      this.refreshWanted(camera)
      this.dirtyQueue = true
    }
    this.pump()
  }

  // Keep the workers busy: called each frame and whenever a job finishes,
  // so meshing never waits for the next frame
  private pump(): void {
    if (this.dirtyQueue) {
      this.queue = [...this.chunks.values()].filter((c) => c.version !== c.meshedVersion && c.pendingVersion < 0)
      const rank = (c: Chunk) => (c.meshedVersion > 0 ? 0 : 1)
      this.queue.sort((a, b) => rank(a) - rank(b) || a.dist - b.dist)
      this.dirtyQueue = false
    }
    while (this.queue.length > 0 && this.pool.capacity() > 0) {
      const c = this.queue.shift() as Chunk
      if (!this.chunks.has(c.key) || c.pendingVersion >= 0 || c.version === c.meshedVersion) continue
      c.pendingVersion = c.version
      this.pool.submit({
        key: c.key,
        version: c.version,
        cx: c.cx,
        cy: c.cy,
        cz: c.cz,
        params: this.params,
        edits: this.editsByChunk.get(c.key) ?? [],
      })
    }
  }

  private refreshWanted(camera: THREE.Vector3): void {
    const size = this.chunkMetres
    const r = this.viewDistance
    const wanted = new Set<string>()
    const x0 = Math.floor((camera.x - r) / size)
    const x1 = Math.floor((camera.x + r) / size)
    const y0 = Math.floor((camera.y - r) / size)
    const y1 = Math.floor((camera.y + r) / size)
    const z0 = Math.floor((camera.z - r) / size)
    const z1 = Math.floor((camera.z + r) / size)
    for (let cz = z0; cz <= z1; cz++) {
      for (let cy = y0; cy <= y1; cy++) {
        for (let cx = x0; cx <= x1; cx++) {
          const dx = (cx + 0.5) * size - camera.x
          const dy = (cy + 0.5) * size - camera.y
          const dz = (cz + 0.5) * size - camera.z
          const dist = Math.sqrt(dx * dx + dy * dy + dz * dz)
          if (dist > r) continue
          const key = chunkKey(cx, cy, cz)
          if (!this.mayHaveSurface(cx, cy, cz) && !this.editsByChunk.has(key)) continue
          wanted.add(key)
          let c = this.chunks.get(key)
          if (!c) {
            c = { key, cx, cy, cz, mesh: null, version: 1, meshedVersion: 0, pendingVersion: -1, dist }
            this.chunks.set(key, c)
          }
          c.dist = dist
        }
      }
    }
    // Unload chunks well outside the view
    for (const c of this.chunks.values()) {
      if (wanted.has(c.key)) continue
      const dx = (c.cx + 0.5) * size - camera.x
      const dy = (c.cy + 0.5) * size - camera.y
      const dz = (c.cz + 0.5) * size - camera.z
      if (Math.sqrt(dx * dx + dy * dy + dz * dz) > r + size * 2) {
        this.dropMesh(c)
        this.chunks.delete(c.key)
      }
    }
  }

  private onResult(r: MeshResult): void {
    this.jobsDone++
    this.recent.push({ sample: r.sampleMs, mesh: r.meshMs })
    if (this.recent.length > 100) this.recent.shift()
    const c = this.chunks.get(r.key)
    if (!c) return
    if (c.pendingVersion === r.version) c.pendingVersion = -1
    if (r.version < c.meshedVersion) return
    c.meshedVersion = r.version
    this.setMesh(c, r.mesh)
    if (c.version !== c.meshedVersion) this.dirtyQueue = true
    this.pump()
    if (this.firstFullLoadMs === null && this.pool.inFlight === 0 && this.queue.length === 0 && this.allMeshed()) {
      this.firstFullLoadMs = performance.now() - this.loadStart
    }
  }

  private allMeshed(): boolean {
    for (const c of this.chunks.values()) if (c.version !== c.meshedVersion) return false
    return true
  }

  private setMesh(c: Chunk, m: ChunkMesh | null): void {
    this.dropMesh(c)
    if (!m) return
    const g = new THREE.BufferGeometry()
    g.setAttribute('position', new THREE.BufferAttribute(m.positions, 3))
    g.setAttribute('normal', new THREE.BufferAttribute(m.normals, 3))
    g.setAttribute('rock', new THREE.BufferAttribute(m.rock, 1))
    g.setIndex(new THREE.BufferAttribute(m.indices, 1))
    g.computeBoundingSphere()
    g.computeBoundingBox()
    const mesh = new THREE.Mesh(g, this.material)
    const size = this.chunkMetres
    mesh.position.set(c.cx * size, c.cy * size, c.cz * size)
    mesh.name = `chunk ${c.key}`
    mesh.matrixAutoUpdate = false
    mesh.updateMatrix()
    c.mesh = mesh
    this.group.add(mesh)
  }

  private dropMesh(c: Chunk): void {
    if (!c.mesh) return
    this.group.remove(c.mesh)
    c.mesh.geometry.dispose()
    c.mesh = null
  }

  // The first point of terrain along a ray, within maxDistance
  raycast(origin: THREE.Vector3, direction: THREE.Vector3, maxDistance = 300): THREE.Intersection | null {
    const rc = new THREE.Raycaster(origin, direction.clone().normalize(), 0, maxDistance)
    const hits = rc.intersectObjects(this.group.children, false)
    return hits[0] ?? null
  }

  stats(): VoxelWorldStats {
    let meshed = 0
    let triangles = 0
    let bytes = 0
    for (const c of this.chunks.values()) {
      if (!c.mesh) continue
      meshed++
      const g = c.mesh.geometry
      const idx = g.getIndex()
      if (idx) {
        triangles += idx.count / 3
        bytes += idx.array.byteLength
      }
      for (const a of Object.values(g.attributes)) bytes += (a.array as ArrayLike<number> & { byteLength: number }).byteLength
    }
    const n = this.recent.length || 1
    return {
      loaded: this.chunks.size,
      meshed,
      triangles,
      queued: this.queue.length,
      inFlight: this.pool.inFlight,
      workers: this.pool.size,
      sampleMs: this.recent.reduce((a, r) => a + r.sample, 0) / n,
      meshMs: this.recent.reduce((a, r) => a + r.mesh, 0) / n,
      meshBytes: bytes,
      jobsDone: this.jobsDone,
      firstFullLoadMs: this.firstFullLoadMs,
      edits: this.editCount,
    }
  }

  dispose(): void {
    this.pool.dispose()
    for (const c of this.chunks.values()) this.dropMesh(c)
    this.chunks.clear()
    this.material.dispose()
  }
}
