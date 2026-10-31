// Client for the backend Extensions REST handlers (backend/features/extensions/rest)

import { http, type ResponseStatus } from './http/client.ts'

export { ServiceError } from './http/client.ts'

// Wire DTOs mirroring backend/features/extensions/rest/extensionsdto

export interface ContributionInfo {
  id: string
  order: number
  source?: string
}

export interface PointInfo {
  id: string
  description: string
  contributions: ContributionInfo[]
}

export interface ListPointsResponse extends ResponseStatus {
  points?: PointInfo[]
}

export const Extensions = {
  async listPoints(): Promise<ListPointsResponse> {
    const response = await http.get<ListPointsResponse>('/extensions/points')
    return response.data
  },
}
