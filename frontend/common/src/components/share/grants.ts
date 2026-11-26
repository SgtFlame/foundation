// Helpers for showing and granting roles on a resource, shared by the Share
// dialog and the Role Editor

import { Authz, type Grant, type GrantRequest, type Role } from '../../services/Authz.ts'
import { Tenants } from '../../services/Tenants.ts'
import { Users } from '../../services/Users.ts'

// Someone a role can be granted to: a person (id their user id) or an
// agent (forge 07j; id "agent:<id>", agent set)
export interface Person {
  id: string
  name: string
  agent?: boolean
}

// The tenant's agents as grantees; none for "app" or when they cannot be read
export async function agentsFor(resource: string): Promise<Person[]> {
  if (resource === 'app') return []
  try {
    return (await Authz.agents(resource)).agents.map((a) => ({ id: `agent:${a.id}`, name: a.name, agent: true }))
  } catch {
    return []
  }
}

// Whom a grant is to, as a Person's id
export function granteeOf(g: Grant): string {
  return g.agentId && g.agentId !== '0' ? `agent:${g.agentId}` : g.userId
}

// The grantee half of a grant request for a Person's id
export function granteeRequest(id: string): Pick<GrantRequest, 'userId' | 'agentId'> {
  return id.startsWith('agent:') ? { agentId: id.slice('agent:'.length) } : { userId: id }
}

// The people who may be given roles on resource: the application's users
// for "app", else the tenant's members
export async function peopleFor(resource: string, tenantId: string): Promise<Person[]> {
  const users = resource === 'app' ? (await Users.list()).users : (await Tenants.members(tenantId)).users
  return users.map((u) => ({ id: u.id, name: u.displayName ? `${u.displayName} (${u.username})` : u.username }))
}

// Whether a role may be granted on resource: app roles only on "app"
export function grantable(role: Role, resource: string): boolean {
  if (role.scope === 'app') return resource === 'app'
  // An app's role: on the tenant, or on the app and below
  if (role.app) return /^tenant:\d+$/.test(resource) || resource.split('/')[1] === `app:${role.app}`
  return resource.startsWith('tenant:')
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
  return { tenant: 'the tenant', volume: `Volume ${id}`, kb: 'the knowledge base folders', record: `record ${id}` }[kind] ?? last
}
