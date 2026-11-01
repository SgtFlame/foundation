// The server-described component tree and its patches (mirroring
// backend/features/ui: Node.go, Diff.go, Page.go)

export interface UiOn {
  handler: string
  // Values that are strings starting with "local." or "event." are filled in
  // from local state or the event's data when the event is sent
  payload?: Record<string, unknown>
}

export interface UiNode {
  type: string
  key: string
  props?: Record<string, unknown>
  // "state.*" and "query:*" are resolved by the server into props;
  // "local.*" by the browser
  bind?: Record<string, string>
  on?: Record<string, UiOn>
  children?: UiNode[]
  slots?: Record<string, UiNode[]>
}

export interface UiPatch {
  op: 'replace' | 'insert' | 'remove' | 'move' | 'props'
  path: string[]
  node?: UiNode
  index?: number
  key?: string
  props?: Record<string, unknown>
}

export interface UiAction {
  type: 'toast' | 'navigate' | 'openEditor' | 'focus' | 'download' | 'copy' | 'confirm' | 'rename'
  message?: string
  severity?: 'success' | 'info' | 'warning' | 'error'
  kind?: string
  id?: string
  name?: string
  helpText?: string
  params?: unknown
  page?: string
  path?: string[]
  url?: string
  text?: string
  fileName?: string
  handler?: string
}

export interface UiSubject {
  type: string
  id: string
  label: string
}

// Applies one patch, copying only the nodes on its path, so unchanged
// subtrees keep their identity and React skips them
export function applyPatch(root: UiNode, patch: UiPatch): UiNode {
  const { op, path } = patch
  switch (op) {
    case 'replace':
      if (path.length === 0) return patch.node!
      return updateAt(root, path.slice(0, -1), (parent) => ({
        ...parent,
        children: (parent.children ?? []).map((c) => (c.key === path[path.length - 1] ? patch.node! : c)),
      }))
    case 'remove':
      return updateAt(root, path.slice(0, -1), (parent) => ({
        ...parent,
        children: (parent.children ?? []).filter((c) => c.key !== path[path.length - 1]),
      }))
    case 'insert':
      return updateAt(root, path, (parent) => {
        const children = [...(parent.children ?? [])]
        children.splice(patch.index ?? children.length, 0, patch.node!)
        return { ...parent, children }
      })
    case 'move':
      return updateAt(root, path, (parent) => {
        const children = [...(parent.children ?? [])]
        const from = children.findIndex((c) => c.key === patch.key)
        if (from < 0) return parent
        const [moved] = children.splice(from, 1)
        children.splice(patch.index ?? children.length, 0, moved)
        return { ...parent, children }
      })
    case 'props':
      return updateAt(root, path, (node) => {
        const props = { ...node.props }
        for (const [k, v] of Object.entries(patch.props ?? {})) {
          if (v === null) delete props[k]
          else props[k] = v
        }
        return { ...node, props }
      })
  }
  return root
}

function updateAt(node: UiNode, path: string[], fn: (node: UiNode) => UiNode): UiNode {
  if (path.length === 0) return fn(node)
  const [key, ...rest] = path
  let found = false
  const children = (node.children ?? []).map((c) => {
    if (c.key !== key) return c
    found = true
    return updateAt(c, rest, fn)
  })
  return found ? { ...node, children } : node
}
