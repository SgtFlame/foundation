// Client for the backend user REST handlers
// (backend/features/identity/rest/userhandlers), for administrators.

import { http, type ResponseStatus } from './http/client.ts'
import type { User } from './Identity.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/identity/rest/userdto

export interface UserSummary extends User {
  tenantIds: string[]
}

export interface ListResponse extends ResponseStatus {
  users: UserSummary[]
}

export interface UserResponse extends ResponseStatus {
  user?: UserSummary
}

export interface CreateRequest {
  username: string
  displayName?: string
  email?: string
  password: string
  superAdmin: boolean
  tenantIds: string[] // at least one; each membership starts as Editor
}

// Only what is set changes. Granting or revoking Super Administrator,
// disabling, or a password ends the user's sessions.
export interface UpdateRequest {
  userId: string
  displayName?: string
  email?: string
  superAdmin?: boolean
  disabled?: boolean
  password?: string
}

export const Users = {
  async list(): Promise<ListResponse> {
    return (await http.post<ListResponse>('/identity/users/list', {})).data
  },

  async create(request: CreateRequest): Promise<UserResponse> {
    return (await http.post<UserResponse>('/identity/users/create', request)).data
  },

  async update(request: UpdateRequest): Promise<UserResponse> {
    return (await http.post<UserResponse>('/identity/users/update', request)).data
  },
}
