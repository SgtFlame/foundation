import type { EditorRegistry } from '../../pages/workbench/EditorRegistry.ts'
import { Collab } from '../../services/Collab.ts'
import { fileTypes } from '../../services/Volume.ts'
import { MarkdownObject } from './MarkdownObject.ts'

// Opens the file at path (folders from the knowledge base root, then the
// file's name) in a MarkdownEditor, or focuses the one already open
export async function openMarkdown(registry: EditorRegistry, path: readonly string[], type: string) {
  const response = await Collab.openFile({ path: { folders: [...path] }, type })
  const extension = fileTypes.find((t) => t.type === type)?.extension ?? ''
  if (response.fileId) registry.edit(new MarkdownObject(response.fileId, path, type, extension))
}
