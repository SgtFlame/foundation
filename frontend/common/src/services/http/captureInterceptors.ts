// Interceptors that record every exchange on an axios instance into
// networkStore and the log. A request opts out with { capture: false }.

import {
  AxiosHeaders,
  isAxiosError,
  type AxiosInstance,
  type AxiosResponse,
  type RawAxiosHeaders,
} from 'axios'
import { log } from '../../logging/index.ts'
import { networkStore } from './NetworkStore.ts'

declare module 'axios' {
  interface AxiosRequestConfig {
    // false keeps this request out of DevTools; defaults to true
    capture?: boolean
    // Set by the capture interceptor: the request's NetworkEntry id
    captureId?: number
  }
}

const httpLog = log.In('http')

let nextId = 1

// Request and response headers as a plain name → value map
function headersOf(headers: object | undefined): Record<string, string> {
  if (!headers) return {}
  const json = AxiosHeaders.from(headers as RawAxiosHeaders).toJSON()
  return Object.fromEntries(
    Object.entries(json).map(([name, value]) => [name, String(value)]),
  )
}

function sizeOf(response: AxiosResponse): number | undefined {
  const length = Number(response.headers['content-length'])
  if (Number.isFinite(length) && length > 0) return length
  if (response.data === undefined || response.data === '') return 0
  const text = typeof response.data === 'string' ? response.data : JSON.stringify(response.data)
  return new Blob([text]).size
}

function complete(id: number, startedAt: number, response: AxiosResponse) {
  const endedAt = Date.now()
  networkStore.update(id, {
    state: 'done',
    endedAt,
    durationMs: endedAt - startedAt,
    status: response.status,
    statusText: response.statusText,
    responseHeaders: headersOf(response.headers),
    responseBody: response.data,
    sizeBytes: sizeOf(response),
  })
}

export function installCapture(instance: AxiosInstance) {
  instance.interceptors.request.use((config) => {
    if (config.capture === false || networkStore.paused) return config
    const id = nextId++
    config.captureId = id
    const method = (config.method ?? 'get').toUpperCase()
    const url = `${config.baseURL ?? ''}${config.url ?? ''}`
    networkStore.push({
      id,
      state: 'pending',
      startedAt: Date.now(),
      method,
      url,
      requestHeaders: headersOf(config.headers),
      requestBody: config.data,
    })
    httpLog.Tags(method).Trace(config.traceId ?? '').With('id', id, 'url', url).debug('→ %s %s', method, url)
    return config
  })

  instance.interceptors.response.use(
    (response) => {
      const id = response.config.captureId
      const entry = id && networkStore.snapshot().find((entry) => entry.id === id)
      if (entry) {
        complete(entry.id, entry.startedAt, response)
        httpLog
          .Tags(entry.method)
          .Trace(response.config.traceId ?? '')
          .With('id', entry.id, 'url', entry.url, 'status', response.status)
          .debug('← %d %s %s', response.status, entry.method, entry.url)
      }
      return response
    },
    (error: unknown) => {
      const id = isAxiosError(error) ? error.config?.captureId : undefined
      const entry = id && networkStore.snapshot().find((entry) => entry.id === id)
      if (!entry || !isAxiosError(error)) return Promise.reject(error)

      const logger = httpLog
        .Tags(entry.method)
        .Trace(error.config?.traceId ?? '')
        .With('id', entry.id, 'url', entry.url)
        .fromError(error)
      if (error.response) {
        complete(entry.id, entry.startedAt, error.response)
        const status = error.response.status
        const level = status >= 500 ? 'error' : 'warn'
        logger.With('status', status)[level]('← %d %s %s', status, entry.method, entry.url)
      } else {
        const endedAt = Date.now()
        networkStore.update(entry.id, {
          state: 'failed',
          endedAt,
          durationMs: endedAt - entry.startedAt,
          error: error.message,
        })
        logger.error('✕ %s %s: %s', entry.method, entry.url, error.message)
      }
      return Promise.reject(error)
    },
  )
}
