// Every request carries a trace ID in X-Trace-Id, so its browser records and
// the server's records for it share one trace (documentation/plans/Logging.md).

export const TRACE_HEADER = 'X-Trace-Id'

// 32 random hex digits, the same form the server makes
export function newTraceId(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}
