// A workspace's saved layout (forge 16b): the flexlayout model's main area
// as JSON, with only the tabs that can be reopened from their objects.
// Kept free of React, flexlayout's runtime and the services so `node
// --test` runs its tests.

// A node of flexlayout's JSON: a row, a tabset, a tab group or a tab
export interface LayoutNode {
  type: string
  id?: string
  component?: string
  children?: LayoutNode[]
  selected?: number
  active?: boolean
  weight?: number
  [attr: string]: unknown
}

// What a workspace's layout holds: format 1 is a flexlayout row
export interface SavedLayout {
  format: 1
  layout: LayoutNode
}

// The tabs saved before workspaces, in this browser's storage (the move-in)
export interface SavedTabs {
  tabs: { id: string; name: string; helpText?: string; component: string; config: unknown }[]
  selected?: string
}

const isTab = (n: LayoutNode) => n.type === 'tab'

// A row holding one empty tabset: a workspace with nothing open
export function emptyRow(): LayoutNode {
  return { type: 'row', weight: 100, children: [{ type: 'tabset', weight: 100, active: true, children: [] }] }
}

// The tabs of a layout, in order
export function tabsOf(node: LayoutNode | undefined): LayoutNode[] {
  if (!node) return []
  if (isTab(node)) return [node]
  return (node.children ?? []).flatMap(tabsOf)
}

// A copy of node with only the tabs keep accepts. A tabset left empty is
// removed, and so is a row left empty; a tabset's selected tab stays
// selected while kept, else the one now in its place is.
export function prune(node: LayoutNode, keep: (tab: LayoutNode) => boolean): LayoutNode | undefined {
  if (isTab(node)) return keep(node) ? { ...node } : undefined
  const before = node.children ?? []
  const children = before.map((c) => prune(c, keep)).filter((c): c is LayoutNode => !!c)
  if (node.type === 'row') return children.length ? { ...node, children } : undefined
  // a tabset or a tab group
  if (children.length === 0) return undefined
  const out: LayoutNode = { ...node, children }
  if (typeof node.selected === 'number') {
    const was = before[node.selected]
    const at = was ? children.findIndex((c) => c.id === was.id) : -1
    out.selected = at >= 0 ? at : Math.min(Math.max(node.selected, 0), children.length - 1)
  }
  return out
}

// What a workspace saves of the main area (model.toJson().layout): the tabs
// restorable accepts, which never include border tabs
export function layoutToSave(main: LayoutNode, restorable: (component: string) => boolean): SavedLayout {
  const layout = prune(main, (t) => t.component !== 'borderTab' && restorable(t.component ?? '')) ?? { ...main, children: [] }
  return { format: 1, layout }
}

// A saved layout as read from a workspace, or undefined when it holds none
// (a new workspace) or one this code cannot read
export function readLayout(raw: unknown): SavedLayout | undefined {
  const v = raw as SavedLayout | undefined
  if (!v || v.format !== 1 || typeof v.layout !== 'object' || v.layout?.type !== 'row') return undefined
  return v
}

// The tabs saved before workspaces, as a layout of one tabset
export function layoutFromTabs(saved: SavedTabs): SavedLayout {
  const tabs = saved.tabs.filter((t) => t && typeof t.id === 'string' && typeof t.component === 'string')
  const selected = Math.max(tabs.findIndex((t) => t.id === saved.selected), 0)
  return {
    format: 1,
    layout: { type: 'row', weight: 100, children: [{ type: 'tabset', weight: 100, active: true, selected, children: tabs.map((t) => ({ type: 'tab', ...t })) }] },
  }
}

// The row to show: layout's (or an empty one), with the tabs given added to
// its active tabset (else its first) unless already there. A row with no
// tabset gets one, so there is somewhere to open an editor.
export function rowToShow(layout: LayoutNode | undefined, add: readonly LayoutNode[] = []): LayoutNode {
  const row = structuredClone(layout && layout.type === 'row' ? layout : emptyRow())
  const tabsets: LayoutNode[] = []
  const visit = (n: LayoutNode) => {
    if (n.type === 'tabset') tabsets.push(n)
    else if (n.type === 'row') (n.children ?? []).forEach(visit)
  }
  visit(row)
  if (tabsets.length === 0) {
    const empty = emptyRow().children![0]
    row.children = [...(row.children ?? []), empty]
    tabsets.push(empty)
  }
  const have = new Set(tabsOf(row).map((t) => t.id))
  const extra = add.filter((t) => t.id && !have.has(t.id))
  if (extra.length) {
    const target = tabsets.find((t) => t.active) ?? tabsets[0]
    target.children = [...(target.children ?? []), ...extra.map((t) => ({ ...t }))]
  }
  return row
}

// Whether two saved layouts are the same, as JSON
export function sameLayout(a: SavedLayout | undefined, b: SavedLayout | undefined): boolean {
  return JSON.stringify(a ?? null) === JSON.stringify(b ?? null)
}
