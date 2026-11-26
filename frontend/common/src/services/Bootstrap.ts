// Client for the backend bootstrap REST handlers
// (backend/features/secrets/rest/bootstraphandlers): whether the server is
// sealed, starting, ready or degraded (documentation/Secrets.md ›
// Sealed start). It needs no session. Unsealing is for unseal.sh only
// (Secrets question 3), so the browser never handles a token.

import { http, type ResponseStatus } from './http/client.ts'
import type { BootstrapState } from './Secrets.ts'

export type { BootstrapState } from './Secrets.ts'

export interface StatusResponse extends ResponseStatus {
  state: BootstrapState
}

export const Bootstrap = {
  // Not captured: the sealed page polls it
  async status(): Promise<StatusResponse> {
    return (await http.get<StatusResponse>('/bootstrap/status', { capture: false, validateStatus: (s) => s === 200 })).data
  },
}
