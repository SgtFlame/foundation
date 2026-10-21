import type { EditorRegistry } from '../../pages/workbench/EditorRegistry.ts'
import { KnowledgeBase } from '../../services/KnowledgeBase.ts'
import { VolumeObject } from './VolumeObject.ts'

// Opens the volume at path (folders from the knowledge base root) in its own
// VolumeEditor, or focuses the one already open
export async function openVolume(registry: EditorRegistry, path: readonly string[]) {
  const response = await KnowledgeBase.openVolume({ path: { folders: [...path] } })
  if (response.volumeId) registry.edit(new VolumeObject(response.volumeId, path))
}
