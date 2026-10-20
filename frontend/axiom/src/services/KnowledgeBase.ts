// Client for the backend Knowledge Base REST handlers (backend/features/kb/rest)

const API_BASE: string = import.meta.env.VITE_API_BASE ?? '/api/v1'

// Wire DTOs mirroring backend/features/kb/rest/kbdto. uint64 IDs are strings,
// since a JavaScript number cannot hold every uint64.

export interface VolumePath {
  tenant?: string
  user?: string
  folders: string[]
}

export interface OpenVaultRequest {
  tenant: string
  path: VolumePath
}

export interface OpenVaultResponse {
  message?: string
  vaultId?: string
}

// Thrown when a handler responds with a non-2xx status
export class ServiceError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ServiceError'
    this.status = status
  }
}

async function post<Req, Resp extends { message?: string }>(path: string, request: Req): Promise<Resp> {
  const response = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  })
  const body = (await response.json().catch(() => ({}))) as Resp
  if (!response.ok) {
    throw new ServiceError(response.status, body.message ?? response.statusText)
  }
  return body
}

export const KnowledgeBase = {
  openVault(request: OpenVaultRequest): Promise<OpenVaultResponse> {
    return post('/kb/vaults/open', request)
  },
}
