// Helpers for showing and granting roles on a resource, shared by the Share
// dialog and the Role Editor

import type { Role } from '../../services/Authz.ts'
import { Tenants } from '../../services/Tenants.ts'
import { Users } from '../../services/Users.ts'

export interface Person {
  id: string
  name: string
}

// The people who may be given roles on resource: the application's users
// for "app", else the tenant's members
export async function peopleFor(resource: string, tenantId: string): Promise<Person[]> {
  const users = resource === 'app' ? (await Users.list()).users : (await Tenants.members(tenantId)).users
  return users.map((u) => ({ id: u.id, name: u.displayName ? `${u.displayName} (${u.username})` : u.username }))
}

// Whether a role may be granted on resource: app roles only on "app"
export function grantable(role: Role, resource: string): boolean {
  return role.scope === 'app' ? resource === 'app' : resource.startsWith('tenant:')
}

// The ancestors of a path, nearest first: "tenant:1/volume:4" → ["tenant:1", "app"]
export function ancestors(resource: string): string[] {
  const parts = resource === 'app' ? [] : resource.split('/')
  const out: string[] = []
  for (let i = parts.length - 1; i > 0; i--) out.push(parts.slice(0, i).join('/'))
  if (resource !== 'app') out.push('app')
  return out
}

export function describeResource(resource: string): string {
  if (resource === 'app') return 'the application'
  const last = resource.split('/').pop() ?? resource
  const [kind, id] = last.split(':')
  return { tenant: 'the tenant', volume: `Volume ${id}`, kb: 'the knowledge base folders' }[kind] ?? last
}
