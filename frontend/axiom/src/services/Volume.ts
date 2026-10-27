// Client for the backend Volume REST handlers (backend/features/kb/rest/volumehandlers)

import { http, type ResponseStatus } from './http/client.ts'
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
  path: VolumePath
}

export interface ListFolderResponse extends ResponseStatus {
  isVolume: boolean
  folders: FolderEntry[]
  files: FileEntry[]
}

export interface MakeFolderRequest {
  path: VolumePath
}

export interface MakeFolderResponse extends ResponseStatus {
}

export interface RemoveFolderRequest {
  path: VolumePath
}

export interface RemoveFolderResponse extends ResponseStatus {
}

export interface CreateVolumeRequest {
  path: VolumePath
}

export interface CreateVolumeResponse extends ResponseStatus {
  volumeId?: string
}

export interface CreateFileRequest {
  path: VolumePath // folders down to the file's name, without its extension
  type: string
  content?: string // the new file's text; omitted makes it empty
}

export interface CreateFileResponse extends ResponseStatus {
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
