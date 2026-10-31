// The browser's session (documentation/MultiTenant.md › Built in
// slice 10). The tokens are HTTP-only cookies; this keeps what the server
// says about them: whether sign-in is required, who is signed in, and when
// the access token expires. It refreshes the session a minute before then,
// and when a request fails with 401 (through the http client's hook), so
// the streaming clients that use fetch find a fresh token too.

import { useSyncExternalStore } from 'react'
import { Identity, type Account, type SessionResponse } from '../services/Identity.ts'
import { setUnauthorizedHandler } from '../services/http/client.ts'
import { log } from '../logging/Logger.ts'

export type SessionStatus = 'loading' | 'singleUser' | 'signedIn' | 'signedOut'

export interface SessionState {
  status: SessionStatus
  account?: Account
  expiresAt?: number // ms since the epoch
}

const sessionLog = log.In('identity').Tags('session')

// Refresh this long before the access token expires
const refreshEarly = 60_000

let state: SessionState = { status: 'loading' }
const listeners = new Set<() => void>()
let timer: ReturnType<typeof setTimeout> | undefined
let refreshing: Promise<boolean> | undefined
let loading: Promise<SessionState> | undefined

function set(next: SessionState) {
  state = next
  schedule()
  for (const l of listeners) l()
}

// Plans the next refresh from the expiry
function schedule() {
  clearTimeout(timer)
  if (state.status !== 'signedIn' || !state.expiresAt) return
  timer = setTimeout(() => void refresh(), Math.max(state.expiresAt - Date.now() - refreshEarly, 1000))
}

function expiryOf(resp: SessionResponse): number | undefined {
  return resp.expiresAt ? Date.parse(resp.expiresAt) : undefined
}

// Refreshes the session once at a time; false when it has ended
async function refresh(): Promise<boolean> {
  if (state.status === 'singleUser') return false
  refreshing ??= (async () => {
    try {
      const resp = await Identity.refresh()
      set({ ...state, status: 'signedIn', expiresAt: expiryOf(resp) })
      return true
    } catch (err) {
      sessionLog.fromError(err).info('the session has ended')
      set({ status: 'signedOut' })
      return false
    } finally {
      refreshing = undefined
    }
  })()
  return refreshing
}

setUnauthorizedHandler(refresh)

// A tab woken from sleep may hold an expired token
addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'visible' && state.status === 'signedIn' && state.expiresAt && state.expiresAt - Date.now() < refreshEarly) {
    void refresh()
  }
})

export const session = {
  get: () => state,

  subscribe(listener: () => void) {
    listeners.add(listener)
    return () => listeners.delete(listener)
  },

  // Asks the server who is signed in. A session is refreshed first, so its
  // expiry is known and an expired one comes back to life.
  load(): Promise<SessionState> {
    loading ??= (async () => {
      try {
        if ((await Identity.status()).singleUser) {
          set({ status: 'singleUser' })
          return state
        }
        let expiresAt: number | undefined
        try {
          expiresAt = expiryOf(await Identity.refresh())
        } catch {
          set({ status: 'signedOut' })
          return state
        }
        const status = await Identity.status()
        set(status.signedIn && status.account ? { status: 'signedIn', account: status.account, expiresAt } : { status: 'signedOut' })
      } catch (err) {
        sessionLog.fromError(err).warn('cannot read the session')
        set({ status: 'signedOut' })
      } finally {
        loading = undefined
      }
      return state
    })()
    return loading
  },

  // Records a session a sign-in, setup or tenant switch started
  started(resp: SessionResponse) {
    set({ status: 'signedIn', account: resp.account ?? state.account, expiresAt: expiryOf(resp) })
  },

  async logout() {
    try {
      await Identity.logout()
    } catch (err) {
      sessionLog.fromError(err).warn('cannot sign out cleanly')
    }
    set({ status: 'signedOut' })
  },

  // Starts a session in another tenant and reloads the page, so every view
  // loads that tenant's data
  async switchTenant(tenantId: string) {
    const resp = await Identity.switchTenant(tenantId)
    session.started(resp)
    location.reload()
  },

  // The account after the user changed it
  updated(resp: SessionResponse) {
    set({ ...state, account: resp.account ?? state.account, expiresAt: expiryOf(resp) ?? state.expiresAt })
  },
}

// The session, re-rendering when it changes
export function useSession(): SessionState {
  return useSyncExternalStore(session.subscribe, session.get)
}

