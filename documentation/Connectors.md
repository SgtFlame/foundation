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
- **An operation** has `id`, `title`, `description`, `args` and `result` (JSON Schemas, type object), `examples` (args with their result), `effect` (`read`, `write` or `send`), `destructive` (07b question 8), `key` (the arg that makes it idempotent), `errors` (the codes it may return), `callbacks` (events that answer it later, each with a `correlate {result, event}` pair of field paths) and `remote` (a discovered operation's outside name).
- **An event** has `id`, `title`, `description`, `payload` (a JSON Schema, type object), `example`, `idFields` (the payload fields that make it unique, its future `Nats-Msg-Id`), `delivery` (`watch`, `callback` or both) and `subscription` (the service's own name for it).
- **A type** (`Type`) has `id`, `contracts` (refs with versions), `meta`, `settings` (Configuration's schema language; fields of type `secret` go to `SecretStore` with 7c), `noConnection`, `source` (`builtin` or `scripted`), `discovers` (its contract comes from each connection) and `receives` (a callback scheme: `stripe`, `github`, `standard-webhooks`, `hmac-sha256`).
- **Builtin contracts and types** are contributions to the `connector.contract` and `connector.type` points (`BuiltinContract`, `BuiltinType`), registered with `connectors.ContributeBuiltins(reg, source, contracts, types)` from a feature's `Contribute`. `serve.go` declares both points. Neither server has builtin types yet; Forge's mail types come with 7e.
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

`serve.go` (Foundation's and Forge's) makes the registry and dispatcher. It starts an in-process adapter from `connectorImpls()` when that lists implementations; it lists none yet.

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

- Connections: `cx.resolve`, `use` checks, the ledger, connection status on `auth`, the *Connection* picker in the details panel, and the secret test on a real connection (7c). The secret test here uses a resolver given to the adapter.
- Event sources, `CX_<tenant>`, and *On connector event* starting triggers (7c).
- Scripted types in the registry (7d), and the mail contracts and types (7e).
- `bp.check` re-normalizing nodes when an announced contract changes, and the editor redrawing pins at once. A change shows when the node's config is next set or the graph is imported.
- A *Call connector* in a chat tool asking for approval by its operation's effect (graph tools ask by their own `asks`).
- The gateway, exposures, MCP and tool adapters, discovery, the hook endpoint and registration (iteration 15).
- Authorization between processes (per-adapter NATS users).
