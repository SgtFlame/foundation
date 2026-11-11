# Jobs

Durable background work on NATS JetStream: a **job** runs one published graph with inputs, as the principal it names, once, and is retried with backoff when it fails in a way worth retrying. An installed app's **triggers** (`app.trigger` graphs) run as jobs, on each event their event node starts on and on each tick of a schedule. Built as Forge's part 7a ([forge 07a-JobRunner.md](../../forge/documentation/plans/07a-JobRunner.md)), slice 45. It answers Cognition question 3 (background runs).

## Built in slice 45

### Built for several processes

One `serve` process runs today, but the target is several runners per tenant, in several processes on several machines, sharing one JetStream. Every piece of shared state is in JetStream, every step is idempotent by an id, and races are settled by JetStream rather than by Go locks. No runner is a leader:

| Concern | How it holds across processes |
|---|---|
| A job's state | CQRS: `job.*` commands commit events on `EVT_<tenant>_jobs`, folded by the `jobs.job` projection. A cancel racing a start is settled by the bus's expected-sequence commit: one wins, and the other is refused. |
| Each job runs once | Work messages go to the work-queue stream `JOBS_<tenant>`, with one durable pull consumer per queue (`run_<queue>`) shared by every runner. A message is delivered to one runner and removed when it is acknowledged. |
| A runner dies mid-run | The runner extends the message's ack wait while the run lives (`InProgress` every third of it). If the runner dies, the message comes back after the ack wait and another runner takes it. `job.start` marks the old attempt `lost`. |
| Delays, *Undo send* | A delayed job is a JetStream message schedule (`Nats-Schedule: @at …`, rounded up to the second) on `jobs.<t>.at.<id>`, which the server puts on the job's queue when it is due. *Cancel* purges the schedule, and `job.start` refuses a canceled job anyway. |
| *On schedule* | One message schedule per installed trigger (`@every 5m`, or a cron line with `Nats-Schedule-Time-Zone` for `at`), on `jobs.<t>.every.<app>.<trigger>`, sent with rollup and a message id per schedule. Every runner may send it, and it stays one schedule. Each tick's job id is `<app>-<trigger>-<tick sequence>`. |
| One key at a time, in order | A job's subject is `jobs.<t>.run.<queue>.<key hash>`. A keyed job runs only when it is the earliest message left on its subject (a next-by-subject read). A running job is still unacknowledged, so the next job of its key waits. |
| Idempotency | The job id is the target of `job.enqueue`, so a second enqueue of the id commits nothing, however late it comes. A dispatch's message id is `<id>#<dispatch>`, and a finish's command id carries the dispatch and attempt. |
| Triggers | Each installed event trigger gets a durable consumer on its event stream (`trg_<app>_<trigger>`, metadata `{app, trigger}`), shared by every runner. It starts at the install's or upgrade's time and keeps its position across upgrades. Every runner reconciles the same consumers and schedules from the `apps.install` projection: create, update the filter, or delete, each idempotent. |
| A command graph's jobs | The outbox rides on the commit's first event (`live.Event.Jobs`). The area's outbox follower (`fol_jobs_outbox_<area>`, a shared durable consumer made with `FollowNew` before the first commit that needs it) enqueues the jobs. Other runners find it by its name. |
| Which runner serves what | `Runner.Serve(tenant)` attaches a process to a tenant. `Runner.Start(tenants)` serves what a function lists, now and every minute. Each runner discovers queues and outboxes other processes made (every 5 s). |

### Jobs

- **A job** (`jobs.Request`): `id` (its idempotency key), `graph`, `event` (the event node type it starts from: *On call* for a `server.action`, the trigger's event node for a trigger), `trigger`, `inputs`, `as` (a `features.PrincipalRef`), `app` (whose installed version's copy of the graph runs), `queue`, `key`, `runAt`, `maxAttempts` and `request`. Its state (`jobs.Job`) adds `status` (`queued`, `running`, `retrying`, `done`, `dead`, `canceled`), `attempts`, `retryAt`, `error`, the times, `by` (who canceled or retried it) and `runs`. The runs are the last 20 attempts, each `{attempt, runner, runId, status (running, ok, failed, lost), error, failedNode, version}`.
- **Commands** (area `jobs`, target type `job`, projection `jobs.job`):
  - `job.enqueue` (internal, `job.create`): a job runs as whoever enqueues it, and only the service principal (the outbox, triggers) enqueues for another principal.
  - `job.start` and `job.finish` (internal, service principal only): a finish for an attempt that is no longer current commits nothing.
  - `job.cancel` (a job not yet started) and `job.retry` (a dead or canceled job, its attempts counted afresh), as tools. Either acts on your own jobs, or on anyone's with `job.manage`.
- **Running**: the runner reads the job and checks that it is not finished, not due later, and not a copy of a running job's message. It then sends `job.start` and runs the graph through the engine as `job.As.In(tenant)`, traced in full (`Trigger: "job:<id>"`), with the job's `jobs.Info` in the context. Afterwards it sends `job.finish`:
  - done: the message is acknowledged.
  - retry: the message is handed back with `NakWithDelay` until `retryAt`. It keeps its place, so later jobs of its key still wait.
  - dead: the message is acknowledged.
  A runner that closes leaves its running jobs alone.
- **Retries**: `jobs.Classify(err)` decides whether to retry. *Fail* `{retryable, after}` gives a `*jobs.Failure`, and any error in the chain with `Retryable() bool` (and `RetryAfter()`) counts, such as 7b's connector errors. The backoff starts at the `jobs` section's `backoffSeconds` and doubles up to `maxBackoffSeconds`, for at most `maxAttempts` (8); then the job is dead. An error not marked retryable is dead at once.
- **The graph**: an app's job (`app` set) or a trigger runs the installed version's copy of its graph (`Version.Graphs`; a trigger's from `Version.Trigger`). Otherwise the tenant's published graph runs. A job whose app is uninstalled, or whose graph is gone, is dead with that reason.
- **Settings**: the `jobs` section (app and tenant scopes): `maxConcurrent` per queue per server (4), `queues: {name: {maxConcurrent}}`, `maxAttempts` (8), `backoffSeconds` (2), `maxBackoffSeconds` (600), `doneHours` (24).
- **Access**: the resource type `job` has these actions:
  - `job.read` (read): see your own jobs.
  - `job.create` (execute): enqueue jobs, and cancel or retry your own.
  - `job.manage` (admin): every job of the tenant.

### The install principal

An app's triggers run as **the app's own principal** (`features.AppPrincipal(tenant, app)`, kind `app`). It has no user row and holds the app's roles in that tenant and nothing else (`authz.AppGrants`): no tenant role, and no other app's roles. Its row rules are its roles' rules, and record rules see `principal.kind == "app"` and `principal.app`. Its commands' events carry `actor: "app:<appId>"` (`bus.ActorOf`), so the audit and the op log show the app. It is never the service principal.

### Triggers

- A manifest trigger names its app's own `app.trigger` graph (`graph`; a trigger without one is only declared) and optionally a `queue`. `apps.CheckManifest` refuses a graph that is missing, of another kind, or not the app's own.
- `app.publish` pins trigger graphs, and also every app-owned graph a pinned graph names in a node's `graph` config (an *Enqueue job*'s, *Call graph*'s), transitively (`apps.GraphsNamed`).
- The graph's event node says when it runs:
  - *On record changed* `{entity, events}`: created, updated (including items added or removed) and deleted, on a type the app principal may `record.read`. Its outputs are `record` (read as the app when the run starts), `old`, `event` and `id`.
  - *On event* `{type, events}`: the app's own area `app_<appId>`. Its outputs are `event`, `data`, `type` and `target`.
  - *On schedule* `{every}` or `{at, timeZone}`.
  - *On webhook* still has no runs (forge 11).
- Event trigger nodes register a `livenodes.TriggerEvent` (`live.triggerEvent`): its subjects for a config, checked as the app principal, and its outputs for an event, read as the job's principal. 7b's *On connector event* will register one too.
- A trigger does not start on events its own app's principal committed, so a trigger that changes what it watches does not run forever.
- A trigger that cannot start yet keeps its app pending, and every discovery tries again. The usual reason is that the app's roles are not synced yet, by whichever process syncs them. The warning is logged once per reason.
- Uninstalling deletes the app's trigger consumers and purges its schedules. Upgrading reconciles them: a kept trigger keeps its consumer's position, and a new one starts at the upgrade.

### Nodes

- *Enqueue job* (`jobs.enqueue`; command, server.action, app.trigger and ui.handler graphs):
  - Config: `graph` and `queue`. Inputs: `inputs`, `key`, `delay` (`10s`, or seconds), `runAt` and `id`. Output: the job's `id`.
  - In a command graph it adds to the decision's outbox (`live.Decision.Enqueue`), so the job is enqueued if and only if the command's events commit. A decision that enqueues but commits no event is refused. `NodeType.PureIn` lets this effect node into the pure command kind.
  - Elsewhere it calls `Runner.Enqueue`, which checks the graph. A sandboxed run enqueues nothing.
- *Cancel job* (`jobs.cancel`): continues by *Canceled* or *Not canceled*.
- *Fail* (`jobs.fail`, category Flow; server.action and app.trigger): `{retryable}` with `message` and `after`.
- *Job info* (`jobs.info`): `id`, `attempt`, `event`, `app` and `trigger`. A command keyed by the job id commits once, however often the job runs.

### Queries and the Jobs view

- `jobs.list {app?, status?, graph?}` (`job.read`) lists the jobs keyed `job.<id>`, live. A user sees their own jobs; an app's jobs when they may `app.read` the app; and every job with `job.manage`. Done and canceled jobs are listed for `doneHours`.
- `jobs.trace {id, runId}` gives one attempt's trace in `/cognition/trace`'s shape to whoever may see the job (traces are otherwise owner-scoped).
- **Administration › Jobs** (`frontend/common/src/editors/jobs/`, kind `jobs`, shown with `job.read`; types in `services/Jobs.ts`) is a live table:
  - Rows: status, graph, app › trigger, runs as, attempts, enqueued, last error; a status filter and a search box.
  - A row opens to its details, inputs and attempts, each with its trace (the failed node's error and inputs).
  - *Retry* and *Cancel* send the commands.

### Live core additions

- `live.Job`, `live.Outbox`, `Decision.Enqueue`/`Jobs`, and `Event.Jobs` on a commit's first event, in `Execute`, `Transact` and the dry run (whose jobs are dropped).
- `bus.ActorOf`.
- `project.FollowNew`: a follower made at the stream's end, kept afterwards. Also `Following` and `FollowerName`.
- `cognition.Exec.GraphID` and `Owner`; `cognition.NodeType.PureIn`.
- The `live.triggerEvent` point.

### Decisions

Answered before building (2026-11-11), each with its proposal:

- 07a question 1: a principal per install, shown as the app.
- Question 2: the outbox on the commit's first event (like the op log), not a `jobQueued` event, which the records projector and followers would have had to skip.
- Forge iteration question 9: the runner is built in iteration 7.
- Question 10: one Foundation slice per Foundation part.
- The user's direction: build for several runners per tenant in several processes.

Decided while building:

1. **Job state is CQRS, dispatch is a work queue.** Commands and a projection give `jobs.list` and `jobs.trace` for free, make *Retry* and *Cancel* tools, and settle races. The work queue gives each message to one runner.
2. **JetStream schedules, not a scheduler.** The server (2.15) fires `@at`, `@every` and cron schedules with time zones, so delays and *On schedule* need no lease and no leader. A delay is rounded up to the second, and a job delivered early goes back until it is due.
3. **Key order by subject.** A keyed job waits until it is the earliest message on its key's subject. A delayed job is not on its subject until it is due, so it does not hold its key early.
4. **A job's copy of a running message** (a dispatch resent after the 10-minute duplicate window) is dropped when its job is running and the message is delivered for the first time. A redelivered message is taken as its runner's death.
5. **Jobs run as the enqueuer**, enforced by `job.enqueue`: only the server's own work names another principal.
6. **The plan's `jobs_done_<tenant>` KV is the projection.** A finished job's id stays in `jobs.job`, so a duplicate is dropped for as long as the job is kept.
7. **The nodes live in `features/jobs/nodes`**, not `live/nodes`, which `jobs` imports. *Fail* is new: no *Fail* node existed.
8. **A trigger ignores its own app's events** (loop guard). A chain inside an app uses *Enqueue job*.
9. **Traces stay in the tenant database** of the process that ran the run. `jobs.trace` reads them there.

### Decisions to review after seeing it

- Whether the Jobs view's dead job and trace say enough to fix it (07a ★).
- The loop guard (decision 8): an app's trigger never sees its own app's changes.
- Done jobs stay in the projection and the jobs stream forever, though listed only for `doneHours` (see *Not built*).

### Verification

`go test ./features/jobs` runs an in-process stack (live core, records, apps, installs, engine), with each simulated process its own projections, bus and runner over one JetStream:

- a job enqueued while no runner serves runs once when one starts, and its id again is dropped;
- a runner killed mid-run (after its graph committed a record keyed by the job id) leaves the job to another, which runs attempt 2; the first is `lost`, and one record exists;
- a retryable *Fail* backs off 1 s between three attempts, then dead; `jobs.trace` names the failed node; *Retry* runs it three more times; a non-retryable *Fail* is dead at once;
- a delay waits; a canceled delayed job never runs; a done job cannot be canceled; a user cannot enqueue as another;
- five jobs with one key over two runners never overlap and run in order; unkeyed jobs overlap;
- thirty jobs over three runners each run once, spread across runners;
- *On record changed* runs as `app:notes` with the record. A note made while no runner runs is seen once by the next two. An uninstall stops it and deletes its consumer;
- *On schedule* every 1 s with two runners: each tick once, as the app; the uninstall stops it;
- a command graph's job is enqueued only when the command commits; a second process takes over the area's outbox;
- `jobs.list`; the manifest refuses a trigger graph of another kind, another app's, or none;
- with access control installed and roles synced by the follower, a trigger starts once its app may read.

`authz`'s `TestAppPrincipal` covers the app principal's grants and rules, and `apps/tools`' `TestMetadata` covers the new nodes and commands.

In Forge (the user's app), on a fresh `serve` with Vite, a Playwright walk over the page's live socket made an app with two *On record changed* triggers, published and installed it, and made a note:

- The reconcile first found the app's roles not synced and logged it once. The retry then started both triggers.
- One trigger ran as `app:notes` and updated the note (`_updatedBy` 0); its own update did not start it again.
- The other trigger's *Fail* made a dead job.
- Administration › Jobs showed both jobs, the dead one's error, inputs, attempt and trace. *Retry* ran it again, live, on a restarted server.

### Not built

- **Retention.** Job events are kept forever, like every stream's, and done jobs stay in the projection. A `MaxAge` on the jobs stream, with done jobs dropped from the projection after `doneHours`, would bound them, at the cost of idempotency for ids older than that.
- **Per-record row rules on triggers.** A trigger starts on every change of a type its app may read. The record it reads is filtered by its rules when the run starts.
- **Run time limits for jobs.** A job's run has the `cognition` section's wall time; a mail sync may need more.
- *On webhook* (forge 11), cron expressions in the editor (forge 11), and *On connector event* (7b).
