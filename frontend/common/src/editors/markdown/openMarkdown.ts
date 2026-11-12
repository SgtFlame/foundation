import type { EditorRegistry } from '../../pages/workbench/EditorRegistry.ts'
import { Collab } from '../../services/Collab.ts'
import { fileTypes } from '../../services/Volume.ts'
import { MarkdownObject } from './MarkdownObject.ts'
import { revealLines } from './reveal.ts'

// Opens the file at path (folders from the knowledge base root, then the
// file's name) in a MarkdownEditor, or focuses the one already open, and
// resolves with the file's collaboration room
export async function openMarkdown(registry: EditorRegistry, path: readonly string[], type: string): Promise<string | undefined> {
  const response = await Collab.openFile({ path: { folders: [...path] }, type })
  const extension = fileTypes.find((t) => t.type === type)?.extension ?? ''
  if (response.fileId) registry.edit(new MarkdownObject(response.fileId, path, type, extension, response.readOnly))
  return response.fileId
}

// Opens a file named by its path from the knowledge base root with its
// extension ("veritas/notes.md"), selecting lines from…to when given
export async function openMarkdownAt(registry: EditorRegistry, path: string, lines?: [number, number]) {
  const parts = path.split('/')
  const name = parts.pop() ?? ''
  const fileType = fileTypes.find((t) => name.endsWith(t.extension))
  if (!fileType) throw new Error(`cannot open ${path}: unknown file type`)
  const fileId = await openMarkdown(registry, [...parts, name.slice(0, -fileType.extension.length)], fileType.type)
  if (fileId && lines) revealLines(fileId, lines[0], lines[1])
}
