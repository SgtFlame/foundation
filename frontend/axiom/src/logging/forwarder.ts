// Forwards browser records to the server's log storage (POST /logs/ingest)
// in batches, from the level the logging settings name (warn by default), so
// they sit next to the server's records of the same trace in the Log
// Explorer (documentation/plans/Logging.md).

import { API_BASE } from '../services/http/client.ts'
import { Logs, type BrowserError, type BrowserRecord } from '../services/Logs.ts'
import { configStore } from '../settings/configStore.ts'
import { getAt } from '../settings/schema.ts'
import { logLevels, type LogErrorInfo, type LogLevel, type LogRecord } from './LogRecord.ts'
import { addLogSink, log } from './Logger.ts'

const flushEvery = 2000
const batchSize = 50
const maxBatch = 200 // the server's limit
const maxQueued = 500

const forwardLog = log.In('logs').Tags('forward')

let queue: BrowserRecord[] = []
let sending = false

// This user's own level while full logging is on for them, from the server
let own: { level: LogLevel; until: number } | undefined
const ownEvery = 5 * 60_000

async function loadOwnLevel() {
  try {
    const resp = await Logs.level()
    own = resp.until && logLevels.includes(resp.browserLevel as LogLevel)
      ? { level: resp.browserLevel as LogLevel, until: Date.parse(resp.until) }
      : undefined
  } catch (err) {
    forwardLog.fromError(err).debug('cannot read this user\'s log level')
  }
}

// The lowest level forwarded, from the logging settings once they load, or
// this user's own level while it lasts
function lowest(): LogLevel {
  const level = getAt(configStore.entry('logging', 'app').view?.value, 'browserLevel')
  const setting: LogLevel = logLevels.includes(level as LogLevel) ? (level as LogLevel) : 'warn'
  if (own && own.until > Date.now() && logLevels.indexOf(own.level) < logLevels.indexOf(setting)) return own.level
  return setting
}

function errorOf(info?: LogErrorInfo): BrowserError | undefined {
  if (!info) return undefined
  return { name: info.name, message: info.message, stack: info.stack, cause: errorOf(info.cause) }
}

// Attributes that cannot be serialized are sent as text
function serializable(attrs: Record<string, unknown>): Record<string, unknown> | undefined {
  const out: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(attrs)) {
    try {
      out[key] = JSON.parse(JSON.stringify(value) ?? 'null')
    } catch {
      out[key] = String(value)
    }
  }
  return Object.keys(out).length ? out : undefined
}

function toBrowserRecord(record: LogRecord): BrowserRecord {
  return {
    time: record.time.toISOString(),
    level: record.level,
    message: record.message,
    domain: record.domain,
    tags: record.tags.length ? record.tags : undefined,
    code: record.code,
    trace: record.trace,
    span: record.span,
    hint: record.hint,
    public: record.public,
    attrs: serializable(record.attrs),
    error: errorOf(record.error),
    url: location.pathname,
  }
}

function accept(record: LogRecord) {
  // The forwarder's own failures are never forwarded
  if (record.domain === 'logs' && record.tags.includes('forward')) return
  if (logLevels.indexOf(record.level) < logLevels.indexOf(lowest())) return
  queue.push(toBrowserRecord(record))
  if (queue.length > maxQueued) queue = queue.slice(queue.length - maxQueued)
  if (queue.length >= batchSize) void flush()
}

async function flush() {
  if (sending || queue.length === 0) return
  sending = true
  const batch = queue.slice(0, maxBatch)
  try {
    await Logs.ingest({ records: batch })
    queue = queue.slice(batch.length)
  } catch (err) {
    // Kept for the next flush, unless the server refused them
    const status = (err as { status?: number }).status ?? 0
    if (status >= 400 && status < 500) queue = queue.slice(batch.length)
    forwardLog.fromError(err).debug('cannot forward %d log records', batch.length)
  } finally {
    sending = false
  }
}

// What is still queued when the page goes away
function flushOnExit() {
  if (queue.length === 0) return
  const body = new Blob([JSON.stringify({ records: queue.slice(0, maxBatch) })], { type: 'application/json' })
  if (navigator.sendBeacon(`${API_BASE}/logs/ingest`, body)) queue = []
}

let started = false

// Starts forwarding once there is a session; RequireSession calls it
export function startLogForwarding() {
  if (started) return
  started = true
  void loadOwnLevel()
  setInterval(() => void loadOwnLevel(), ownEvery)
  // Subscribing keeps the level current through the config event stream
  configStore.subscribe(() => {})
  configStore.ensure('logging', 'app')
  addLogSink(accept)
  setInterval(() => void flush(), flushEvery)
  addEventListener('pagehide', flushOnExit)
}
