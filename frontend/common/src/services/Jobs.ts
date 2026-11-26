// Background jobs (forge 07a, documentation/Jobs.md): read with the live
// jobs.list query, changed with the job.cancel and job.retry commands.
// There is no REST client: everything goes over the live connection.

export type JobStatus = 'queued' | 'running' | 'retrying' | 'dead' | 'done' | 'canceled'

export const jobStatuses: JobStatus[] = ['queued', 'running', 'retrying', 'dead', 'done', 'canceled']

export type RunStatus = 'running' | 'ok' | 'failed' | 'lost'

// One attempt of a job; runId names its Cognition run, whose trace
// Cognition.getTrace reads
export interface JobRun {
  attempt: number
  runner?: string
  runId?: string
  status: RunStatus
  error?: string
  failedNode?: string
  version?: number
  startedAt: string
  finishedAt?: string
}

export interface JobPrincipal {
  kind: 'user' | 'service' | 'app'
  userId?: string
  app?: string
}

// A job as jobs.list gives it
export interface Job {
  id: string
  graph: string
  event?: string
  trigger?: string
  inputs?: unknown
  as: JobPrincipal
  app?: string
  queue: string
  key?: string
  runAt?: string
  maxAttempts?: number
  request?: string
  status: JobStatus
  attempts: number
  enqueuedAt: string
  startedAt?: string
  finishedAt?: string
  retryAt?: string
  error?: string
  runs: JobRun[]
  by?: string
}

// jobs.list's params
export interface JobsListParams {
  app?: string
  status?: JobStatus
  graph?: string
}

// jobs.list's result: jobs keyed job.<id>
export type JobsList = Record<string, Job>

// Who a job runs as, in words
export function principalText(p: JobPrincipal): string {
  switch (p.kind) {
    case 'user':
      return `User ${p.userId ?? ''}`
    case 'app':
      return `App ${p.app ?? ''}`
    default:
      return 'The server'
  }
}
