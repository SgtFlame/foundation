// Client for the backend tenant REST handlers
// (backend/features/identity/rest/tenanthandlers), for administrators.

import { http, type ResponseStatus } from './http/client.ts'
import type { Tenant, User } from './Identity.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/identity/rest/tenantdto

export interface TenantSummary extends Tenant {
  members: number
}

export interface ListResponse extends ResponseStatus {
  tenants: TenantSummary[]
}

export interface TenantResponse extends ResponseStatus {
  tenant?: Tenant
}

// A member with the names of the roles they hold on the tenant
export interface Member extends User {
  roles: string[]
}

export interface MembersResponse extends ResponseStatus {
  users: Member[]
}

export const Tenants = {
  async list(): Promise<ListResponse> {
    return (await http.post<ListResponse>('/identity/tenants/list', {})).data
  },

  async create(name: string): Promise<TenantResponse> {
    return (await http.post<TenantResponse>('/identity/tenants/create', { name })).data
  },

  async rename(tenantId: string, name: string): Promise<TenantResponse> {
    return (await http.post<TenantResponse>('/identity/tenants/rename', { tenantId, name })).data
  },

  // The default tenant cannot be deleted; a tenant's content stays for now
  async delete(tenantId: string): Promise<ResponseStatus> {
    return (await http.post<ResponseStatus>('/identity/tenants/delete', { tenantId })).data
  },

  async members(tenantId: string): Promise<MembersResponse> {
    return (await http.post<MembersResponse>('/identity/tenants/members', { tenantId })).data
  },

  async addMember(tenantId: string, userId: string): Promise<ResponseStatus> {
    return (await http.post<ResponseStatus>('/identity/tenants/add-member', { tenantId, userId })).data
  },

  async removeMember(tenantId: string, userId: string): Promise<ResponseStatus> {
    return (await http.post<ResponseStatus>('/identity/tenants/remove-member', { tenantId, userId })).data
  },
}
