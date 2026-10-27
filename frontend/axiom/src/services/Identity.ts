// Client for the backend identity REST handlers
// (backend/features/identity/rest/identityhandlers): sign-in, sessions and
// the user's own account (documentation/plans/MultiTenant.md). The session's
// tokens are HTTP-only cookies the browser never reads; responses carry only
// the access token's expiry. These requests never refresh the session
// themselves (noRefresh).

import { http, type ResponseStatus } from './http/client.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/identity/rest/identitydto. uint64 IDs
// are strings, since a JavaScript number cannot hold every uint64.

export interface User {
  id: string
  username: string
  displayName: string
  email?: string
  superAdmin: boolean // holds Super Administrator on the application
  disabled?: boolean
}

export interface Tenant {
  id: string
  name: string
}

export interface Account {
  user: User
  tenantId: string // the active tenant
  tenants: Tenant[]
}

export interface StatusResponse extends ResponseStatus {
  singleUser: boolean // no accounts yet: no sign-in, and setup is available
  signedIn: boolean
  account?: Account
}

export interface SessionResponse extends ResponseStatus {
  expiresAt?: string // RFC 3339: when the access token expires
  account?: Account // when it changed
}

export interface SetupRequest {
  tenantName: string
  username: string
  displayName?: string
  email?: string
  password: string
}

export interface LoginRequest {
  username: string
  password: string
  tenantId?: string
}

export interface UpdateAccountRequest {
  displayName?: string
  email?: string
  currentPassword?: string
  newPassword?: string
}

const quiet = { noRefresh: true }

export const Identity = {
  async status(): Promise<StatusResponse> {
    return (await http.get<StatusResponse>('/identity/status', quiet)).data
  },

  async setup(request: SetupRequest): Promise<SessionResponse> {
    return (await http.post<SessionResponse>('/identity/setup', request, quiet)).data
  },

  async login(request: LoginRequest): Promise<SessionResponse> {
    return (await http.post<SessionResponse>('/identity/login', request, quiet)).data
  },

  // Rotates the refresh cookie; a 401 session_ended means signing in again
  async refresh(): Promise<SessionResponse> {
    return (await http.post<SessionResponse>('/identity/refresh', undefined, quiet)).data
  },

  async logout(): Promise<ResponseStatus> {
    return (await http.post<ResponseStatus>('/identity/logout', undefined, quiet)).data
  },

  async switchTenant(tenantId: string): Promise<SessionResponse> {
    return (await http.post<SessionResponse>('/identity/switch-tenant', { tenantId })).data
  },

  // A new password ends the user's other sessions and starts a new one here
  async updateAccount(request: UpdateAccountRequest): Promise<SessionResponse> {
    return (await http.post<SessionResponse>('/identity/account', request)).data
  },
}
