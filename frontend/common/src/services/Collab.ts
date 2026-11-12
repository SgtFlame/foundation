// Client for the backend Collab REST handlers (backend/features/kb/rest/collabhandlers);
// the rooms themselves are channels on the live connection (collab/CollabProvider.ts)

import { http, type ResponseStatus } from './http/client.ts'
import type { VolumePath } from './KnowledgeBase.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/kb/rest/collabdto. uint64 IDs are
// strings, since a JavaScript number cannot hold every uint64.

export interface OpenFileRequest {
  path: VolumePath // folders down to the file's name, without its extension
  type: string
}

export interface OpenFileResponse extends ResponseStatus {
  fileId?: string // the collaboration room, shared by everyone editing the file
  readOnly?: boolean // you may read the Volume but not write it: the room drops your edits
}

export const Collab = {
  async openFile(request: OpenFileRequest): Promise<OpenFileResponse> {
    const response = await http.post<OpenFileResponse>('/kb/collab/files/open', request)
    return response.data
  },
}
