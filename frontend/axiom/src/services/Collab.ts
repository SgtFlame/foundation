// Client for the backend Collab REST handlers (backend/features/kb/rest/collabhandlers)

import { API_BASE, http } from './http/client.ts'
import type { VolumePath } from './KnowledgeBase.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/kb/rest/collabdto. uint64 IDs are
// strings, since a JavaScript number cannot hold every uint64.

export interface OpenFileRequest {
  tenant?: string
  path: VolumePath // folders down to the file's name, without its extension
  type: string
}

export interface OpenFileResponse {
  message?: string
  fileId?: string // the collaboration room, shared by everyone editing the file
}

export const Collab = {
  async openFile(request: OpenFileRequest): Promise<OpenFileResponse> {
    const response = await http.post<OpenFileResponse>('/kb/collab/files/open', request)
    return response.data
  },

  // The WebSocket URL of a file's room, speaking the y-websocket protocol
  socketUrl(fileId: string): string {
    const url = new URL(`${API_BASE}/kb/collab/files/${fileId}`, window.location.href)
    url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
    return url.toString()
  },
}
