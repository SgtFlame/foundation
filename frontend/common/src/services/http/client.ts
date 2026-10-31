// The axios instance every backend client uses. Capture into DevTools is on by
// default; opt out per client with createHttpClient({ capture: false }) or per
// request with { capture: false } in the request config.

import axios, { isAxiosError } from 'axios'
import { installCapture } from './captureInterceptors.ts'
import { newTraceId, TRACE_HEADER } from './trace.ts'

declare module 'axios' {
  interface AxiosRequestConfig {
    // Set on every request: the X-Trace-Id it was sent with
    traceId?: string
    // Set on the identity requests: a 401 never refreshes the session
    noRefresh?: boolean
    // Set on the retry after a refresh, so it is retried once only
    retried?: boolean
  }
}

// Called when a request fails with 401: refreshes the session and tells
// whether to try again (src/session/session.ts sets it).
let onUnauthorized: (() => Promise<boolean>) | undefined

export function setUnauthorizedHandler(handler: () => Promise<boolean>) {
  onUnauthorized = handler
}

export const API_BASE: string = import.meta.env.VITE_API_BASE ?? '/api/v1'

// Thrown when a handler responds with a non-2xx status, or with status 0 when
// the backend could not be reached. errorCode is the server's code for the
// failure (e.g. 'volume_not_found'); trace is the request's trace ID, which
// finds the server's records of it.
export class ServiceError extends Error {
  readonly status: number
  readonly errorCode?: string
  readonly trace?: string

  constructor(status: number, message: string, errorCode?: string, trace?: string) {
    super(message)
    this.name = 'ServiceError'
    this.status = status
    this.errorCode = errorCode
    this.trace = trace
  }
}

// The status fields every response DTO has (backend rest.Status)
export interface ResponseStatus {
  message?: string // the error text
  errorCode?: string
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
  // Registered after capture so that it runs first, and capture records the header
  instance.interceptors.request.use((config) => {
    config.traceId = newTraceId()
    config.headers.set(TRACE_HEADER, config.traceId)
    return config
  })

  instance.interceptors.response.use(undefined, async (error: unknown) => {
    if (!isAxiosError(error)) return Promise.reject(error)
    const config = error.config
    // An expired session is refreshed once, then the request is sent again
    if (error.response?.status === 401 && config && !config.noRefresh && !config.retried && onUnauthorized) {
      if (await onUnauthorized()) return instance({ ...config, retried: true })
    }
    const trace = config?.traceId
    const response = error.response
    if (!response) return Promise.reject(new ServiceError(0, error.message, undefined, trace))
    const body = response.data as ResponseStatus | undefined
    return Promise.reject(
      new ServiceError(response.status, body?.message ?? response.statusText, body?.errorCode, trace),
    )
  })
  return instance
}

export const http = createHttpClient()
