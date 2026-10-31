// A structured log record, modelled on the context carried by samber/oops errors

export type LogLevel = 'debug' | 'info' | 'warn' | 'error'

export const logLevels: readonly LogLevel[] = ['debug', 'info', 'warn', 'error']

export interface LogUser {
  id: string
  attrs: Record<string, unknown>
}

export interface LogErrorInfo {
  name: string
  message: string
  stack?: string
  cause?: LogErrorInfo
}

export interface LogRecord {
  id: number
  time: Date
  level: LogLevel
  message: string
  domain?: string
  tags: string[]
  code?: string
  trace?: string
  span?: string
  hint?: string
  public?: string
  user?: LogUser
  attrs: Record<string, unknown>
  error?: LogErrorInfo
}
