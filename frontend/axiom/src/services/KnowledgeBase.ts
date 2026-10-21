// Client for the backend Knowledge Base REST handlers (backend/features/kb/rest)

import { http } from './http/client.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/kb/rest/kbdto. uint64 IDs are strings,
// since a JavaScript number cannot hold every uint64.

export interface VolumePath {
  tenant?: string
  user?: string
  folders: string[]
}

export interface OpenVolumeRequest {
  tenant?: string
  path: VolumePath
}

export interface OpenVolumeResponse {
  message?: string
  volumeId?: string
}

export const KnowledgeBase = {
  async openVolume(request: OpenVolumeRequest): Promise<OpenVolumeResponse> {
    const response = await http.post<OpenVolumeResponse>('/kb/volumes/open', request)
    return response.data
  },
}
