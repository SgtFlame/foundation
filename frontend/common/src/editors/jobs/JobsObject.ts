// Administration › Jobs: the tenant's background jobs (forge 07a)
export class JobsObject {
  readonly kind = 'jobs'
  readonly id = 'main'
  readonly name = 'Jobs'
  readonly helpText = 'Background jobs and app triggers: queued, running, retrying, dead and done, with each run’s trace'
}
