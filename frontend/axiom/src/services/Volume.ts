// Client for the backend Volume REST handlers (backend/features/kb/rest/volumehandlers)

import { http } from './http/client.ts'
import type { VolumePath } from './KnowledgeBase.ts'

export { ServiceError } from './http/client.ts'
export type { VolumePath } from './KnowledgeBase.ts'

// Wire DTOs mirroring backend/features/kb/rest/volumedto. uint64 IDs are
// strings, since a JavaScript number cannot hold every uint64.

export interface FolderEntry {
  name: string
  isVolume: boolean
}

export interface FileEntry {
  name: string // without the extension
  type: string // content type, one of fileTypes when created here
}

// The file types createFile accepts, mirroring volumeservice.FileTypes
export const fileTypes = [{ type: 'markdown', label: 'Markdown', extension: '.md' }] as const

export interface ListFolderRequest {
  tenant?: string
  path: VolumePath
}

export interface ListFolderResponse {
  message?: string
  isVolume: boolean
  folders: FolderEntry[]
  files: FileEntry[]
}

export interface MakeFolderRequest {
  tenant?: string
  path: VolumePath
}

export interface MakeFolderResponse {
  message?: string
}

export interface RemoveFolderRequest {
  tenant?: string
  path: VolumePath
}

export interface RemoveFolderResponse {
  message?: string
}

export interface CreateVolumeRequest {
  tenant?: string
  path: VolumePath
}

export interface CreateVolumeResponse {
  message?: string
  volumeId?: string
}

export interface CreateFileRequest {
  tenant?: string
  path: VolumePath // folders down to the file's name, without its extension
  type: string
}

export interface CreateFileResponse {
  message?: string
}

export const Volume = {
  async listFolder(request: ListFolderRequest): Promise<ListFolderResponse> {
    const response = await http.post<ListFolderResponse>('/kb/volumes/folders/list', request)
    return response.data
  },

  async makeFolder(request: MakeFolderRequest): Promise<MakeFolderResponse> {
    const response = await http.post<MakeFolderResponse>('/kb/volumes/folders/make', request)
    return response.data
  },

  async removeFolder(request: RemoveFolderRequest): Promise<RemoveFolderResponse> {
    const response = await http.post<RemoveFolderResponse>('/kb/volumes/folders/remove', request)
    return response.data
  },

  async createVolume(request: CreateVolumeRequest): Promise<CreateVolumeResponse> {
    const response = await http.post<CreateVolumeResponse>('/kb/volumes/create', request)
    return response.data
  },

  async createFile(request: CreateFileRequest): Promise<CreateFileResponse> {
    const response = await http.post<CreateFileResponse>('/kb/volumes/files/create', request)
    return response.data
  },
}
