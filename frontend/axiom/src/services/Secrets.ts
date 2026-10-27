// Client for the backend secrets REST handlers
// (backend/features/secrets/rest/secretshandlers): the server's secrets in
// Vault and the app scope's secret settings, write-only
// (documentation/plans/Secrets.md). No response ever carries a value.

import { http, type ResponseStatus } from './http/client.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/secrets/rest/secretsdto

export type SecretKind = 'server' | 'setting'

export interface Secret {
  name: string // its path in Vault: 'server/gemini-api-key', 'config/app/ai.providers/gemini.apiKey'
  kind: SecretKind
  title?: string // a setting's section and field titles
  set: boolean
  version?: number
  updatedAt?: string
  updatedBy?: string
  source?: 'env' | 'server' // where a setting's value comes from when not stored at the app scope
  protected?: boolean // the database key: rotated with rotate-db-key.sh
}

export interface ListResponse extends ResponseStatus {
  secrets: Secret[]
}

export interface PutResponse extends ResponseStatus {
  secret?: Secret
}

export type BootstrapState = 'sealed' | 'starting' | 'ready' | 'degraded'

export interface StatusResponse extends ResponseStatus {
  state?: BootstrapState
  vault: boolean
  addr?: string
  mount?: string
  tokenExpires?: string
  problem?: string // why it is degraded
}

export const Secrets = {
  async list(prefix = ''): Promise<ListResponse> {
    return (await http.post<ListResponse>('/secrets/list', { prefix })).data
  },

  // The value is never captured in DevTools' Network tab
  async put(name: string, value: string): Promise<PutResponse> {
    return (await http.post<PutResponse>('/secrets/put', { name, value }, { capture: false })).data
  },

  async delete(name: string): Promise<ResponseStatus> {
    return (await http.post<ResponseStatus>('/secrets/delete', { name })).data
  },

  async status(): Promise<StatusResponse> {
    return (await http.get<StatusResponse>('/secrets/status')).data
  },
}
