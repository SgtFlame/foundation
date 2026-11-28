import type { EditorRegistry } from '../../pages/workbench/EditorRegistry.ts'
import { Collab } from '../../services/Collab.ts'
import { fileTypes } from '../../services/Volume.ts'
import { RichTextObject } from './RichTextObject.ts'

// Opens the document file at path (folders from the knowledge base root,
// then the file's name) in the rich-text editor, or focuses the one
// already open, and resolves with the file's collaboration room
export async function openDocument(registry: EditorRegistry, path: readonly string[]): Promise<string | undefined> {
  const response = await Collab.openFile({ path: { folders: [...path] }, type: 'document' })
  const extension = fileTypes.find((t) => t.type === 'document')?.extension ?? ''
  if (response.fileId) registry.edit(new RichTextObject(response.fileId, path, extension, response.readOnly))
  return response.fileId
}
