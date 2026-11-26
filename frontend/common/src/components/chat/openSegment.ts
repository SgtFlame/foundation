// Opening a cited passage (forge 15b): a Chronicle entry in its editor, a
// Volume file at the cited lines, or (forge 18f) whatever its workbench:
// link opens, such as a Blackboards section on its board. Chat's citations and the chat.sources
// primitive (an agent run's or a draft's sources) share it.

import { ChronicleObject } from '../../editors/chronicle/ChronicleObject.ts'
import { openMarkdownAt } from '../../editors/markdown/openMarkdown.ts'
import { log } from '../../logging/index.ts'
import type { EditorRegistry } from '../../pages/workbench/EditorRegistry.ts'
import { openWorkbenchLink } from '../../workbench/links.ts'

export interface OpenableSegment {
  title: string
  location: { entryId?: string; path?: string; startLine?: number; endLine?: number; link?: string }
}

// Whether a segment names something to open
export function openable(segment: OpenableSegment): boolean {
  return !!(segment.location.entryId || segment.location.path || segment.location.link)
}

// Opens what a segment cites; false when it names nothing
export function openSegment(registry: EditorRegistry, segment: OpenableSegment): boolean {
  const { entryId, path, startLine, endLine, link } = segment.location
  if (link) {
    openWorkbenchLink(registry, link).catch((err: unknown) => log.In('chat').fromError(err).With('link', link).warn('cannot open the cited place'))
    return true
  }
  if (entryId) {
    registry.edit(new ChronicleObject({ id: entryId, title: segment.title }))
    return true
  }
  if (!path) return false
  openMarkdownAt(registry, path, startLine ? [startLine, endLine ?? startLine] : undefined).catch((err: unknown) =>
    log.In('chat').fromError(err).With('path', path).warn('cannot open the cited file'),
  )
  return true
}
