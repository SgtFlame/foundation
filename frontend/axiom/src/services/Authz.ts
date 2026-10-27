// Client for the backend authz REST handlers
// (backend/features/authz/rest/authzhandlers): what the principal may do,
// resource types, roles and grants (documentation/plans/RBAC.md).

import { http, type ResponseStatus } from './http/client.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/authz/rest/authzdto

export type ActionClass = 'read' | 'write' | 'create' | 'delete' | 'share' | 'moderate' | 'execute' | 'admin' | 'self'

export interface Action {
  name: string
  title: string
  class: ActionClass
  on: 'app' | 'tenant'
}

export interface ResourceType {
  id: string
  title: string
  source: string
  parent: string
  owned?: boolean
  actions: Action[]
}

export interface Role {
  id: string
  name: string
  description?: string
  scope: 'app' | 'tenant'
  builtIn?: boolean
  permissions: string[] // '*' is every permission
  revision?: number
}

export interface Grant {
  id: string
  userId: string
  roleId: string
  resource: string // 'app', 'tenant:1', 'tenant:1/volume:42'
  grantedBy: string
  createdAt: string
  expiresAt?: string
}

export interface Held {
  grant: Grant
  roleName: string
  permissions: string[]
}

export interface MeResponse extends ResponseStatus {
  all?: boolean
  grants: Held[]
  self: string[]
  ownedTypes: string[]
}

export interface ResourceTypesResponse extends ResponseStatus {
  types: ResourceType[]
}

export interface RolesResponse extends ResponseStatus {
  roles: Role[]
}

export interface RoleResponse extends ResponseStatus {
  role?: Role
}

export interface GrantsResponse extends ResponseStatus {
  grants: Grant[]
}

export interface GrantResponse extends ResponseStatus {
  grant?: Grant
}

export interface Decision {
  allowed: boolean
  reason: string
  grantId?: string
  roleId?: string
}

export interface ExplainResponse extends ResponseStatus {
  decision: Decision
}

export interface SaveRoleRequest {
  roleId?: string
  name: string
  description: string
  permissions: string[]
  revision: number
}

export interface GrantsRequest {
  resource?: string
  under?: boolean
  roleId?: string
}

export interface GrantRequest {
  userId: string
  roleId: string
  resource: string
  expiresAt?: string
}

export const Authz = {
  async me(): Promise<MeResponse> {
    return (await http.get<MeResponse>('/authz/me')).data
  },

  async resourceTypes(): Promise<ResourceTypesResponse> {
    return (await http.get<ResourceTypesResponse>('/authz/resource-types')).data
  },

  // The built-in roles, then the tenant's custom roles
  async roles(): Promise<RolesResponse> {
    return (await http.get<RolesResponse>('/authz/roles')).data
  },

  // 409 role_conflict when the revision is stale
  async saveRole(request: SaveRoleRequest): Promise<RoleResponse> {
    return (await http.post<RoleResponse>('/authz/roles/save', request)).data
  },

  async deleteRole(roleId: string): Promise<ResponseStatus> {
    return (await http.post<ResponseStatus>('/authz/roles/delete', { roleId })).data
  },

  async grants(request: GrantsRequest): Promise<GrantsResponse> {
    return (await http.post<GrantsResponse>('/authz/grants', request)).data
  },

  async grant(request: GrantRequest): Promise<GrantResponse> {
    return (await http.post<GrantResponse>('/authz/grant', request)).data
  },

  async revoke(grantId: string): Promise<ResponseStatus> {
    return (await http.post<ResponseStatus>('/authz/revoke', { grantId })).data
  },

  // Whether a user (none: you) may do action on resource, and why
  async explain(action: string, resource: string, userId?: string): Promise<ExplainResponse> {
    return (await http.post<ExplainResponse>('/authz/explain', { action, resource, userId })).data
  },
}
