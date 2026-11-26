import type { Npc, Placement } from './PieceSet.ts'

// The Sheaflands† farmstead (forge 375 S1), laid out from the farmland set:
// a yard with a well, a farmhouse to the east, a barn to the north-west,
// lanes joining them, fields, trees and a fence. 12 × 10 cells. The World
// and Area editors offer it to start from; Fit.test.ts checks it fits.
export const farmstead: { name: string; width: number; depth: number; pieces: Omit<Placement, 'id'>[] } = {
  name: 'Sheaflands farmstead',
  width: 12,
  depth: 10,
  pieces: [
    { piece: 'yard', x: 4, z: 3, rot: 0 },
    { piece: 'road', x: 5, z: 0, rot: 0 },
    { piece: 'road', x: 5, z: 1, rot: 0 },
    { piece: 'road', x: 5, z: 2, rot: 0 },
    { piece: 'road', x: 7, z: 4, rot: 1 },
    { piece: 'road', x: 8, z: 4, rot: 1 },
    { piece: 'road', x: 9, z: 4, rot: 1 },
    { piece: 'roadCorner', x: 10, z: 4, rot: 3 },
    { piece: 'farmhouse', x: 9, z: 2, rot: 0 },
    { piece: 'barn', x: 1, z: 1, rot: 0 },
    { piece: 'road', x: 1, z: 4, rot: 0 },
    { piece: 'road', x: 1, z: 5, rot: 0 },
    { piece: 'roadCorner', x: 1, z: 6, rot: 0 },
    { piece: 'road', x: 2, z: 6, rot: 1 },
    { piece: 'road', x: 3, z: 6, rot: 1 },
    { piece: 'road', x: 4, z: 6, rot: 1 },
    { piece: 'roadT', x: 5, z: 6, rot: 0 },
    { piece: 'road', x: 6, z: 6, rot: 1 },
    { piece: 'roadEnd', x: 7, z: 6, rot: 3 },
    { piece: 'field', x: 8, z: 7, rot: 0 },
    { piece: 'field', x: 9, z: 7, rot: 0 },
    { piece: 'field', x: 10, z: 7, rot: 0 },
    { piece: 'copse', x: 7, z: 0, rot: 0 },
    { piece: 'tree', x: 0, z: 0, rot: 0 },
    { piece: 'tree', x: 3, z: 0, rot: 0 },
    { piece: 'tree', x: 0, z: 8, rot: 0 },
    { piece: 'tree', x: 11, z: 1, rot: 0 },
    { piece: 'fence', x: 2, z: 8, rot: 0 },
    { piece: 'fence', x: 3, z: 8, rot: 0 },
    { piece: 'fence', x: 4, z: 8, rot: 0 },
  ],
}

// The farmhouse kitchen (forge 375 S3), laid out from the kitchen set: the
// hearth with the last sheaf over it on the north wall between shelves and
// a window, the table and its benches, a box bed in the east corner,
// barrels, the door out on the south wall (turned so its wall is south)
// and the farmer by the hearth. 6 × 5 cells of 2 m.
export const kitchen: {
  name: string
  width: number
  depth: number
  pieces: (Omit<Placement, 'id'> & { id?: string })[]
  npcs: Omit<Npc, 'id'>[]
} = {
  name: 'Farmhouse kitchen',
  width: 6,
  depth: 5,
  pieces: [
    { piece: 'shelf', x: 0, z: 0, rot: 0 },
    { piece: 'hearth', x: 2, z: 0, rot: 0 },
    { piece: 'window', x: 4, z: 0, rot: 0 },
    { piece: 'window', x: 0, z: 2, rot: 3 },
    { piece: 'table', x: 2, z: 2, rot: 0 },
    { piece: 'bench', x: 2, z: 3, rot: 0 },
    { piece: 'bench', x: 3, z: 3, rot: 0 },
    { piece: 'bed', x: 5, z: 2, rot: 0 },
    { piece: 'barrel', x: 0, z: 4, rot: 0 },
    { id: 'door', piece: 'door', x: 2, z: 4, rot: 2 },
    { piece: 'window', x: 4, z: 4, rot: 2 },
  ],
  npcs: [{ name: 'The farmer', kind: 'farmer', x: 8.8, z: 3, facing: 3.9, dialogue: 'farmer' }],
}
