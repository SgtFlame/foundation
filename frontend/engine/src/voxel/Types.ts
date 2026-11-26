// Shared types of the voxel world. A density sample is a signed distance in
// metres: negative inside solid ground, positive in empty space, zero on
// the surface. Each sample also has a material id.

// Material ids. V1 has two placeholder materials; V8 adds ore, found only
// off the moon (the asteroids), which bigger ships need
export const MATERIAL_DUST = 0
export const MATERIAL_ROCK = 1
export const MATERIAL_ORE = 2
// V9: the living soil of a planet with air, where the moon has dust
export const MATERIAL_SOIL = 3
export const MATERIAL_COUNT = 4

// The chunk grid: CHUNK_SIZE cells along each axis
export const CHUNK_SIZE = 32

// The moon generator's parameters, as the world definition holds them
export interface MoonParams {
  seed: number
  // Metres per voxel (0.5 m target near the player)
  voxelSize: number
  // Mean radius of the moon in metres
  radius: number
  // Height of the fractal hills in metres
  amplitude: number
  // Features per metre of the first octave
  frequency: number
  octaves: number
  // How many craters, and the largest crater's radius in metres
  craters: number
  craterMaxRadius: number
  // Depth below the surface (metres) where dust gives way to rock
  dustDepth: number
  // Small asteroids floating near the moon (forge 360 V2), 0 to 8
  asteroids?: number
  // How much of the rock is ore, 0 to 1 (V8): none on the moon itself
  ore?: number
  // Soil in place of dust (V9): a planet with air
  soil?: boolean
}

export const DEFAULT_MOON: MoonParams = {
  seed: 1,
  voxelSize: 0.5,
  radius: 100,
  amplitude: 6,
  frequency: 0.02,
  octaves: 5,
  craters: 24,
  craterMaxRadius: 22,
  dustDepth: 1.5,
  asteroids: 2,
}

export type EditKind = 'dig' | 'fill'

// One terrain edit: a smooth sphere removed (dig) or added (fill). The
// server decides and orders edits; every client applies them in that order.
export interface VoxelEdit {
  // The edit's sequence number on the server (its order)
  seq: number
  kind: EditKind
  x: number
  y: number
  z: number
  radius: number
  // The material a fill adds
  material?: number
}

export interface ChunkCoord {
  cx: number
  cy: number
  cz: number
}

export function chunkKey(cx: number, cy: number, cz: number): string {
  return `${cx},${cy},${cz}`
}

// The mesh of one chunk, in metres relative to the chunk's origin
export interface ChunkMesh {
  positions: Float32Array
  normals: Float32Array
  // Rock fraction of each vertex, 0 (dust) to 1 (rock)
  rock: Float32Array
  indices: Uint16Array | Uint32Array
}
