# Data services

Who owns each table, stream and bucket (forge iteration 21, part 21d). A **data service** is the only code that holds a handle on its storage: every other package calls its operations, which travel between processes like any other service's ([Services.md](Services.md)). With several `serve` processes, the process that runs a data service is the only one that opens its storage; a SQLite file has exactly one such process, chosen by placement, never by a leader (iteration question 9).

## Built in forge 21d-1

- **The guard**: `go test ./features/db/guard` walks every package under `features/`, `cli/` and `cmd/` and fails when one imports a storage package (`gorm.io/gorm`, `github.com/nats-io/nats.go/jetstream`) without a row in `guard.Allow`, or when a row's package no longer imports what it allows, so the list only shrinks. A row names the package, its owner, its storage and the sub-part that removes it (`Until`). `guard.Check(root, dirs, allow)` is also what Forge's guard runs (`forge/backend/features/devtools/guard`, with Forge's own rows). Tests (`_test.go`) are skipped. The guard reads imports only: a handle passed through a method without naming its type (`engine.Engine.DB()`, read by `jobs.trace` and `cognition/drafts`) is listed below and goes in 21d-2.
- **Grants in every process**: an `authz.Policy` reads through an `authz.Source`. `authz.NewPolicy(appDB, reg)` reads the application database. `authz.NewPolicyFrom(authzservice.Remote(proxy, service), reg)` reads through `authzservice.ReadGrants` and `ReadRoles` (Go only, idempotent, service principals only: 403 `services_only` for anyone else), in a process without the application database; it cannot write grants, roles or audits. `Policy.Broadcast(nc)` makes `Changed()` publish `svc.authz.changed`, and `Policy.Follow(nc)` drops the caches on that subject and after a reconnect; `serve.go` does both, so a grant changed in one process reaches every other's cache within a second. `authz.Check` stays a read of the local cache, not a call (21d-1 question 2).
- **Users' names**: records' expansions of a relation to `user` (`recordsdata.Deps.Users`) call `userservice.Names {ids}` (Go only, idempotent; `tenant.read`, the tenant's members only, keyed by id as text) instead of reading the application database.
- **Not moved yet**: `rest`'s `Authenticate` and `identity.Sessions` still read the signing keys and refresh tokens in-process; they move behind `identityservice` with the `web` group (21g-1). `identity.MigrateRoles`, `MarkSuperAdmins` and `Bootstrap` run in the process that holds the application database.

## The inventory

| Storage | Owner (data service) | Packages allowed a handle | Removed by |
|---|---|---|---|
| Application DB: users, tenants, memberships, refresh tokens, signing keys | `identity` (`identityservice`, `tenantservice`, `userservice`) | `identity`, the three services' `impl` | stays |
| Application DB: grants, custom roles, access audit | `authz` (`authzservice`) | `authz`, `authzservice/impl` | stays |
| Application DB: secret audit | `secrets` (`secretsservice`) | `secrets`, `secretsservice/impl` | stays |
| Application DB: user log levels | `logs` (`logsservice`) | `logsservice/impl` | stays |
| Tenant DB: settings | `config` | `configservice/impl` | stays |
| Tenant DB: usage records and captures | `usage` | `usage`, `usageservice/impl` | stays |
| Tenant DB: conversations, rounds, segments, feedback, tool calls | `conversation` | `conversation`, `conversationservice/impl`; `tools`, `tools/nodes` | 21d-2 (`tools`) |
| Tenant DB: Chronicle entries, links, rounds, shares; LanceDB `entries` | `chronicle` | `chronicle`, `chronicleservice/impl`; `chronicle/steps` | 21d-2 (`steps`) |
| Tenant DB: segment feedback and votes | `retrieval` | `retrieval`, `retrievalservice/impl`, `retrievaltest`; `retrieval/tools`, Forge's `devtools/catalog` | 21d-2 (`tools`, catalog) |
| Tenant DB: Cognition runs and steps | `cognition` | `cognitionservice/impl`; `cognition/engine` (and its `DB()` read by `jobs`, `cognition/drafts`) | 21d-2 |
| Tenant DB: entity versions, prefab copies, app rows, installs, versions; large tables `rec_*` | `entity`, `prefab`, `app`, `record` | the services' `impl`; `records/entities`, `ui/prefabs`, `apps/manifests`, `apps/installs`, `records/data` | 21d-3 |
| Tenant streams, projections, side buckets | `live` | `live/project`, `live/bus`; `live/presence`, `live/nodes`, `activity`, `ui`, `apps/branches` | 21d-4 |
| Record files' object stores | `files` | `records/files` | 21d-4 |
| Job work queues and schedules | `jobs` | `jobs` | stays |
| Connector KV (`cx_*`) | `connectors` | `connectors`, `connections`; `connectors/adapter`, `scripted` | 21e-1 |
| The control account and desired state | `controlplane` | `controlplane` and its daemons' packages | stays (not tenant storage) |
| Opening every store | `serve` | `cli/cmd`, `db`, `live/natsd` | stays |
