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

## Built in forge 21d-4

- **Two kinds of holders**: the live core (`live/project`, `live/bus`, the projectors, followers and trigger consumers, presence and activity) holds the JetStream handle in the data and runner processes; everyone else calls it (21d-4 question 1: one `live` service, the area an argument).
- **`svc.live` for Go callers**: `liveservice` gained `Execute` and `Transact` (a command or batch as the args' principal, as `bus.Execute`/`bus.Transact`; idempotent by the command id), `Read` (one projected key, service principals only, 403 `services_only`) and `Subscribe` (a query as a 21c stream of snapshots, changes and the failure that ends it; params default to `{}`), beside `Serve`, the browser's socket. `liveimpl.WithStore` gives `Read` its store; without the bus or store the operations answer 503 `live_core_unavailable`.
- **`svc.files`**: `filesservice` (`records/services/filesservice`, Go only) keeps the record files' object stores: `PutPart` (an upload's part, at most `files.PartSize` = 256 KiB, kept as an object of its own so the parts may reach any process), `Put` (the parts as one file uploaded by the principal, 413 `file_too_large`), `Uploader`, `Get` (a stream: the reference, then the content in parts) and `Delete`. `files.Files` (`data.Files` with `Open` and `Delete`) is what `recordservice` and *Parse transcript* take: the `*files.Store` in its own process, `filesservice.Remote(svc)` elsewhere. Callers check access to the records first, as `data.Upload` and `recordservice.OpenFile` do.
- **UI sessions** stay in the process holding the browser's socket, which holds `ui_sessions_*` (21d-4 question 2: a handler's every event would otherwise make two hops).
- The guard's 21d-4 rows became owner rows, each naming whose storage it is: the live core's (`live/presence`, `live/nodes`, `activity`, `apps/branches`), `ui`'s sessions and `files`' store.
- Not yet: `liveservice.Serve` calling `svc.live` when the core is elsewhere (it calls the bus in its own process), presence and activity through `svc.live`, and the test of a `web` process with no JetStream handle serving the Tasks app, a chat round and an upload, which wait for 21g-1's groups and harness.

## Built in forge 21e-1

- **`connectors.Books`** is the bookkeeping an adapter runtime keeps: announcements (`Announce`, `Withdraw`, `Announcements`), discovered contracts, the call ledger, exclusive locks, event sources (`Sources`, a watch with `Ready` once the current ones are in), their leases (`LeaseCreate` failing with `ErrLeaseTaken`, `LeaseUpdate` with `ErrLeaseLost`, `LeaseDelete`), cursors, and events published into `CX_<tenant>`. `connectors.InJetStream(js)` keeps them in the `cx_*` buckets (`connectors.BooksOn(nc)` for a connection's JetStream).
- **The adapter runtime holds no JetStream handle**: `adapter.Options.Books` (default `BooksOn(nc)`), and `adapter.Context` writes the ledger and discovered contracts through it; `scripted.Deps.Books` replaces `JS` for its cursors and *Emit connector event*.
- **`svc.connectors`** (`connectors/services/connectorsservice`, Go only, service principals only, 403 `services_only`) keeps the books for adapters in other processes: `Announce`, `Withdraw`, `Announcements`, `PutDiscovered`, `LedgerPut`, `TryLock`/`Unlock` (the remote `Lock` tries every 250 ms until its context ends), `Sources` (a 21c stream), `LeaseCreate`/`LeaseUpdate` (409 `lease_taken`, `lease_lost`), `LeaseDelete`, `Cursor`, `PutCursor` and `Publish` (400 `bad_event`). `connectorsservice.Remote(svc)` is `connectors.Books` over it: an adapter given it as `Options.Books` announces itself, serves calls, leases and runs event sources through the service (21e-1 question 1: one service).
- A server stopping now ends its streams in flight (`services.Server.Stop`), so their callers see them lost rather than the server waiting for them.
- The guard lost `connectors/adapter` and `connectors/scripted`; `connectorsservice/impl` joined `connectors` and `connectors/connections` (which answers `cx.resolve` and `cx.status`) as the holders.
- Not yet: `adapter.Context.Services` (records, files and connections proxies built from the type's manifest, 21f-1); `cx.resolve` checked against the call token rather than headers; the Mail types in their own process (21g-1's harness).

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
| Tenant streams, projections, side buckets | `live` | `live/project`, `live/bus`; `live/presence`, `live/nodes`, `activity`, `ui`, `apps/branches` | stays (others call `svc.live`, 21d-4) |
| Record files' object stores | `files` (`filesservice`) | `records/files` | stays (others call `svc.files`, 21d-4) |
| Job work queues and schedules | `jobs` | `jobs` | stays |
| Connector KV (`cx_*`) | `connectors` (`connectorsservice`) | `connectors`, `connections`, `connectorsservice/impl` | stays (adapters call `svc.connectors`, 21e-1) |
| The control account and desired state | `controlplane` | `controlplane` and its daemons' packages | stays (not tenant storage) |
| Opening every store | `serve` | `cli/cmd`, `db`, `live/natsd` | stays |
