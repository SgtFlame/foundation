import { SAMPLES } from './ChunkSampler.ts'
import { CHUNK_SIZE, MATERIAL_ORE, MATERIAL_ROCK, type ChunkMesh } from './Types.ts'

// Surface nets over one chunk's samples: one vertex per cell the surface
// crosses, placed at the mean of the crossings on the cell's edges, and one
// quad per crossed grid edge joining the four cells around it.
//
// Samples run from local index -1 to CHUNK_SIZE (SAMPLES per axis). Cells
// c = -1 .. CHUNK_SIZE-1 lie between samples c and c+1. The chunk owns the
// grid edges whose base point is in [0, CHUNK_SIZE)³, so every edge belongs
// to exactly one chunk and the seams close.

const N = CHUNK_SIZE
const S = SAMPLES
const C = N + 1 // cells per axis, local cell m = c + 1

// The 8 corners of a cell and its 12 edges as corner pairs
const CORNERS = [
  [0, 0, 0],
  [1, 0, 0],
  [0, 1, 0],
  [1, 1, 0],
  [0, 0, 1],
  [1, 0, 1],
  [0, 1, 1],
  [1, 1, 1],
]
const EDGES = [
  [0, 1], [2, 3], [4, 5], [6, 7],
  [0, 2], [1, 3], [4, 6], [5, 7],
  [0, 4], [1, 5], [2, 6], [3, 7],
]

export function surfaceNets(density: Float32Array, material: Uint8Array, voxelSize: number): ChunkMesh | null {
  const cellVertex = new Int32Array(C * C * C).fill(-1)
  const pos: number[] = []
  const nrm: number[] = []
  const rock: number[] = []
  const d = new Float64Array(8)
  const sIdx = (i: number, j: number, k: number) => i + S * (j + S * k)

  // Vertices
  for (let mk = 0; mk < C; mk++) {
    for (let mj = 0; mj < C; mj++) {
      for (let mi = 0; mi < C; mi++) {
        // Cell (mi, mj, mk) spans samples (mi..mi+1) in local sample index
        let mask = 0
        let solid = 0
        let rocky = 0
        let ore = 0
        for (let c = 0; c < 8; c++) {
          const s = sIdx(mi + CORNERS[c][0], mj + CORNERS[c][1], mk + CORNERS[c][2])
          const v = density[s]
          d[c] = v
          if (v < 0) {
            mask |= 1 << c
            solid++
            if (material[s] === MATERIAL_ROCK) rocky++
            else if (material[s] === MATERIAL_ORE) {
              rocky++
              ore++
            }
          }
        }
        if (mask === 0 || mask === 255) continue
        let vx = 0
        let vy = 0
        let vz = 0
        let count = 0
        for (const [a, b] of EDGES) {
          const da = d[a]
          const db = d[b]
          if (da < 0 === db < 0) continue
          const t = da / (da - db)
          vx += CORNERS[a][0] + t * (CORNERS[b][0] - CORNERS[a][0])
          vy += CORNERS[a][1] + t * (CORNERS[b][1] - CORNERS[a][1])
          vz += CORNERS[a][2] + t * (CORNERS[b][2] - CORNERS[a][2])
          count++
        }
        const fx = vx / count
        const fy = vy / count
        const fz = vz / count
        // Gradient of the trilinear interpolation at the vertex
        const gx =
          (d[1] - d[0]) * (1 - fy) * (1 - fz) + (d[3] - d[2]) * fy * (1 - fz) + (d[5] - d[4]) * (1 - fy) * fz + (d[7] - d[6]) * fy * fz
        const gy =
          (d[2] - d[0]) * (1 - fx) * (1 - fz) + (d[3] - d[1]) * fx * (1 - fz) + (d[6] - d[4]) * (1 - fx) * fz + (d[7] - d[5]) * fx * fz
        const gz =
          (d[4] - d[0]) * (1 - fx) * (1 - fy) + (d[5] - d[1]) * fx * (1 - fy) + (d[6] - d[2]) * (1 - fx) * fy + (d[7] - d[3]) * fx * fy
        const gl = Math.sqrt(gx * gx + gy * gy + gz * gz) || 1
        cellVertex[mi + C * (mj + C * mk)] = pos.length / 3
        // Local sample index s is grid point s - 1, so cell m starts at m - 1
        pos.push((mi - 1 + fx) * voxelSize, (mj - 1 + fy) * voxelSize, (mk - 1 + fz) * voxelSize)
        nrm.push(gx / gl, gy / gl, gz / gl)
        // 0 (dust) to 1 (rock), then on to 2 (ore): one attribute for three
        rock.push((rocky + ore) / solid)
      }
    }
  }
  if (pos.length === 0) return null

  // Quads, one per crossed edge owned by this chunk
  const idx: number[] = []
  const cv = (mi: number, mj: number, mk: number) => cellVertex[mi + C * (mj + C * mk)]
  for (let k = 0; k < N; k++) {
    for (let j = 0; j < N; j++) {
      for (let i = 0; i < N; i++) {
        // Grid point (i, j, k) is local sample (i+1, j+1, k+1) and lies at
        // the corner shared by cells m = (i..i+1, j..j+1, k..k+1)
        const s0 = density[sIdx(i + 1, j + 1, k + 1)]
        const inside = s0 < 0
        for (let axis = 0; axis < 3; axis++) {
          const s1 =
            axis === 0 ? density[sIdx(i + 2, j + 1, k + 1)] : axis === 1 ? density[sIdx(i + 1, j + 2, k + 1)] : density[sIdx(i + 1, j + 1, k + 2)]
          if (inside === s1 < 0) continue
          // The four cells around the edge, in the plane of the other axes
          let a: number, b: number, c: number, e: number
          const mi = i + 1
          const mj = j + 1
          const mk = k + 1
          if (axis === 0) {
            a = cv(mi, mj - 1, mk - 1); b = cv(mi, mj, mk - 1); c = cv(mi, mj, mk); e = cv(mi, mj - 1, mk)
          } else if (axis === 1) {
            a = cv(mi - 1, mj, mk - 1); b = cv(mi - 1, mj, mk); c = cv(mi, mj, mk); e = cv(mi, mj, mk - 1)
          } else {
            a = cv(mi - 1, mj - 1, mk); b = cv(mi, mj - 1, mk); c = cv(mi, mj, mk); e = cv(mi - 1, mj, mk)
          }
          if (a < 0 || b < 0 || c < 0 || e < 0) continue
          // Face outward: from solid toward empty
          if (inside) idx.push(a, b, c, a, c, e)
          else idx.push(a, c, b, a, e, c)
        }
      }
    }
  }
  if (idx.length === 0) return null
  const vertexCount = pos.length / 3
  return {
    positions: new Float32Array(pos),
    normals: new Float32Array(nrm),
    rock: new Float32Array(rock),
    indices: vertexCount < 65536 ? new Uint16Array(idx) : new Uint32Array(idx),
  }
}
