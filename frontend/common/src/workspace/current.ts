// Which workspace a browser tab shows, and /workspace's matcher (forge
// 16a). Kept free of React and the services so `node --test` runs its
// tests.

// A workspace as the workspace.list query sends it
export interface Workspace {
  id: string
  name: string
  isDefault?: boolean
  position: number
  layout?: unknown
  context?: unknown
  version: number
  createdAt?: string
  updatedAt?: string
}

export const defaultWorkspaceId = 'default'

// The longest name a workspace may have, as the server checks it
export const maxWorkspaceName = 60

// The workspace a tab shows, given the list and what is stored: the tab's
// own (sessionStorage) while it exists; a tab with none opens the last one
// used in this browser (localStorage); a workspace that no longer exists
// (deleted elsewhere) falls back to Default, quietly. Undefined while the
// list is empty (before Default is made).
export function chooseCurrent(list: readonly Workspace[], tabId?: string | null, lastId?: string | null): string | undefined {
  const has = (id?: string | null) => !!id && list.some((w) => w.id === id)
  if (tabId) {
    if (has(tabId)) return tabId
  } else if (has(lastId)) {
    return lastId!
  }
  return (list.find((w) => w.isDefault) ?? list[0])?.id
}

// A name as names are compared: letters and digits, lower case, as the
// server's workspace.Normalized
export function normalizeWorkspaceName(name: string): string {
  return name.toLowerCase().replace(/[^\p{L}\p{N}]/gu, '')
}

// Why a name cannot be used for the workspace self (none when new), or
// undefined when it can
export function nameProblem(name: string, list: readonly Workspace[], self?: string): string | undefined {
  const n = normalizeWorkspaceName(name)
  if (!n) return 'A name needs letters or digits'
  if ([...name.trim()].length > maxWorkspaceName) return `At most ${maxWorkspaceName} characters`
  const taken = list.find((w) => w.id !== self && normalizeWorkspaceName(w.name) === n)
  return taken ? `You already have a workspace called ${taken.name}` : undefined
}

// The first free name like base: "Support copy", "Support copy 2", ...
export function copyName(base: string, list: readonly Workspace[]): string {
  const stem = `${base} copy`.slice(0, maxWorkspaceName)
  for (let i = 1; ; i++) {
    const name = i === 1 ? stem : `${stem.slice(0, maxWorkspaceName - String(i).length - 1)} ${i}`
    if (!nameProblem(name, list)) return name
  }
}

export type WorkspaceMatch =
  | { kind: 'found'; workspace: Workspace }
  | { kind: 'ambiguous'; workspaces: Workspace[] }
  | { kind: 'unknown' }

// The workspace a person named: by its name (ignoring case, emoji and
// spaces), else by a unique prefix. Exact names win over prefixes.
export function matchWorkspace(query: string, list: readonly Workspace[]): WorkspaceMatch {
  const q = normalizeWorkspaceName(query)
  if (!q) return { kind: 'unknown' }
  for (const hits of [
    list.filter((w) => normalizeWorkspaceName(w.name) === q),
    list.filter((w) => normalizeWorkspaceName(w.name).startsWith(q)),
  ]) {
    if (hits.length === 1) return { kind: 'found', workspace: hits[0] }
    if (hits.length > 1) return { kind: 'ambiguous', workspaces: hits }
  }
  return { kind: 'unknown' }
}

// The workspaces as a markdown list, the current one marked, each with the
// /workspace that switches to it
export function workspaceList(list: readonly Workspace[], current?: string): string {
  return list.map((w) => `- \`/workspace ${w.name.toLowerCase()}\`: ${w.name}${w.id === current ? ' (this tab)' : ''}`).join('\n')
}
