# Data services

Who owns each table, stream and bucket (forge iteration 21, part 21d). A **data service** is the only code that holds a handle on its storage: every other package calls its operations, which travel between processes like any other service's ([Services.md](Services.md)). With several `serve` processes, the process that runs a data service is the only one that opens its storage; a SQLite file has exactly one such process, chosen by placement, never by a leader (iteration question 9).

## Built in forge 21d-1

- **The guard**: `go test ./features/db/guard` walks every package under `features/`, `cli/` and `cmd/` and fails when one imports a storage package (`gorm.io/gorm`, `github.com/nats-io/nats.go/jetstream`) without a row in `guard.Allow`, or when a row's package no longer imports what it allows, so the list only shrinks. A row names the package, its owner, its storage and the sub-part that removes it (`Until`). `guard.Check(root, dirs, allow)` is also what Forge's guard runs (`forge/backend/features/devtools/guard`, with Forge's own rows). Tests (`_test.go`) are skipped. The guard reads imports only: a handle passed through a method without naming its type (`engine.Engine.DB()`, read by `jobs.trace` and `cognition/drafts`) is listed below and goes in 21d-2.
- **Grants in every process**: an `authz.Policy` reads through an `authz.Source`. `authz.NewPolicy(appDB, reg)` reads the application database. `authz.NewPolicyFrom(authzservice.Remote(proxy, service), reg)` reads through `authzservice.ReadGrants` and `ReadRoles` (Go only, idempotent, service principals only: 403 `services_only` for anyone else), in a process without the application database; it cannot write grants, roles or audits. `Policy.Broadcast(nc)` makes `Changed()` publish `svc.authz.changed`, and `Policy.Follow(nc)` drops the caches on that subject and after a reconnect; `serve.go` does both, so a grant changed in one process reaches every other's cache within a second. `authz.Check` stays a read of the local cache, not a call (21d-1 question 2).
- **Users' names**: records' expansions of a relation to `user` (`recordsdata.Deps.Users`) call `userservice.Names {ids}` (Go only, idempotent; `tenant.read`, the tenant's members only, keyed by id as text) instead of reading the application database.
- **Not moved yet**: `rest`'s `Authenticate` and `identity.Sessions` still read the signing keys and refresh tokens in-process; they move behind `identityservice` with the `web` group (21g-1). `identity.MigrateRoles`, `MarkSuperAdmins` and `Bootstrap` run in the process that holds the application database.

## Built in forge 21d-2

The tenant database's runtime tables are reached only through their data services. Each owner gives an interface its callers hold, implemented over the database in the owning process and over the service's proxy elsewhere:

- **Tool calls**: `tools.Loop.Calls` is a `tools.CallStore` (`SaveCall(args, rec)`, the record's ID set before the save, so a retry stores it once): `conversation.CallsIn(gdb)` in process, `conversationservice.ToolCalls(svc)` through `SaveToolCall` (`conversation.write`; tenant and owner from the principal). `toolsnodes.Deps.Calls` and `conversation.CallHost.Calls` replace their `DB`.
- **Votes**: `retrievaltools.Contribute(reg, func() retrievalservice.Service)`; `retrievaltools.Vote` checks the sandbox and dry run on the caller's side (the context does not travel) and calls `retrievalservice.VoteOnce` (`retrieval.vote`, 400 `bad_vote` for a verdict other than up or down).
- **Cognition traces**: `engine.New(t engine.Traces, reg, cfg)`, `Engine.Traces()` in place of `DB()`. `cognition/traces` (`traces.In(gdb)`, nil for a nil database) is the store; `cognitionservice.Traces(svc)` reaches it through `SaveRun` (with `Begin` as a run starts), `ReadRun` and `PurgeRuns`, service principals only. `jobs.trace` reads a trace through `Traces().Get`, so it no longer depends on where the run ran, and `bp.test` takes the failed node from the run's result.
- **Chronicle memory**: Chronicle's steps keep running where the graph runs, since they call the model and render templates with the run's `tmpl.Env`, which cannot travel (21d-2 question 1: the data behind `chronicleservice`, the steps beside the run). `chroniclesteps.Steps.Memory` is a `chronicle.Memory` (`Entries`, `Links`, `Active`, `Apply` for a memory update's changes in one transaction, `Flag` for a 🚫 round): `chronicle.MemoryIn(gdb)` or `chronicleservice.Memory(svc)` through the `Memory*` operations (`chronicle.read`/`write`; `MemoryApply` is not retried, since it proposes entries).
- **Usage**: `usage.NewRecorder(store usage.Store, …)` batches as before and writes through `Append` (each record and capture once by ID; the recorder sets the IDs) and `PurgeCaptures`: `usage.StoreIn(gdb)` or `usageservice.Store(svc)`, service principals only. Records are still published on `usage.Events` in the recorder's process (buses cross processes in 21f-2).
- **Settings** were already read through `configservice` (`config.Read`), so nothing moved.
- The guard lost the rows of `tools`, `tools/nodes`, `retrieval/tools`, `chronicle/steps` and `cognition/engine` (`cognition/traces` is the owner's), and Forge's allow list is now empty.

## Built in forge 21d-3

- **Followers by database instance**: a follower that copies into the tenant database is named after the database instance, `db.Named(name, gdb)` (`<name>.<instance>`, the instance an id kept in the database's own `records_db_instance` row, `db.Instance`), as the large tables' `records.sql.<instance>.<entity>` already were: `records.entityVersions`, `ui.prefabVersions`, `apps.apps`, `apps.versions` and `apps.installs`. Each database file therefore gets every event, whichever processes run; a database placed on a new instance rebuilds its copies from the stream's beginning (21d-3 question 1: the stream is the truth).
- **Prefab versions through `prefabservice`**: `app.publish` reads the prefab versions it pins through `manifests.Deps.Prefabs` (`prefabservice.GetPrefab`, `manifests.PrefabVersion`), falling back to the copy when no service is wired (tests).
- The converter and the rollups follower send commands and keep no database, so they did not move.
- The guard's 21d-3 rows became owner rows (no `Until`): `records/entities`, `ui/prefabs`, `apps/manifests`, `apps/installs` and `records/data` are the followers and stores of their data services.
- Not yet: `appservice`'s export and import still read the copies in process (they run in the `app` service's own process); the two-process test of one database per tenant waits for 21g-1's harness; running each follower only where its data service is real waits for the locator (21f-1).

## The inventory

| Storage | Owner (data service) | Packages allowed a handle | Removed by |
|---|---|---|---|
| Application DB: users, tenants, memberships, refresh tokens, signing keys | `identity` (`identityservice`, `tenantservice`, `userservice`) | `identity`, the three services' `impl` | stays |
| Application DB: grants, custom roles, access audit | `authz` (`authzservice`) | `authz`, `authzservice/impl` | stays |
| Application DB: secret audit | `secrets` (`secretsservice`) | `secrets`, `secretsservice/impl` | stays |
| Application DB: user log levels | `logs` (`logsservice`) | `logsservice/impl` | stays |
| Tenant DB: settings | `config` | `configservice/impl` | stays |
| Tenant DB: usage records and captures | `usage` | `usage`, `usageservice/impl` | stays |
| Tenant DB: conversations, rounds, segments, feedback, tool calls | `conversation` | `conversation`, `conversationservice/impl` | stays |
| Tenant DB: Chronicle entries, links, rounds, shares; LanceDB `entries` | `chronicle` | `chronicle`, `chronicleservice/impl` | stays |
| Tenant DB: segment feedback and votes | `retrieval` | `retrieval`, `retrievalservice/impl`, `retrievaltest` | stays |
| Tenant DB: Cognition runs and steps | `cognition` | `cognitionservice/impl`, `cognition/traces` | stays |
| Tenant DB: entity versions, prefab copies, app rows, installs, versions; large tables `rec_*` | `entity`, `prefab`, `app`, `record` | the services' `impl`; `records/entities`, `ui/prefabs`, `apps/manifests`, `apps/installs`, `records/data` | stays (followers named per database, 21d-3) |
| Tenant streams, projections, side buckets | `live` | `live/project`, `live/bus`; `live/presence`, `live/nodes`, `activity`, `ui`, `apps/branches` | 21d-4 |
| Record files' object stores | `files` | `records/files` | 21d-4 |
| Job work queues and schedules | `jobs` | `jobs` | stays |
| Connector KV (`cx_*`) | `connectors` | `connectors`, `connections`; `connectors/adapter`, `scripted` | 21e-1 |
| The control account and desired state | `controlplane` | `controlplane` and its daemons' packages | stays (not tenant storage) |
| Opening every store | `serve` | `cli/cmd`, `db`, `live/natsd` | stays |
