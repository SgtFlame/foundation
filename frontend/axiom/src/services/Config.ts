// Client for the backend Config REST handlers (backend/features/config/rest)

import { log } from '../logging/index.ts'
import { API_BASE, http, type ResponseStatus, ServiceError } from './http/client.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/config/rest/configdto

export type Scope = 'app' | 'tenant' | 'user'

// Where a resolved leaf's value came from
// server: the server's own secret, from Vault's bootstrap bundle
export type ValueSource = 'default' | 'server' | 'env' | Scope

export interface SectionInfo {
  id: string
  order: number
  source: string
  title: string
  description?: string
  category: string[]
  scopes: Scope[]
  schema: unknown // the JSON-Schema subset; see settings/schema.ts
  defaults: unknown
  version: number
}

export interface SectionsResponse extends ResponseStatus {
  sections?: SectionInfo[]
}

// A secret as the server shows it: never its value, and no hint when Vault
// holds it
export interface SecretView {
  set: boolean
  hint?: string
}

// A section resolved as seen from a scope
export interface View {
  section?: string
  scope?: Scope
  value?: Record<string, unknown>
  sources?: Record<string, ValueSource> // by leaf path
  revision: number
}

export interface GetRequest {
  section: string
  scope?: Scope
}

export type GetResponse = ResponseStatus & View

export interface SetRequest {
  section: string
  scope?: Scope
  patch: Record<string, unknown> // a JSON merge patch; null inherits a key again
  revision: number // the View's; a stale one fails with 409
}

export interface FieldError {
  path: string
  message: string
}

export interface SetResponse extends ResponseStatus, View {
  errors?: FieldError[]
}

export interface ResetRequest {
  section: string
  scope?: Scope
  keys?: string[] // leaf paths; every key when omitted
}

export type ResetResponse = ResponseStatus & View

export interface ChangedEvent {
  section: string
  scope: string // app, tenant:<id> or user:<id>
}

const configLog = log.In('config')

export const Config = {
  async sections(): Promise<SectionsResponse> {
    const response = await http.get<SectionsResponse>('/config/sections')
    return response.data
  },

  async get(request: GetRequest): Promise<GetResponse> {
    const response = await http.post<GetResponse>('/config/get', request)
    return response.data
  },

  // An invalid patch (400) resolves with errors at their paths, so a form
  // can show them inline; any other failure throws ServiceError, a 409
  // meaning the section was changed elsewhere.
  // A patch carrying a secret is not captured in DevTools' Network tab
  async set(request: SetRequest, { secret = false }: { secret?: boolean } = {}): Promise<SetResponse> {
    const response = await http.post<SetResponse>('/config/set', request, {
      validateStatus: (status) => (status >= 200 && status < 300) || status === 400,
      capture: !secret,
    })
    if (response.status === 400 && !response.data.errors?.length) {
      throw new ServiceError(400, response.data.message ?? 'Bad request', response.data.errorCode, response.config.traceId)
    }
    return response.data
  },

  async reset(request: ResetRequest): Promise<ResetResponse> {
    const response = await http.post<ResetResponse>('/config/reset', request)
    return response.data
  },

  // Calls onChange for every stored change until the returned function is
  // called. EventSource reconnects by itself; it is logged, not captured.
  events(onChange: (event: ChangedEvent) => void): () => void {
    const source = new EventSource(`${API_BASE}/config/events`)
    source.addEventListener('changed', (e) => onChange(JSON.parse((e as MessageEvent<string>).data) as ChangedEvent))
    source.addEventListener('error', () => configLog.debug('the settings event stream dropped; reconnecting'))
    return () => source.close()
  },
}
