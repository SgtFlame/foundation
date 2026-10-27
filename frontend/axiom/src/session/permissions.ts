// What the signed-in user may do, for hiding or disabling what they may not
// (documentation/plans/RBAC.md › Services: useCan). The server's check is
// authoritative; this mirrors its rules over the user's grants from
// GET /authz/me: a grant on a path covers it and everything under it, the
// self actions need no grant, and a user-owned type under "user:<id>" is
// theirs.

import { useEffect, useSyncExternalStore } from 'react'
import { Authz, type MeResponse } from '../services/Authz.ts'
import { log } from '../logging/Logger.ts'
import { session, useSession, type SessionState } from './session.ts'

const permissionsLog = log.In('authz').Tags('permissions')

let me: MeResponse | undefined
let loading: Promise<void> | undefined
const listeners = new Set<() => void>()

function set(next: MeResponse | undefined) {
  me = next
  for (const l of listeners) l()
}

export const permissions = {
  get: () => me,

  subscribe(listener: () => void) {
    listeners.add(listener)
    return () => listeners.delete(listener)
  },

  // Loads (or reloads, after a grant changed) the user's permissions
  load(): Promise<void> {
    loading ??= (async () => {
      try {
        set(await Authz.me())
      } catch (err) {
        permissionsLog.fromError(err).warn('cannot read your permissions')
        set(undefined)
      } finally {
        loading = undefined
      }
    })()
    return loading
  },
}

// Drop what the last user could do when the session changes hands
let lastUser: string | undefined
session.subscribe(() => {
  const s = session.get()
  const user = s.status === 'signedIn' ? s.account?.user.id : s.status
  if (user !== lastUser) {
    lastUser = user
    set(undefined)
  }
})

function segments(path: string): string[] {
  const p = path.replace(/^\/+|\/+$/g, '')
  if (p === '' || p === 'app') return []
  return p.replace(/^app\//, '').split('/')
}

// can mirrors the server's check of action ("<type>.<action>") on resource
export function can(p: MeResponse | undefined, userId: string | undefined, action: string, resource: string): boolean {
  if (!p) return false
  if (p.all || p.self.includes(action)) return true
  const path = segments(resource)
  const type = action.split('.')[0]
  if (userId && p.ownedTypes.includes(type) && path.includes(`user:${userId}`)) return true
  return p.grants.some((h) => {
    if (h.grant.expiresAt && Date.parse(h.grant.expiresAt) <= Date.now()) return false
    const g = segments(h.grant.resource)
    return g.every((s, i) => path[i] === s) && g.length <= path.length && h.permissions.includes(action)
  })
}

// The user's ID as the server sees it: the reserved first user in
// single-user mode
export function userIdOf(s: SessionState): string | undefined {
  return s.status === 'singleUser' ? '1' : s.account?.user.id
}

// The resource path of the user's tenant, e.g. "tenant:1"
export function tenantOf(s: SessionState): string {
  return `tenant:${s.account?.tenantId ?? '1'}`
}

export type Can = (action: string, resource?: string) => boolean

// useCan gives a function telling whether the user may do an action on a
// resource (their tenant when none is given), loading the permissions once
export function useCan(): Can {
  const s = useSession()
  const p = useSyncExternalStore(permissions.subscribe, permissions.get)
  const ready = s.status === 'signedIn' || s.status === 'singleUser'
  useEffect(() => {
    if (ready && !permissions.get()) void permissions.load()
  }, [ready, p])
  const user = userIdOf(s)
  const tenant = tenantOf(s)
  return (action, resource) => can(p, user, action, resource ?? tenant)
}

// canIn reads a contribution context's can (the Editors view passes
// useCan's): whether a command's when() should show it
export function canIn(ctx: Record<string, unknown>, action: string, resource?: string): boolean {
  return typeof ctx.can === 'function' && (ctx.can as Can)(action, resource)
}
