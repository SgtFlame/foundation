# Connectors

Services outside the tenant (a mail server, an HTTP API, a SaaS), described by metadata and called from blueprints. A **contract** is a named, versioned API of operations and events. A **connector type** implements contracts. Every call is a NATS request/reply message on the **connector bus**, answered by a **connector adapter** that may run in the server's process or in another process on another machine. Built as Forge's part 7b ([forge 07b-ConnectorRegistry.md](../../forge/documentation/plans/07b-ConnectorRegistry.md)), slice 46. Connections (settings and secrets, sessions, event sources) come with 7c, and scripted connectors with 7d.

## Built in slice 46

### Packages

| Package | What it holds |
|---|---|
| `features/connectors` | The metadata (`Contract`, `Operation`, `EventDef`, `CallbackDef`, `Type`, `SurfaceInfo`, `Meta`), typed errors, subjects and headers, the metadata check, the tool mappings, announcements and discovered contracts in KV, the `Registry`, the `Dispatcher`, the `connector` resource type and the `connectors.*` queries |
| `features/connectors/adapter` | The adapter runtime: `Serve(ctx, nc, Options)`, `Impl`, `Session`, `Context`, `Connection`, and the interfaces declared for later parts (`Watcher`, `Describer`, `Receiver`, `Registrar`, `Surface`, `Gateway`) |
| `features/connectors/nodes` | *Call connector* (`connector.call`) and *On connector event* (`connector.on`) |
| `features/connectors/connectorstest` | The test contract `test.echo@1` and the builtin test types `test.echo` (no connection) and `test.secure` (a connection with a secret password), with their adapter options. Test code only. |

### Contracts and types

- **A contract** (`Contract{ID, Version, Meta, Operations, Events}`) is referenced as `id@version` (`Ref`, `ParseRef`). An incompatible change is a new version beside the old (07b question 2). A node names its version, and a type implements one version of each contract.
- **An operation** has `id`, `title`, `description`, `args` and `result` (JSON Schemas, type object), `examples` (args with their result), `effect` (`read`, `write` or `send`), `destructive` (07b question 8), `key` (the arg that makes it idempotent: a top-level field or a dotted path to a nested one, such as `mail.transport` `send`'s `message.messageId`, read by `KeyOf`; added with Forge's 7e), `errors` (the codes it may return), `callbacks` (events that answer it later, each with a `correlate {result, event}` pair of field paths) and `remote` (a discovered operation's outside name).
- **An event** has `id`, `title`, `description`, `payload` (a JSON Schema, type object), `example`, `idFields` (the payload fields that make it unique, its future `Nats-Msg-Id`), `delivery` (`watch`, `callback` or both) and `subscription` (the service's own name for it).
- **A type** (`Type`) has `id`, `contracts` (refs with versions), `meta`, `settings` (Configuration's schema language; fields of type `secret` go to `SecretStore` with 7c), `noConnection`, `source` (`builtin` or `scripted`), `discovers` (its contract comes from each connection) and `receives` (a callback scheme: `stripe`, `github`, `standard-webhooks`, `hmac-sha256`).
- **Builtin contracts and types** are contributions to the `connector.contract` and `connector.type` points (`BuiltinContract`, `BuiltinType`), registered with `connectors.ContributeBuiltins(reg, source, contracts, types)` from a feature's `Contribute`. `serve.go` declares both points. Foundation's server has no builtin types; Forge's contributes the mail contracts and `mail.mime` (Forge [MailConnectors.md](../../forge/documentation/MailConnectors.md#built-in-part-7e)).
- **Errors** (`*connectors.Error{code, message, retryable, retryAfter}`) use the codes `auth`, `notFound`, `conflict`, `invalid`, `rateLimited`, `unavailable` and `notSupported`. `Errorf` makes `unavailable` and `rateLimited` retryable. The error has `Retryable()` and `RetryAfter()`, so `jobs.Classify` retries a job that fails with a retryable one. `AsError` turns any other error into a non-retryable `unavailable`.

### The metadata check

`CheckContract`, `CheckType` and `CheckSurface` return problems at paths such as `operations.echo.args.properties.text`. They check:

- **Metadata everywhere:** a title, a description for a model and tags on the contract and type; a title and description on each operation and event; a description on each top-level field of args, result and payload.
- **Ids:** dotted words of letters, digits and `-`, never `_` (subject tokens map `.` to `_`). A type may not be called `surface`.
- **The tool shape:** `<contract>.<operation>` is at most 64 characters. Args and result are objects at the top, with no `oneOf`/`anyOf`/`allOf`/`not` there, and no `$ref`, `$defs`, `definitions`, `patternProperties` or `$dynamicRef` anywhere.
- **Examples:** at least one per operation, whose args and result pass their schemas (`tools.CheckArgs`); each event's example passes its payload.
- **Names that must exist:** a `key` names an arg; `idFields` name payload fields; a callback names a declared event, and its correlation names a field of the result and of the event's payload.
- **Types:** they name contracts with versions, never two versions of one contract, contracts that exist (when given a lookup), a settings schema that parses, and a known callback scheme.

`apps/tools`' `TestMetadata` runs the check over every builtin contract and type, and `TestMetadataRefusesBadConnectors` shows it failing. The registry runs the same check on every announcement.

### Tool-shaped metadata

These pure functions of the metadata each have golden files in `connectors/testdata/golden` (rewrite them with `go test ./features/connectors -update`):

- `ToolSpec(contract, op)`: a `tools.Spec` named `<contract>.<operation>`, with source `connector` and the effect mapped (`ToolEffect`: `read`→`read`, `write`→`write`, `send`→`external`).
- `MCPTool(contract, op)`: an MCP `Tool` of revision `MCPRevision` (2025-11-25). It has the wire name (`.`→`_`), `inputSchema` and `outputSchema`, and `annotations`: `readOnlyHint` for read, `destructiveHint` for a destructive write, `idempotentHint` for a keyed or read operation, `openWorldHint` for send. Each test operation's tool validates against the MCP schema's `#/$defs/Tool` (`testdata/mcp-2025-11-25.schema.json`). `MCPError` gives a typed error as a failed tool result (`isError`).
- `FunctionSpec(contract, op, vendor)`: Gemini's `functionDeclarations` entry, Claude's tool (`input_schema`) and OpenAI's function tool, for `gemini`, `anthropic` and `openai`.
- `OpenAPI(contract)`: an OpenAPI 3.1 document. Each operation is `POST /<operation>`, its args the request body, its result the 200 and a typed `Error` the default response. Its callbacks are operation `callbacks`, keyed by `{$request.header.Cx-Hook-Url}`. The contract's events are top-level `webhooks`. `x-effect`, `x-destructive`, `x-idempotency-key`, `x-errors`, `x-delivery`, `x-id-fields`, `x-subscription`, `x-correlate`, `x-contract` and `x-tags` keep what OpenAPI has no word for, so 15e can read the document back. The test contract's document validates against the OpenAPI 3.1 schema (`testdata/openapi-3.1.schema.json`).

### The connector bus: protocol

The bus is plain NATS with JSON bodies, so an adapter could be written in any language (07b question 5). This is the protocol in full; iteration 15 adds the `cx.in` subjects to it.

**Tokens.** An id in a subject is its token: `.` replaced by `_` (`mail.imap` → `mail_imap`).

**Subjects:**

| Subject | Kind | What |
|---|---|---|
| `cx.call.<tenant>.<type>.<connection>.<operation>` | request/reply | A call. `_` is the connection of a type that needs none. |
| `cx.event.<tenant>.<connection>.<contract>.<event>` | JetStream, `CX_<tenant>` | A connector event (7c), pushed or polled alike |
| `cx.changed.<tenant>.<connection>` | plain | A connection changed: drop its sessions |
| `cx.discovered.<tenant>.<connection>` | plain | A connection's discovered contract changed |
| `cx.in.<tenant>.<exposure>.<method>`, `cx.in.notify.<session>` | reserved | The inbound gateway (15a). Nothing subscribes to `cx.in.>` yet. |

**Queue groups.** Every instance serving a type subscribes to `cx.call.*.<type>.>` in the queue group `cx.<type>`, so NATS gives each call to one instance. An adapter serving some tenants subscribes to `cx.call.<tenant>.<type>.>` instead.

**Request headers:**

| Header | Value |
|---|---|
| `Cx-Call` | The call id, a TSID |
| `Cx-Contract` | `id@version` (or `conn.<connection>` for a discovered contract) |
| `Cx-Principal` | The caller as `features.PrincipalRef` JSON (`{kind, userId, app}`); the tenant is the subject's |
| `Cx-Meta` | `features.Meta` as JSON, for usage attribution |
| `Cx-Trace` | The trace id |
| `Cx-Deadline` | RFC 3339 with nanoseconds. An adapter drops a request past it unanswered. |
| `Cx-Key` | The idempotency key, when the operation has one |

The request body is the operation's args, a JSON object.

**Reply.** `{"result": {...}}` or `{"error": {"code", "message", "retryable", "retryAfter"}}` (`retryAfter` in milliseconds). Secrets never appear in a request or a reply.

**Announcements.** Each instance writes one value per type to KV `cx_adapters`, keyed `<type>.<instance>`, and one per surface, keyed `surface.<surface>.<instance>`. The value is `connectors.Announcement`: `{adapter, instance, type + contracts | surface, tenants?, at, expiresAt}`. An instance refreshes its values every `AnnounceEvery` (5 s), and each stays online until `expiresAt` (`TTL`, three times that). A graceful stop deletes them; a killed process's values just expire. The bucket keeps an unrefreshed value for 10 minutes.

**Discovered contracts.** KV `cx_discovered_<tenant>` maps `<connection>` to `{contract, problems, at}`. The adapter writes it through `Context.PutDiscovered`, which leaves out the operations and events failing the check (listing them as `problems`) and publishes `cx.discovered.<tenant>.<connection>`. The contract's id is `conn.<connection>`.

**Callbacks.** KV `cx_hooks` (15f) maps `<hookId>` to `connectors.Hook{tenant, connection, type}`; only its shape is fixed here.

Slice 47 adds `cx.resolve`, `cx.status`, the ledger, locks, event sources and `CX_<tenant>` to the protocol: see [Built in slice 47 › Protocol additions](#protocol-additions).

### The registry

`connectors.NewRegistry(reg, js)` merges, per tenant:

- the builtin contracts and types;
- the types and surfaces announced in `cx_adapters`, each listed as `announced`;
- discovered contracts, read on demand.

Each process keeps a cache of `cx_adapters`, kept current by a KV watch: a cache of JetStream state, the same in every process. A ticker (1 s) notices announcements expiring.

- **Online.** A type is online while an announcement of it serves the tenant and has not expired. A builtin type with no adapter running is listed offline. A contract is online when one of its types is. `NeedsConnection` is set when some type implementing a contract needs a connection.
- **Refusals.** An announcement that cannot be read, or whose contracts, type or surface fail the check, is left out and logged once per revision with code `announcement_refused`, naming each problem.
- **`Catalog(ctx, tenant)`** lists contracts (with their source, types, online and needs-connection), types (with their source, direction `outbound`, online and instances) and surfaces (direction `inbound`).
- **`Contract(ctx, tenant, ref)`** takes `id@version`, `id` alone (its latest version) or `conn.<connection>`.
- **`Describe(ctx, tenant, ref)`** takes `contract:`, `connector:`, `surface:` or `connection:` refs.
- **`Changes(ctx)`** signals every change.

**Queries** (action `connector.read`, class read, on the tenant). Each sends the whole value again when the registry or a discovered contract changes it:

- `connectors.contracts {search?}`
- `connectors.types {contract?, search?, direction?}`
- `connectors.describe {ref}`

**The catalog.** `forge.catalog {area: "connectors"}` lists `contract:<id@version>` and `connector:<type>` items, tagged `offline` when they are. `forge.describe` takes those refs. With no registry (tests), it lists the builtins, offline. `ForgeDeps.Connectors` is the registry.

### The dispatcher

`connectors.Dispatcher{NC, Registry, Connections, Timeout}` is the caller's side. It holds no state, and any process may call any connector. `Call(args, Call{Contract, Operation, Connection, Args})`:

1. finds the contract and operation (a missing one is `notSupported`) and checks the args against the operation's schema (`invalid`);
2. resolves the type:
   - a connection's type comes from `Connections.TypeOf` (7c; until then it is given, and `notSupported` without it), and must implement the contract;
   - a discovered contract names its connection;
   - otherwise it takes a type implementing the contract that needs no connection, an online one first;
3. sends the request with the headers above and a deadline (`Timeout`, default 30 s, or the context's earlier one);
4. returns the reply.

A call no adapter serves gets NATS's *no responders* at once and returns retryable `unavailable`; so does a timeout. The dispatcher never retries: a job does. The ledger for keyed operations, `use` on the connection, and the connection status change on `auth` come with 7c.

### The adapter runtime

`adapter.Serve(ctx, nc, Options{Name, Types, Contracts, Impls, Surfaces, Tenants, Resolve, AnnounceEvery, TTL, MaxConcurrent})` serves until `ctx` ends. The same code runs in-process, on the server's own NATS connection (07b question 3: always through NATS), and in an external binary.

- It refuses options where a described type has no `Impl`, or an `Impl` has no type.
- It subscribes to each type's calls in its queue group and runs up to `MaxConcurrent` (64) calls at once.
- It refuses a call for a contract the type does not implement (`notSupported`), and drops one past its deadline.
- **Sessions.** It opens one session per tenant, type and connection with `Impl.Open(ctx, conn)` and keeps it until `cx.changed` names the connection or the adapter stops. `Options.Resolve` gives the connection's settings, secrets included; 7c's `cx.resolve` replaces it (07b question 4). `Connection.Settings` is never marshalled, and nothing logs it.
- **Calls.** `Session.Call(ctx, op, args)` runs one. A `*connectors.Error` is replied as it is; any other error, or a panic, is `unavailable`.
- **`adapter.Context`** carries the caller's `ServiceArgs` (principal in the subject's tenant, Meta), the call id, contract, operation and key, and a logger carrying the trace. The tenant's file store and the ledger writer join it with 7c.
- **Announcements** are written at once and every `AnnounceEvery`. A stop unsubscribes, waits for running calls, deletes its announcements and closes its sessions.
- **Surfaces.** `Surfaces` are announced, and each `Listen` runs with a `Gateway` that refuses every request (`notSupported`) until 15a. `Watcher` is run by 7c, `Describer` by 15c, and `Receiver`/`Registrar` by 15f; they are declared only.

`serve.go` (Foundation's and Forge's) makes the registry and dispatcher. It starts an in-process adapter from `connectorImpls()` when that lists implementations: none in Foundation's; Forge's lists `mail.mime` (7e).

### Nodes

- **Call connector** (`connector.call`, *Connectors*, `Effects`, `External`). Its config is `{contract, operation, catchRetryable?}`, and `Normalize` completes it from the registry when the node is added, changed or imported:
  - `contract` becomes `id@version`;
  - `inputs` is one pin per top-level arg, typed `string`, `number`, `bool` or `json`, optional unless required (a nested object is one `json` pin, 07b question 1);
  - `outputs` is one pin per result field;
  - `connection` is set when some type needs a connection (not for a discovered contract, whose connection is in its id);
  - `effect`, `title`, and `sample` (the first example's result).

  Its pins are exec in, an optional `connection` input, the args, exec out, the exec *Error* (`failed`), the result fields and `error` (the typed error). It sends the call through the dispatcher as the run's principal. A typed error leaves by *Error*, except a retryable one, which fails the run so a job retries it, unless `catchRetryable`. `NodeType.SampleFor` (new) gives a stubbed run the operation's result example, so `forge.test` and `bp.test` stub it with no adapter.
- **On connector event** (`connector.on`, an `app.trigger` event node, allowed in the trigger kind). Its config is `{contract, event, connection?}`; its outputs are `connection`, `event` and the payload's fields, from `Normalize`. The job runner runs it from `CX_<tenant>` with 7c.
- **`bp.import`** now normalizes each node it can. A config that cannot be normalized, such as a *Call graph* whose callee is imported later, is kept as exported.

### Decisions

Answered before building (2026-11-11), each with its proposal: questions 1 (nested objects as one pin), 2 (versions side by side), 4 (`cx.resolve` in 7c), 5 (the protocol written here, the Go runtime only), 6 (the bidirectional shapes here, the gateway and adapters in 15), 7 (claiming declared contracts in 15c), 8 (`destructive`), 9 (no durable wait for a callback) and 10 (callback metadata and interfaces here, the endpoint in 15f). Question 3 (always through NATS) was the user's direction.

Decided while building:

1. **Online by `expiresAt`, not KV TTL.** A KV entry that expires sends no watch event, so each announcement carries its own `expiresAt` and the registry checks it on a ticker. The bucket's 10-minute TTL only clears entries out.
2. **`Announcement` carries its contracts.** An external adapter's contracts reach the registry with its type, and the registry checks them together.
3. **The dispatcher picks a type for connectionless calls**, an online one first. With connections (7c) the connection decides.
4. **The Error exec pin is `failed`**, since the data output `error` takes that id.
5. **A retryable error fails the run** unless *Catch retryable* is on, so a job retries it with backoff without wiring.
6. **Pins come from `Normalize`** (like *Call graph*): the config carries them, so a graph resolves without the registry, and a contract change shows when the node is next normalized.
7. **The `connector.contract`/`connector.type` contributions wrap the metadata** (`BuiltinContract`, `BuiltinType`), since contributions embed `extensions.Descriptor`.
8. **A test JSON Schema validator** (`santhosh-tekuri/jsonschema/v6`, test-only) checks the MCP tool and OpenAPI document against the published schemas, kept in `testdata`.

### Decisions to review after seeing it

- The *Connectors* palette, and how a call node with many pins reads (07b ★).
- Whether a discovered contract's operations read well beside declared ones (first seen with 15c).
- Retryable errors failing the run by default (decision 5).

### Verification

`go test ./features/connectors/...`:

- the test contract and types pass the check; the check refuses a 70-character name, a `$ref`, non-object args, a top-level `oneOf`, an undescribed operation or field, a bad correlation on either side, an id with `_`, a bad example, a bad delivery, a key naming no arg, a type without a version and a type with two versions;
- each test operation's `ToolSpec`, `MCPTool` and three `FunctionSpec`s match their golden files, and the MCP tool validates against the MCP schema, which refuses a string input. The OpenAPI document matches its golden file and validates as OpenAPI 3.1, with `webhooks` and `callbacks`;
- **in-process:**
  - `describe` gives the contract with both types (offline, then online);
  - `echo` returns its result;
  - `fail` returns `notFound` (not retryable) and `rateLimited` (retryable);
  - bad args are `invalid`, and an unknown contract is `notSupported`;
- **no adapter:** `unavailable` at once and retryable; after an adapter starts, the call succeeds;
- **two instances:** 40 calls are shared between them. With 60 concurrent calls and one instance stopped after 20, every call is served after retries;
- **external process:** the test binary run again as a helper, connected over TCP. Its type is announced and online, and calls reach it. Killed with no goodbye, the type goes offline when its announcement expires, and calls are `unavailable`;
- **secrets:** a tap on `>` and the logs at debug, during calls on a connection whose settings hold a secret (including a failed call and an unknown connection), never see it;
- **refusals:** a bad announcement (an undescribed operation) is refused and logged with its path; a good one beside it is listed;
- **queries and both directions:**
  - a stub surface (its `Listen` blocking until canceled, the gateway refusing) is listed by `connectors.types {direction: inbound}`;
  - `connectors.contracts` turns online when an adapter starts;
  - `connectors.describe {ref: connection:42}` gives the discovered contract without its 70-character operation, listed under `problems`, and follows a rediscovery;
  - nothing is sent on `cx.in.>`.

`go test ./features/connectors/nodes`, on the live core, drafts, engine and a job runner:

- *Call connector*'s pins follow the schemas (`then connection? text repeat? options:json?` in, `then failed text length by error` out); an unknown operation is refused by `bp.setConfig`;
- in a job, with no adapter, the job retries; once an adapter starts, it is done with the result;
- a `notFound` takes *Error* once and the job is done; a `rateLimited` retries;
- `forge.test` stubs the node with `hihi`, the example's result, with no adapter;
- a discovered `conn.77` gives pins without a connection input.

`apps/tools`' `TestMetadata` checks the builtin contracts and types, and `TestCatalog` the `connectors` area and `forge.describe` of `contract:` and `connector:` refs.

In Forge, the Blueprint Editor was checked in headless Chromium. The setup was a scratch program running a NATS server with a listener and the test adapter, with `serve --nats-url` pointed at it, the way an external adapter runs. On a server-action graph:

- the Palette's *Connectors* category listed *Test echo: Echo*, *Fail* and *Start a job*; *On … Finished* is left out, since that kind allows no trigger node;
- double-clicking *Echo* and dragging *Fail* onto the canvas drew their pins from the schemas, required inputs marked until connected;
- the details panel showed the contract and operation.

### Not built

- ~~Connections, `cx.resolve`, `use` checks, the ledger, status on `auth`, event sources, `CX_<tenant>` and *On connector event* starting triggers~~: built in [slice 47](#built-in-slice-47). The *Connection* picker in *Call connector*'s details panel is still to build.
- ~~Scripted types in the registry (7d)~~: built in slice 48. ~~The mail contracts (7e)~~: built in Forge ([MailConnectors.md](../../forge/documentation/MailConnectors.md#built-in-part-7e)); the SMTP, IMAP and POP3 types are 7f to 7h.
- `bp.check` re-normalizing nodes when an announced contract changes, and the editor redrawing pins at once. A change shows when the node's config is next set or the graph is imported.
- A *Call connector* in a chat tool asking for approval by its operation's effect (graph tools ask by their own `asks`).
- The gateway, exposures, MCP and tool adapters, discovery, the hook endpoint and registration (iteration 15).
- Authorization between processes (per-adapter NATS users).

## Built in slice 47

Forge's part [7c](../../forge/documentation/plans/07c-Connections.md): connections, sessions and event sources. Packages: `features/connectors/connections` (the connections themselves, Foundation's side of the bus and of event sources), additions to `features/connectors` (`State.go`: subjects, buckets, the ledger, locks, the event stream) and `connectors/adapter` (`sources.go`: leases and `Watch`), the `connection` field type in `records`, the `connection.form` primitive, and in the live core the commands' `Seal` and `NoTool` and the trigger events' `Stream` and `Accept`.

### Connections

A connection is one configured use of a connector type, a **definition** on the `def` stream (target type `connection`, projection `connectors.connection`, keyed `connection.<id>`): `{id, name, type, owner (tenant or user:<id>), settings, secrets (field → version), status (ok, error, disabled), lastError, checkedAt, options {pollInterval, since, paused}, createdAt, createdBy, updatedAt}`.

- **Commands**, every one an op of the operation log with its inverse: `connection.create {type, name, owner? (me, the default, or tenant), settings?, secrets?, options?}` (its inverse a delete), `connection.update {settings? (a merge, null removes), secrets? (a value replaces, "" keeps, null clears), options?}` (clears an error status; its inverse restores each changed setting, secret reference and the options), `connection.rename`, `connection.disable`/`connection.enable` (each the other's inverse), `connection.delete` (irreversible: its secrets are forgotten) and the internal `connection.setStatus {status, error}`. Each create and update is checked whole against the type's settings schema (`connections.FieldsOf`: Configuration's schema language, the top-level `secret` fields being the secrets).
- **Secrets are sealed by the bus.** `live.Command.Seal` runs after the authz and argument checks and before the command is decided or logged: each secret value goes to the `config.SecretStore` under a new version (`SecretRef{tenant/<t>, connections/<id>, <field>/<version>}`), its placeholder into KV `cx_secrets_<tenant>` at `<connection>.<field>.<version>`, and the arguments keep `{"$ref": <version>}`. So events, ops, `forge.apply` batches and undo carry only references; old versions stay (an undo restores one) until a delete purges them. Seal checks `manage` first, so a refused command stores nothing; in a dry run it stores nothing. `connection.create` and `connection.update` have no tool (`live.Command.NoTool`): a tool call records its arguments.
- **Access**: the resource type `connection` (`read`, `use`, `manage`; `Owned`, fenced by `user`). A user's own connection is at `tenant:<t>/user:<u>/connection:<id>`: used and managed by that user alone (`connections.CheckAccess`; an administrator sees that it exists). A tenant connection is at `tenant:<t>/connection:<id>`, made by holders of `connection.manage` on the tenant (class `admin`: *Owner*, *Administrator*), used with `use` (class `execute`). An app role may be granted either on one connection.
- **Queries**: `connections.list {type?, contract?}` and `connections.describe {id}` give `connections.View` (each secret only `{set: true}`), checked for `read`. `connection.test {id}` (a call ending with one `result` event, and a tool) sends the type's `check` operation as the caller and records the status.
- **The `connection` field type** holds a connection's id, optionally only of types implementing `contract` (id@version); reading the record never reads a secret. An app's export leaves its values out of the seed, and an import clears them for the tenant to fill.

### Calls on a connection

- The dispatcher's `Connections` is `connections.Connections`: `TypeOf` checks `use` as the run's principal (agents included) and refuses a disabled or errored connection (`unavailable`, not retryable; `connection.test` may call an errored one). Adapters trust this check.
- **The ledger**: for an operation with a `key`, the dispatcher reads KV `cx_ledger_<tenant>` (`<connection>.<operation>.<sha256 of the key>`, TTL 30 days) before sending; a hit returns the stored result with no request. The adapter writes the entry through `Context.Accepted(result)` the moment the outside service accepts the request, and the runtime writes it after a successful call if the type did not.
- **Status**: `Dispatcher.Failed` is told of each typed error a reply carries. `auth` commits the connection's `error` status (which stops its event source until it is updated); `unavailable` and `rateLimited` are kept as its last error. Only a change is committed.
- **Sessions** live in the adapter instance that receives the call: one per connection, opened on demand with the settings from `cx.resolve` (or `Options.Resolve`), closed when idle (`Options.IdleTimeout`, 5 minutes) or on `cx.changed`.
- **Exclusive types** (`Type.Exclusive`, POP3 in 7h) run each call under the connection's lock in KV `cx_locks_<tenant>` (created only if absent, released by revision, gone after 2 minutes if its holder dies), on a session opened for it and closed after. A waiter wakes on the lock's release (a KV watch), else retries every 250 ms; one that cannot take it before its deadline is `unavailable`.

### Event sources

- **Which sources run.** KV `cx_sources`, keyed `<tenant>.<connection>`: `{tenant, connection, type, contracts, pollInterval, since, revision, at}`, for each connection that is ok, not paused, of a type implementing a contract an installed trigger's *On connector event* names (unfiltered, or filtered to it), and that the trigger's app may use. `connections.Reconcile` computes it from the installs and connections projections and writes only what differs; every server process runs it (`connections.Start`) from a follower on connection and install events (`connectors.sources`, one durable consumer shared by all) and once a minute. The same follower purges a deleted connection's secrets. `revision` hashes the settings, secret references and options, so a change restarts the source.
- **Who runs it.** Each adapter instance serving a type with watched events watches `cx_sources` and contends for a lease in KV `cx_leases` (`<tenant>.<connection>`, created only if absent, refreshed with the revision it holds every `LeaseEvery`, TTL 15 s): `{adapter, instance, host, since, lastError, lastErrorAt}`. It takes a new lease only while it holds fewer than its share, sources of the type ÷ its live instances in `cx_adapters`, plus one; running sources are never handed over. A holder that has not refreshed for TTL − `LeaseEvery` stops its `Watch`, so two never run; a stopped instance deletes its leases at once.
- **Running it.** The holder opens its own session (under the lock for an exclusive type) as the tenant's service principal and calls `Watch(ctx, cursor, emit)` with the cursor from KV `cx_cursors` (`{cursor, at, lastEvent}`) and `Context.Since` from the connection's window. A push connector's `Watch` runs until cancelled; a poll connector's returns and is called again after the poll interval (1 minute by default). An error backs off from 1 s to the interval; an `auth` error is sent on `cx.status` for Foundation to commit.
- **Once and in order.** Each emitted event is published to `CX_<tenant>` with `<connection>:<event>:<id>` as its message id (a hash of its payload when it has no id), then its cursor is stored. A new holder resumes from the stored cursor; an event emitted again after a crash between the two is dropped by JetStream within the stream's duplicate window (2 minutes, `connectors.EventDuplicates`). The stream keeps events 30 days.
- **Triggers.** *On connector event* is a trigger event with its own stream (`livenodes.TriggerEvent.Stream`, CX_<tenant>): a trigger's durable consumer filters `cx.event.<tenant>.<connection or *>.<contract>.<event>`. A trigger filtered to a connection its app may not use is refused, with the reason in the runner's warning, and does not run; an unfiltered one skips (`TriggerEvent.Accept`) the events of connections its app may not use. Its outputs are the payload's fields, `connection` and `event {contract, name, id, payload, at}`. A message in `CX_<tenant>` is a `live.Event` (type the event, target `{cx, <contract token>, <connection>}`, data the payload, `commandId` the event's id), so the job runner's trigger code serves it unchanged.
- The live `connectors.sources` query (`job.manage`) lists them with their holder, cursor age, last event and last error, and whether an adapter serves the type; Administration › Jobs shows it under *Event sources*.

### Protocol additions

| Subject or bucket | Kind | What |
|---|---|---|
| `cx.resolve.<tenant>.<connection>` | request/reply, queue `cx.foundation` | An adapter asks for a connection's settings: body `{type}`, reply `{type, settings}` with the secrets in place, or `{error}`. Answered only for a connection of the type asked. Until authorization between processes is built, this channel is as trusted as the cluster. |
| `cx.status.<tenant>.<connection>` | request/reply, queue `cx.foundation` | An event source's error: body `{error {code, message}}`; an `auth` one marks the connection errored |
| `cx.event.<tenant>.<connection>.<contract>.<event>` | JetStream `CX_<tenant>` | An event as a `live.Event`; header `Cx-Contract: id@version`; `Nats-Msg-Id` `<connection>:<event>:<id>` |
| `cx_sources` | KV | `<tenant>.<connection>` → the source (Foundation writes) |
| `cx_leases` | KV, TTL 15 s | `<tenant>.<connection>` → its holder (adapters write) |
| `cx_cursors` | KV | `<tenant>.<connection>` → where it resumes (adapters write) |
| `cx_ledger_<tenant>` | KV, TTL 30 days | `<connection>.<operation>.<key hash>` → `{result, at, callId}` |
| `cx_locks_<tenant>` | KV, TTL 2 min | `<connection>` → the holder of an exclusive connection |
| `cx_secrets_<tenant>` | KV | `<connection>.<field>.<version>` → the SecretStore's placeholder, never a value |

Every connector type that needs a connection should implement a contract with a `check` operation (a sign-in and a no-op): `connection.test` sends it, and is `notSupported` without one. The test contract has one.

### The Connection form

The primitive `connection.form` (`frontend/common/src/ui/renderers/connectors/ConnectionForm.tsx`; props `connection`, `contract`, `owner`, `saveLabel`, `disabled`; events `saved {id}`, `tested {ok, error}`) picks a connector type implementing the contract (an offline one marked), draws its plain settings with Configuration's `SchemaField`, its secrets as write-only password fields (a stored one is *Set: leave empty to keep it*), and has *Test* and *Save*, then goes on editing a connection it made. It sends `connection.create`/`connection.update` with `{secret: true}`, which records the frame in DevTools' Live tab with each secret as `•••`. A `connection` field's editor picks among `connections.list {contract}`. Forge's starter library wraps the primitive as the *Connection form* prefab.

### Decisions

Answered before building (the user, 2026-11-11), each as proposed: question 1 (connections are definitions on the `def` stream), 2 (tenant connections made by holders of `connection.manage`; an app role may be granted it on one connection), 3 (an instance takes leases up to its share plus one, no rebalancing).

Decided while building:

1. **Secrets sealed by the bus** (`Command.Seal`), versioned, so no event, op or undo ever carries a value and a refused or failed command leaves at most an unreferenced version.
2. **Sources, leases and cursors in single buckets keyed `<tenant>.<connection>`**, not one per tenant as the plan had: an adapter instance watches every tenant's sources, and a KV watch cannot span buckets. The ledger, locks and secrets stay per tenant.
3. **A user's own connection is theirs alone for `use` and `manage`**, even for an administrator, who sees that it exists (the iteration's question 7 proposal for mail accounts).
4. **"Refused at install"** for a trigger on a connection its app may not use means the trigger does not run and the runner warns why, as for *On record changed* on a type the app may not read; `app.install` itself is not refused.
5. **`connection.update` clears an error status**, so fixing a password restarts the source; `connection.enable` clears one too.
6. **Status commits only on a change**, so a flaky service does not fill the `def` stream; `checkedAt` is set by each status commit and by `connection.test`.
7. **Connector events are `live.Event`s** in `CX_<tenant>`, so the job runner's trigger consumers needed only a stream opener and an accept hook (`TriggerEvent.Stream`, `Accept`), and job ids stay `<app>-<trigger>-<seq>`.
8. **A source's session is its own**, apart from the call pool, since a push `Watch` holds its connection open.

### Decisions to review after seeing it

- ★ The Connection form for a type with many settings (seen with the test type's six fields: each `SchemaField` row keeps room for its source badge, which a connection never has).
- Whether an event re-emitted more than 2 minutes after its first publish (a cursor lost for longer) should be deduplicated further than the stream's window.

### Verification

`go test ./features/connectors/connections` (all seven three times in a row), on the live core, drafts, installs, a job runner and adapters on their own NATS connections:

- **secrets:** a connection made with a secret, echoed through, updated with an empty secret (kept) and a new one (replaced): the secret is in no command result, event or op on the `def` stream, query result (`connections.list`/`describe` say `{set: true}`), log line at debug, `connection.test` tool result, or message on the bus but the replies to `cx.resolve`; a delete purges every version from the store;
- **use:** with a policy installed, a user's own connection is refused to another *Editor* and to an *Administrator*; an *Editor* cannot make a tenant connection, an *Administrator* can and an *Editor* uses it; *On connector event* filtered to another's connection is refused for the app, an unfiltered one gives `cx.event.1.*.test_echo.finished` and skips that connection's events;
- **event source:** an installed trigger on `test.echo@1 finished`, a tenant connection with three events whose third stores no cursor, polled every second: the trigger runs three times though event 3 is emitted on every pass; the holder (adapter *a*) shown by `connectors.sources` is stopped and an adapter in another connection takes the lease and resumes from the cursor, still three; raised to five events, each of the five reaches the trigger once; paused, the source is gone;
- **exclusive:** ten concurrent calls on a `test.exclusive` connection served by two instances: all succeed, never two sessions open at once;
- **ledger:** `start {id: k1}` is served once; the same call from another process's dispatcher returns `j-k1` with no message on `cx.call.>`; another key is served;
- **auth:** a refused password on a call marks the connection `error`, after which calls are refused (not retryable) and an installed trigger lists no source; an update clears it, the source's own `auth` failure (sent on `cx.status`) marks it again and removes the source; another update restores it; `connection.test` records ok;
- **undo:** each of six commands (settings, a secret, options, rename, disable, enable) undone by its inverse restores the connection exactly, and done again gives the same state; delete is irreversible.

`go test ./features/connectors/...`, `./features/jobs`, `./features/live/...`, `./features/apps/...` and `./features/records/...` pass (records/search builds only with `build.sh`); `apps/tools`' `TestMetadata` checks the connection commands, queries, tool and the `connection.form` primitive.

In Forge, in headless Chromium, with a scratch NATS server running the test adapter and `serve --nats-url` against it: the *Connection form* prefab's Canvas (Interact) listed the three test types, drew *Test secure echo*'s settings and its *Password* field, saved a new connection (`connections.list`: `"secrets":{"password":{"set":true}}`), then showed it with its status, the password as *Set: leave empty to keep it*, and *Test* answered *The connection works* through the external adapter, settings resolved over `cx.resolve` from the cipher store. The secret was in neither the server's log nor any file under the data directories.

### Not built

- The *Connection* picker in *Call connector*'s details panel, and a *Connections* administration view (apps embed the form).
- Pushed events at a hook URL, registration with the service and backfill after a gap (15f).
- Several sessions per connection in one instance (one session serves concurrent calls).
- Authorization between processes, which `cx.resolve` waits for.

## Built in slice 48

Forge's part [7d](../../forge/documentation/plans/07d-ScriptedConnectors.md): scripted connectors, connector types an app declares in its manifest and implements in blueprints. Packages: `features/connectors/scripted` (the `connector.op` kind, its nodes, *Emit connector event* and the scripted adapter), additions to `apps` (the manifest's `connectors`, their check, the version's pinned graphs), `apps/manifests` (the keyed list, publishing their graphs, lint), `apps/installs` (`app.installs` lists them), `connectors` (`EventID`, `PublishEvent`, the registry's `scripted` source) and `connectors/connections` (disabling the connections of removed types).

### The manifest's `connectors`

A keyed list by `name` (`app.addConnector`, `app.updateConnector`, `app.removeConnector`, each an op with its inverse): `{name, title?, description, icon?, contracts: [id@version], settings? (Configuration's schema language; a field of type `secret` is sealed into the SecretStore like any connection's), operations: {<operation id>: <graph>}, watch?}`. `operations` merges per key (`null` removes one). Installed, the type is `<appId>.<name>` with source `scripted`, tags `scripted` and the app's id.

`apps.CheckManifest` (with the contracts in `apps.Env.Contract`, `apps.BuiltinContracts(reg)`: the `connector.contract` contributions, the contracts defined in Go):
- the name is letters, digits and `-`, and the type id passes `connectors.ValidID` (an app id with `_` cannot have connectors);
- the type passes `connectors.CheckType`, so the registry never refuses its announcement: a description, known contracts, one version of each, a settings schema that parses;
- every operation named is one of its contracts', answered by a `connector.op` graph owned by the app whose *Operation called* names that operation, and whose every *Return* gives only the result's fields and all of its required ones;
- `watch` names a `connector.op` graph whose entry is the watch entry.

An operation of its contracts with no graph is a **warning** of `forge.check` (`apps.ConnectorWarnings`): calling it answers `notSupported`. `app.publish` copies the operation and watch graphs into the version (`Manifest.ConnectorGraphs`, `Version.ConnectorGraph`, `Version.WatchGraph`), so an export carries them (question 2), and an import under another id renames `op-<app>-…` graphs as it does `cmd-` and `qry-`.

### The `connector.op` graph kind

- **Operation called** (`connector.operation`, config `{contract, operation}` or `{watch: true}`): outputs one pin per top-level field of the operation's args (from `Normalize`, as *Call connector*'s; a field named `settings`, `connection`, `caller`, `cursor` or `args` is read from `args`), `args` whole, `settings` (secrets included: the graph runs on the server), `connection` and `caller` (`{kind, tenant, user?, app?}`). The watch entry gives `cursor` in place of the args.
- **Return** (`flow.return`) gives the result's fields; **Return error** (`connector.returnError`, `{code, message, retryable?, retryAfter? (seconds)}`) ends the run with a typed error, retryable by its code's default unless the input says otherwise.
- It runs as **the app's principal** (`features.AppPrincipal`), so it reads and writes the app's records by commands; *Enqueue job*, *Cancel job* and *Upload file* allow the kind too (`livenodes.KindConnectorOp`).

### Raising events

**Emit connector event** (`connector.emit`, config `{contract, event}`, inputs `connection`, `payload`; output `id`), in `server.action`, `app.trigger` and `connector.op` graphs: only in a graph owned by an installed app that declares a type implementing the contract, for a connection of that type. The payload is checked against the event's schema; its id is the id fields' values joined by `|` (`connectors.EventID`, else a hash); it is published by `connectors.PublishEvent`, the code a builtin `Watch`'s events go through, so the message id `<connection>:<event>:<id>` keeps an event raised twice once. In a watch graph's run its events are held and published by the event source after the run, the returned cursor stored with the last (or alone when there are none).

### The scripted adapter

`scripted.Serve(ctx, nc, deps, tenants, Options)` runs in every process (`serve.go`, beside `connections.Start`). Per tenant it watches `apps.install` and, when the scripted types the installs declare change (`scripted.Types`), starts an `adapter.Serve` for that tenant alone (`Tenants: [t]`) over those types and their contracts, then stops the old one a second later, so no call meets *no responders*. Being an ordinary adapter, it **announces** the types in `cx_adapters` (which is how they reach the registry: `connectors.types` lists them with source `scripted` and they leave it at uninstall, the registry's subscribers told of both), answers calls in their queue groups (`cx.<type>`, shared by every process), resolves settings on `cx.resolve`, writes the ledger after a keyed call, and contends for their event sources' leases. A session reads the install at each call, so an upgrade's graphs answer the next call; it runs the operation's pinned graph through the engine (`TraceFailures`), `Trigger` `connector:<appId>.<name>`, and replies with its outputs or its typed error; a failed run is `unavailable` naming the run. A type without a watch graph has a source that only waits: its events come from the app's triggers.

**Uninstalling** an app, or an upgrade dropping one of its connectors, disables the connections of the removed types (`connection.disable` as the tenant's service principal, from the connections follower on install events); enabling one again is the user's choice once the type is back.

### Decisions

Answered before building (the user, 2026-11-11), each as proposed: question 1 (no scripted contracts here; 11-Zapier adds them) and 2 (the types travel with the manifest in an export).

Decided while building:

1. **Installed types reach the registry by announcement**, not by the registry reading installs: the scripted adapter is an adapter like any other, so online state, subscriptions, queue groups, the ledger and event sources needed nothing new, and `connectors` still does not import `apps`.
2. **One adapter per tenant**, since two tenants may install different versions of one app with one type id; announcements are per instance.
3. **The graphs run as the app's principal**, the caller given as `caller` (the plan said both: "as the call's principal" and "as the app's install principal"; the app's own records need the second).
4. **Only contracts defined in Go** (`connector.contract` contributions) may be implemented, not announced ones: an app's check must not depend on an external adapter being online.
5. **A secret setting is a field of type `secret`**, Configuration's schema language, as the builtin types' are (the plan wrote `"secret": true`).
6. **One graph per operation id**: an operation two of the connector's contracts declare (`check`) is answered by the same graph.
7. **Removed types' connections are disabled** by the connections follower that already follows install events, reading the old version for an upgrade.

### Decisions to review after seeing it

- ★ The Applications editor's Connectors tab (Forge [ApplicationsEditor.md](../../forge/documentation/ApplicationsEditor.md#built-in-part-7d)), once there are contracts defined in Go (7e).
- Whether a failed run should be `unavailable` (retried by a job) or not retryable: a bug in a graph is retried until the job dies.

### Verification

`go test ./features/connectors/...` (three times in a row), `./features/apps/...`, `./features/jobs/...`, `./features/live/...`, `./features/cognition/...`. In `connectors/connections`' `scripted_test.go`, the Stub app implements `test.echo@1` in blueprints as `stub.echo`, served by two scripted adapters on their own NATS connections beside the builtin `test.secure`:

- **same answers:** `echo` with and without `repeat` and `options.upper` gives the builtin's text and length through a connection to each type; `fail` with `notFound`, `conflict`, `rateLimited` and `invalid` gives the same code, message and retryability; `connectors.types` lists `stub.echo` as `scripted` with two instances;
- **ledger:** `start {id: k1}` three times runs its graph once; **check:** `connection.test` runs the check graph;
- **left out:** with only `echo` and `check`, `forge.check` warns of `operations.fail` and calling `fail` is `notSupported`, not retryable;
- **events:** an action of the Stub app emitting `finished` starts the Watcher app's trigger; the same event id twice starts it once; emitting on a `test.secure` connection is refused;
- **install:** the type is absent before install, listed after, gone after uninstall, the registry's subscribers told each time; its connection is disabled and a call on it refused;
- **refused:** `app.addConnector` naming a missing graph, a graph answering another operation, a graph of another kind or an operation no contract has is refused at `connectors[echo].operations.<op>` with a fix; a Return without the required `text` is refused.

In Chromium against Forge's server, the Applications editor's Connectors tab renders beside Commands and Queries with no console errors; the server defines no Go contracts until 7e, so the tab says so and a connector cannot be made from it yet.
