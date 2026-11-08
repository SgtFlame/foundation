// Links that open a subject in its editor (forge 06g): workbench:<type>/<id>
// with where to go inside it as query parameters, such as
// workbench:prefab/contactCard?node=root/save. The chat writes them in its
// summaries; a click, or the workbench.open client tool, opens them through
// the workbench.subjectOpener contribution for the type.

import type { EditorRegistry } from '../pages/workbench/EditorRegistry.ts'
import { extensions } from './extensions/index.ts'
import { subjectOpeners } from './points.ts'

export interface WorkbenchLink {
  type: string
  id: string
  at: Record<string, string> // e.g. { tab: 'pages', part: '/contacts' }
}

const scheme = 'workbench:'

// The link an href names, or undefined when it is not a workbench link
export function parseWorkbenchLink(href: string): WorkbenchLink | undefined {
  if (!href.startsWith(scheme)) return undefined
  const rest = href.slice(scheme.length)
  const [path, query = ''] = rest.split('?', 2)
  const slash = path.indexOf('/')
  if (slash <= 0 || slash === path.length - 1) return undefined
  const at: Record<string, string> = {}
  for (const [k, v] of new URLSearchParams(query)) at[k] = v
  return { type: decodeURIComponent(path.slice(0, slash)), id: decodeURIComponent(path.slice(slash + 1)), at }
}

// Opens a subject in its editor; false when no contribution opens its type
export async function openSubject(registry: EditorRegistry, link: WorkbenchLink): Promise<boolean> {
  const opener = extensions.get(subjectOpeners).find((o) => o.id === link.type)
  if (!opener) return false
  await opener.open(registry, link.id, link.at)
  return true
}

// Opens a workbench link; false when href is not one, or names a type
// nothing opens
export async function openWorkbenchLink(registry: EditorRegistry, href: string): Promise<boolean> {
  const link = parseWorkbenchLink(href)
  return link ? openSubject(registry, link) : false
}
