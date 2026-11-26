// Where a character may walk in an area (forge 375 S2, thin): a grid of
// 1 m cells over the area, blocked where a piece's model says (buildings,
// trees, a well, fences), with A* paths that are then straightened by line
// of sight. A stand-in for the Recast navigation mesh of 350j and 375e,
// which comes when areas get height levels; this grid is deterministic, so
// the server can run the same one when it takes movement over (S4).

export interface Point {
  x: number
  z: number
}

export class WalkGrid {
  readonly cols: number
  readonly rows: number
  readonly res: number
  readonly blocked: Uint8Array

  // width and depth in metres; res metres per cell
  constructor(width: number, depth: number, res = 1) {
    this.res = res
    this.cols = Math.ceil(width / res)
    this.rows = Math.ceil(depth / res)
    this.blocked = new Uint8Array(this.cols * this.rows)
  }

  // Blocks an axis-aligned rectangle in metres, grown by pad (the walker's radius)
  block(x: number, z: number, w: number, d: number, pad = 0.4): void {
    const c0 = Math.max(0, Math.floor((x - pad) / this.res))
    const r0 = Math.max(0, Math.floor((z - pad) / this.res))
    const c1 = Math.min(this.cols - 1, Math.ceil((x + w + pad) / this.res) - 1)
    const r1 = Math.min(this.rows - 1, Math.ceil((z + d + pad) / this.res) - 1)
    for (let r = r0; r <= r1; r++) for (let c = c0; c <= c1; c++) this.blocked[r * this.cols + c] = 1
  }

  walkable(c: number, r: number): boolean {
    return c >= 0 && r >= 0 && c < this.cols && r < this.rows && this.blocked[r * this.cols + c] === 0
  }

  cellOf(p: Point): [number, number] {
    return [Math.floor(p.x / this.res), Math.floor(p.z / this.res)]
  }

  centre(c: number, r: number): Point {
    return { x: (c + 0.5) * this.res, z: (r + 0.5) * this.res }
  }

  // The walkable cell nearest to p, searching outwards up to maxRing cells
  nearestWalkable(p: Point, maxRing = 12): [number, number] | null {
    const [c, r] = this.cellOf(p)
    if (this.walkable(c, r)) return [c, r]
    for (let k = 1; k <= maxRing; k++) {
      let best: [number, number] | null = null
      let bestD = Infinity
      for (let dr = -k; dr <= k; dr++) {
        for (let dc = -k; dc <= k; dc++) {
          if (Math.max(Math.abs(dc), Math.abs(dr)) !== k || !this.walkable(c + dc, r + dr)) continue
          const q = this.centre(c + dc, r + dr)
          const d = (q.x - p.x) ** 2 + (q.z - p.z) ** 2
          if (d < bestD) {
            bestD = d
            best = [c + dc, r + dr]
          }
        }
      }
      if (best) return best
    }
    return null
  }

  // A path of points from a to b (b moved to the nearest walkable cell), or
  // null when there is none. 8 directions, no cutting corners, then
  // straightened where a straight line stays on walkable cells.
  findPath(a: Point, b: Point, maxNodes = 200_000): Point[] | null {
    const s = this.nearestWalkable(a, 3)
    const g = this.nearestWalkable(b)
    if (!s || !g) return null
    const cols = this.cols
    const start = s[1] * cols + s[0]
    const goal = g[1] * cols + g[0]
    const n = cols * this.rows
    const cost = new Float32Array(n).fill(Infinity)
    const from = new Int32Array(n).fill(-1)
    const closed = new Uint8Array(n)
    const heap = new Heap()
    const h = (i: number) => {
      const dx = Math.abs((i % cols) - g[0])
      const dz = Math.abs(Math.floor(i / cols) - g[1])
      return Math.max(dx, dz) + (Math.SQRT2 - 1) * Math.min(dx, dz)
    }
    cost[start] = 0
    heap.push(start, h(start))
    let visited = 0
    while (heap.size > 0) {
      const i = heap.pop()
      if (i === goal) break
      if (closed[i]) continue
      closed[i] = 1
      if (++visited > maxNodes) return null
      const c = i % cols
      const r = (i - c) / cols
      for (let dr = -1; dr <= 1; dr++) {
        for (let dc = -1; dc <= 1; dc++) {
          if (!dc && !dr) continue
          const nc = c + dc
          const nr = r + dr
          if (!this.walkable(nc, nr)) continue
          if (dc && dr && (!this.walkable(c + dc, r) || !this.walkable(c, r + dr))) continue
          const j = nr * cols + nc
          const step = dc && dr ? Math.SQRT2 : 1
          const nextCost = cost[i] + step
          if (nextCost < cost[j]) {
            cost[j] = nextCost
            from[j] = i
            heap.push(j, nextCost + h(j))
          }
        }
      }
    }
    if (from[goal] === -1 && goal !== start) return null
    const cells: number[] = []
    for (let i = goal; i !== -1; i = from[i]) cells.push(i)
    cells.reverse()
    const pts = cells.map((i) => this.centre(i % cols, Math.floor(i / cols)))
    pts[0] = { ...a }
    if (this.walkable(...this.cellOf(b))) pts[pts.length - 1] = { ...b }
    return this.straighten(pts)
  }

  // Drops the points a straight line can skip
  straighten(pts: Point[]): Point[] {
    if (pts.length <= 2) return pts
    const out = [pts[0]]
    let i = 0
    while (i < pts.length - 1) {
      let j = pts.length - 1
      while (j > i + 1 && !this.clear(pts[i], pts[j])) j--
      out.push(pts[j])
      i = j
    }
    return out
  }

  // Whether the segment a-b crosses only walkable cells (sampled finely)
  clear(a: Point, b: Point): boolean {
    const len = Math.hypot(b.x - a.x, b.z - a.z)
    const steps = Math.ceil(len / (this.res * 0.25))
    for (let k = 0; k <= steps; k++) {
      const t = k / Math.max(1, steps)
      const [c, r] = this.cellOf({ x: a.x + (b.x - a.x) * t, z: a.z + (b.z - a.z) * t })
      if (!this.walkable(c, r)) return false
    }
    return true
  }
}

// A binary min-heap of cell indices by priority
class Heap {
  private items: number[] = []
  private prio: number[] = []
  get size(): number {
    return this.items.length
  }
  push(item: number, p: number): void {
    this.items.push(item)
    this.prio.push(p)
    let i = this.items.length - 1
    while (i > 0) {
      const parent = (i - 1) >> 1
      if (this.prio[parent] <= this.prio[i]) break
      this.swap(i, parent)
      i = parent
    }
  }
  pop(): number {
    const top = this.items[0]
    const lastItem = this.items.pop()!
    const lastPrio = this.prio.pop()!
    if (this.items.length > 0) {
      this.items[0] = lastItem
      this.prio[0] = lastPrio
      let i = 0
      for (;;) {
        const l = 2 * i + 1
        const r = l + 1
        let m = i
        if (l < this.items.length && this.prio[l] < this.prio[m]) m = l
        if (r < this.items.length && this.prio[r] < this.prio[m]) m = r
        if (m === i) break
        this.swap(i, m)
        i = m
      }
    }
    return top
  }
  private swap(a: number, b: number): void {
    ;[this.items[a], this.items[b]] = [this.items[b], this.items[a]]
    ;[this.prio[a], this.prio[b]] = [this.prio[b], this.prio[a]]
  }
}
