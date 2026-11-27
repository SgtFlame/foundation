45. **The job runner and triggers** (Forge iteration 7, part 7a, [forge 007a-JobRunner.md](../../forge/documentation/plans/007a-JobRunner.md)). *Built* ([Jobs.md](Jobs.md#built-in-slice-45)).
    - **Built**: durable jobs on JetStream (CQRS state on `EVT_<tenant>_jobs`, dispatch through the work queue `JOBS_<tenant>`, message schedules for delays and *On schedule*, keyed jobs in order), installed apps' triggers as the app's own principal, *Enqueue job* on a command's outbox, *Cancel job*, *Fail*, *Job info*, Administration › Jobs; built for several runners per tenant in several processes.
    - **Blockers**: none; see the questions table below.
    - **Decisions to review after seeing it**: in [Jobs.md](Jobs.md#decisions-to-review-after-seeing-it).
