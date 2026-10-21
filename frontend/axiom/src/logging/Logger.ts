// Structured logging modelled on samber/oops: an immutable builder collects
// context (domain, tags, code, attributes, ...) and a terminal method either
// logs a record (debug/info/warn/error) or builds an OopsError (Errorf/Wrap).
//
//   log.In('kb').Tags('volume').With('volumeId', id).info('opened %s', name)
//   throw log.In('kb').Code('volume_open_failed').Wrap(err)
//   log.fromError(err).error('could not open volume')

import type {
  LogErrorInfo,
  LogLevel,
  LogRecord,
  LogUser,
} from './LogRecord.ts'
import { logStore } from './LogStore.ts'

interface LogContext {
  domain?: string
  tags: string[]
  code?: string
  trace?: string
  span?: string
  hint?: string
  public?: string
  user?: LogUser
  attrs: Record<string, unknown>
}

const emptyContext: LogContext = { tags: [], attrs: {} }

// Thrown errors carrying the builder's context, like oops.OopsError
export class OopsError extends Error {
  readonly context: LogContext
  override readonly cause?: unknown

  constructor(message: string, context: LogContext, cause?: unknown) {
    super(message)
    this.name = 'OopsError'
    this.context = context
    this.cause = cause
  }
}

// printf-style formatting: %s %d %i %f %v %j %o, and %% for a literal %
export function format(template: string, args: readonly unknown[]): string {
  let next = 0
  const formatted = template.replace(/%([sdifvjo%])/g, (match, verb) => {
    if (verb === '%') return '%'
    if (next >= args.length) return match
    const arg = args[next++]
    switch (verb) {
      case 'd':
      case 'i':
        return String(Math.trunc(Number(arg)))
      case 'f':
        return String(Number(arg))
      case 'j':
      case 'o':
        return stringify(arg)
      default:
        return typeof arg === 'object' && arg !== null && !(arg instanceof Error)
          ? stringify(arg)
          : String(arg)
    }
  })
  const rest = args.slice(next).map((arg) => (typeof arg === 'string' ? arg : stringify(arg)))
  return rest.length ? `${formatted} ${rest.join(' ')}` : formatted
}

function stringify(value: unknown): string {
  try {
    return JSON.stringify(value) ?? String(value)
  } catch {
    return String(value)
  }
}

function errorInfo(error: unknown): LogErrorInfo | undefined {
  if (error === undefined) return undefined
  if (!(error instanceof Error)) return { name: 'Error', message: String(error) }
  return {
    name: error.name,
    message: error.message,
    stack: error.stack,
    cause: errorInfo(error.cause),
  }
}

let nextId = 1

export class Logger {
  private readonly context: LogContext
  private readonly err?: unknown

  constructor(context: LogContext = emptyContext, err?: unknown) {
    this.context = context
    this.err = err
  }

  // This builder's context layered over a wrapped OopsError's context
  private contextOver(error: unknown): LogContext {
    const inner = error instanceof OopsError ? error.context : emptyContext
    return {
      ...inner,
      ...definedOnly(this.context),
      tags: [...inner.tags, ...this.context.tags],
      attrs: { ...inner.attrs, ...this.context.attrs },
    }
  }

  private derive(patch: Partial<LogContext>): Logger {
    return new Logger({ ...this.context, ...patch }, this.err)
  }

  // Builder methods, named after their oops counterparts

  In(domain: string) {
    return this.derive({ domain })
  }

  Tags(...tags: string[]) {
    return this.derive({ tags: [...this.context.tags, ...tags] })
  }

  Code(code: string) {
    return this.derive({ code })
  }

  Trace(trace: string) {
    return this.derive({ trace })
  }

  Span(span: string) {
    return this.derive({ span })
  }

  Hint(hint: string) {
    return this.derive({ hint })
  }

  Public(message: string) {
    return this.derive({ public: message })
  }

  User(id: string, attrs: Record<string, unknown> = {}) {
    return this.derive({ user: { id, attrs } })
  }

  // With('k1', v1, 'k2', v2, ...) adds attributes, like oops.With
  With(...pairs: unknown[]) {
    const attrs = { ...this.context.attrs }
    for (let i = 0; i + 1 < pairs.length; i += 2) attrs[String(pairs[i])] = pairs[i + 1]
    return this.derive({ attrs })
  }

  WithContext(attrs: Record<string, unknown>) {
    return this.derive({ attrs: { ...this.context.attrs, ...attrs } })
  }

  // Attaches an error to the records this builder logs. An OopsError's
  // context is merged underneath this builder's.
  fromError(error: unknown) {
    return new Logger(this.contextOver(error), error)
  }

  // Error builders: return an OopsError without logging it

  New(message: string) {
    return new OopsError(message, this.context)
  }

  Errorf(template: string, ...args: unknown[]) {
    return new OopsError(format(template, args), this.context)
  }

  Wrap(error: unknown) {
    return this.Wrapf(error, error instanceof Error ? error.message : String(error))
  }

  Wrapf(error: unknown, template: string, ...args: unknown[]) {
    return new OopsError(format(template, args), this.contextOver(error), error)
  }

  // Terminal logging methods

  debug(template: string, ...args: unknown[]) {
    this.write('debug', template, args)
  }

  info(template: string, ...args: unknown[]) {
    this.write('info', template, args)
  }

  warn(template: string, ...args: unknown[]) {
    this.write('warn', template, args)
  }

  error(template: string, ...args: unknown[]) {
    this.write('error', template, args)
  }

  private write(level: LogLevel, template: string, args: readonly unknown[]) {
    const record: LogRecord = {
      id: nextId++,
      time: new Date(),
      level,
      message: format(template, args),
      ...this.context,
      error: errorInfo(this.err),
    }
    logStore.push(record)
    if (import.meta.env.DEV) mirror(record)
  }
}

function definedOnly(context: LogContext): Partial<LogContext> {
  return Object.fromEntries(
    Object.entries(context).filter(([, value]) => value !== undefined),
  )
}

function mirror(record: LogRecord) {
  const prefix = [
    record.domain && `[${record.domain}]`,
    ...record.tags.map((tag) => `#${tag}`),
  ]
    .filter(Boolean)
    .join(' ')
  const details: Record<string, unknown> = { ...record.attrs }
  if (record.code) details.code = record.code
  if (record.trace) details.trace = record.trace
  if (record.hint) details.hint = record.hint
  if (record.user) details.user = record.user
  const extra: unknown[] = []
  if (Object.keys(details).length) extra.push(details)
  if (record.error) extra.push(record.error)
  // The one place console is allowed: mirroring records in dev builds
  console[record.level](prefix ? `${prefix} ${record.message}` : record.message, ...extra)
}

export const log = new Logger()
