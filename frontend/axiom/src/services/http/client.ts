// The axios instance every backend client uses. Capture into DevTools is on by
// default; opt out per client with createHttpClient({ capture: false }) or per
// request with { capture: false } in the request config.

import axios, { isAxiosError } from 'axios'
import { installCapture } from './captureInterceptors.ts'

export const API_BASE: string = import.meta.env.VITE_API_BASE ?? '/api/v1'

// Thrown when a handler responds with a non-2xx status, or with status 0 when
// the backend could not be reached
export class ServiceError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ServiceError'
    this.status = status
  }
}

export interface HttpClientOptions {
  baseURL?: string
  capture?: boolean
}

export function createHttpClient({ baseURL = API_BASE, capture = true }: HttpClientOptions = {}) {
  const instance = axios.create({
    baseURL,
    headers: { 'Content-Type': 'application/json' },
  })
  if (capture) installCapture(instance)

  instance.interceptors.response.use(undefined, (error: unknown) => {
    if (!isAxiosError(error)) return Promise.reject(error)
    const response = error.response
    if (!response) return Promise.reject(new ServiceError(0, error.message))
    const body = response.data as { message?: string } | undefined
    return Promise.reject(
      new ServiceError(response.status, body?.message ?? response.statusText),
    )
  })
  return instance
}

export const http = createHttpClient()
