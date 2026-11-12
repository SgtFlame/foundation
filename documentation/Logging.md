# Logging: structured logs and errors, VictoriaLogs, and the Log Explorer

## Context
- **The backend** logs with plain `log/slog` (a few `slog.Warn` calls) and returns errors as strings in `Common.Message`. Logs are not stored anywhere, and they cannot be searched.
- **The frontend** already logs in a structured way: `src/logging/` is modelled on `samber/oops`, and its records go to DevTools' in-memory Log tab.

Logging makes the backend match the frontend, and stores and searches the logs:
1. **One structured model** for both errors and logs on the server, built on `samber/oops`. An error and a log line carry the same context (domain, tags, code, trace, attributes…) with the same field names as the frontend's `LogRecord`.
2. **VictoriaLogs** stores the logs. The `logs` feature is a thin wrapper over its HTTP API, and the only setting is where VictoriaLogs is (host and port).
3. **The Log Explorer**: a viewer with a structured query builder and a raw LogsQL editor in Monaco.

VictoriaLogs' query language is called **LogsQL**.

Built: the builder, the request middleware and `Fail` (slice 1); storage in VictoriaLogs, `logsservice`, browser forwarding and the Log Explorer (slice 3); full logging for one user (slice 10). Saved queries, `Stats`/`StatsRange` charts, the error toast's link, setting full logging from the Explorer, Cognition run summaries and alerts are in [plans/Logging.md](plans/Logging.md).

## 1. Errors and logs: one builder
### Errors: `samber/oops`
- **How errors are made**: every error a service creates or wraps is an oops error:
  ```go
  return oops.In("kb").Tags("volume").Code("volume_not_found").
      With("volumeId", args.VolumeID).Public("That volume does not exist").
      Errorf("no volume marker in %s", rel)
  ```
  - `oops` captures the stack trace and keeps the context through `Wrap`.
  - `Public` holds the message that is safe to show users. `Errorf` holds the detail for the logs.
- **Request context**: middleware puts an oops builder in the request context, through `oops.WithBuilder`, with the trace ID, tenant and user. The trace ID is the request's `X-Trace-Id` header, which the browser sets, or a new one when the header is missing. Builders made from that context (`oops.FromContext(ctx)`) inherit them, so every error and log in a request is correlated.
- **Service responses**: a helper turns an error into `Common`:
  - **`features.Fail(&resp.Common, status, err)`** sets:
    - `Code` to the HTTP status;
    - `Message` to the oops `Public()` text, or a generic message for 5xx;
    - a new **`ErrorCode`**, the oops code (`volume_not_found`), so the frontend can match on it and show it in its own `LogRecord.code`;
    - a new **`Trace`**, the trace ID, for the server's own use. **It is never serialized into the response**: the browser already knows the trace, because it sent it.
  - **Logging**: `Fail` logs the full error, 5xx at `error` and 4xx at `debug`. Handlers and callers never log the same failure again.
  - The DTO `Common` gains `errorCode` only. A frontend error keeps the trace ID it sent with the request, so a user's error report still leads straight to the server logs.

### Logs: the same builder
- **The `features/logging` package** wraps an `oops.OopsErrorBuilder`. One builder can either log or make an error, exactly like the frontend's `log`:
  ```go
  log := logging.In("index").Tags("reindex").With("files", n)
  log.Info(ctx, "reindex started")                      // a log record
  return log.Code("reindex_failed").Wrap(err)            // an oops error with the same context
  log.Error(ctx, err, "cannot index %s", file)           // logs err with its oops context flattened in
  ```
- **Under the hood it is `slog`**. Records go through a `slog.Handler`, so `slog.Warn` calls in libraries still work, and they get the request's trace from `ctx`.
- **Field names**: the same names on the server and in the frontend `LogRecord`:

| Field | From |
|---|---|
| `_time`, `level`, `_msg` | the record. `_msg` and `_time` are VictoriaLogs' reserved names. |
| `domain`, `tags`, `code` | `In`, `Tags`, `Code` |
| `trace`, `span` | the request middleware, `Trace`, `Span` |
| `hint`, `public` | `Hint`, `Public` |
| `user.id`, `tenant.id` | the request principal ([MultiTenant.md](MultiTenant.md)) |
| `attr.<key>` | `With(key, value)`, flattened with a prefix so attributes never clash with the fields above |
| `error`, `error.code`, `error.domain`, `error.stack`, `error.cause` | a logged error's oops context |
| `app`, `instance`, `source` | the stream fields: `app=foundation`, the host or instance name, and `server` or `browser` |

- **Request logs**: gin's default logger is replaced by one `info` record per request, with the method, route, status, duration, bytes, trace and user. Gin's debug route dump stays on stderr only.
- **Frontend logs on the server**: the browser `log` forwards records of `warn` and above to `POST /logs/ingest` in batches, with `source=browser`. The axios client generates a trace ID for every request and sends it as `X-Trace-Id`, so a browser record and the server records for that request share one trace.
- **Full logging for one user**: for a hard-to-reproduce bug, an application administrator can turn on full logging for one user.
  - A `UserLogLevel` record holds the user, the level (`debug`), who set it, and an expiry (default 24 hours).
  - While it is active, that user's browser forwards everything from `debug` up, and the server stores that user's `debug` records too.
  - The browser learns its level when the session starts and every 5 minutes (see [Built in slice 10](#built-in-slice-10)). The server keeps the active overrides in memory.
  - It is set from the Users editor, and needs `logs.setUserLevel` ([RBAC.md](RBAC.md)).
- **No personal data**: records identify users only by `user.id`, with no emails or names. Prompts and replies are never logged, as in [Usage.md](Usage.md).

## 2. Storage: VictoriaLogs
VictoriaLogs runs as a separate process (a single binary, default port **9428**). Foundation never links to it; it only uses the HTTP API.

### Writing: `logging.VictoriaHandler`
- **What it is**: a `slog.Handler` that batches records and posts them to **`/insert/jsonline`** as JSON lines, with:
  - `_stream_fields=app,instance,source,tenant.id`;
  - `_msg_field=_msg`;
  - `_time_field=_time`.
- **It never blocks a request**: the queue is bounded, and when it is full the oldest records are dropped and counted. A flush happens every second or at 1000 records. Failed posts retry with backoff.
- **Fan-out**: records always go to stderr as well (text in dev, JSON otherwise). VictoriaLogs being down never loses the local output.
- **Without VictoriaLogs configured**, logs go to stderr only, and the Log Explorer says that log storage is not configured.

### Reading: the `logs` feature
- **The service**: `features/logs/services/logsservice` is a thin wrapper over VictoriaLogs' query API. Each operation maps to one endpoint:

| Operation | VictoriaLogs endpoint | Used for |
|---|---|---|
| `Query{Query, Start, End, Limit}` | `/select/logsql/query` | result rows (the JSON-lines response is streamed through) |
| `Hits{Query, Start, End, Step, Field}` | `/select/logsql/hits` | the histogram over time, optionally split by a field such as `level` |
| `FieldNames{Query, Start, End}` | `/select/logsql/field_names` | the builder's and editor's field autocomplete |
| `FieldValues{Query, Field, Limit}` | `/select/logsql/field_values` | value autocomplete and facet counts |
| `Streams{Query}` | `/select/logsql/streams` | the stream picker |
| `Tail{Query}` | `/select/logsql/tail` | live tail, relayed as SSE |
| `Ingest{Records}` | `/insert/jsonline` | frontend log forwarding |

- **What the wrapper adds**:
  1. **Scoping**: every query is wrapped as `{tenant.id="<active tenant>"} AND (<user query>)` unless the principal may read all logs. Tenants are isolated by this stream field, not by VictoriaLogs' own `AccountID` tenancy, whose 32-bit IDs would need a mapping from tenant TSIDs.
  2. **Access checks**: `logs.read` and `logs.readAll` in [RBAC.md](RBAC.md).
  3. **Limits**: a default and maximum `limit`, and a maximum time range.
  4. **Errors**: VictoriaLogs' LogsQL parse errors become 400 responses with the error position, so the editor can underline it.
- **REST and client**: under `/api/v1/logs`, with `src/services/Logs.ts`. `Query` and `Tail` stream with `fetch`, like `Chat.stream`, and are logged, not captured.

### Configuration
- **The section `logging`** ([Configuration.md](Configuration.md)), app scope:
  - `victoriaLogs.host` and `victoriaLogs.port` (default `localhost`, `9428`);
  - optional credentials, as a `secret`, for a VictoriaLogs instance behind an auth proxy;
  - the minimum level stored (`info`) and the minimum level forwarded from browsers (`warn`).
- **Before configuration exists**: the flag `--vlogs-url`, or `$FOUNDATION_VLOGS_URL`, is the fallback.
- **Changes apply live**: a `config.Changed` event swaps the handler's target without a restart.
- **Retention** is VictoriaLogs' own `-retentionPeriod` flag, not a Foundation setting.

### Running it in development
`backend/scripts/victorialogs.sh` downloads a pinned VictoriaLogs release into `backend/third_party/victorialogs/` (once), and runs it on `:9428` with data in `<data-dir>/victorialogs`. `serve.sh` does not start it.

## 3. The Log Explorer
An editor, `LogsObject` (`src/editors/logs/`), opened from the **Administration** sidebar section. It is separate from DevTools' Log tab, which stays the browser's own in-memory buffer, but both use the same row renderer.

### Query bar
- **Time range**: relative presets (last 15 minutes, 1 hour, 24 hours, 7 days) or an absolute range. It is sent as `start`/`end`, never put into the query text.
- **A Builder / LogsQL toggle**, over one query.
  - **Builder → LogsQL** always works: the builder generates the text.
  - **LogsQL → Builder** works when the query uses only what the builder can show. Otherwise the builder shows "edited as LogsQL" and offers to reset.

### Structured builder
- **Streams**: chips for the stream fields (`app`, `source`, `tenant.id`), with values from `Streams`.
- **Level**: a multi-select (`debug` / `info` / `warn` / `error`).
- **Words**: free text, as a LogsQL word or phrase filter on `_msg`.
- **Field filters**: rows of field, operator and value, grouped in **AND/OR** groups with a **NOT** toggle per row.
  - Fields come from `FieldNames`, and values from `FieldValues` with counts.
  - Operators:

    | Operator | LogsQL |
    |---|---|
    | equals | `field:="v"` |
    | contains a word or phrase | `field:"v"` |
    | starts with | `field:v*` |
    | matches a regex | `field:~"re"` |
    | is empty / exists | `field:""` / `field:*` |
    | greater or less than | `field:>n`, `field:<n` |
    | in a range | `field:range[a, b]` |
    | one of | `field:in(a, b)` |
- **Pipes** (an expandable section): `fields`, `sort by`, `limit`, `uniq by` and `stats by (…) count()`. More pipes can be written in LogsQL mode.
- **Quick filters**: clicking any value in the results adds a filter for it. *Show this trace* filters by `trace`.

### LogsQL editor
- **Monaco**, already bundled locally for the Markdown Editor's Source view, with a **LogsQL language** registered through a Monarch tokenizer: filters, operators, pipes, strings and comments.
- **Completion**:
  - field names, from `FieldNames` over the current time range;
  - field values after `field:`, from `FieldValues`;
  - pipe names after `|`;
  - stream fields inside `{…}`.
- **Errors**: parse errors from the server are shown as markers at their position. Ctrl+Enter runs the query.

### Results
- **A hits histogram** above the rows, stacked by `level`. Dragging across it narrows the time range.
- **A virtualized table**:
  - the columns are `_time`, `level`, `domain` and `_msg`, plus any fields the user picks;
  - expanding a row shows every field as a tree, a logged error's stack and causes, and links to the trace and to *surrounding records* (the same stream, ±N records around it);
  - `stats` queries render as a table.
- **Live tail**: a toggle that streams new records through `Tail`, with pause and auto-scroll.
- **Deep links**: the URL holds the query and time range.

## Built in slice 1
- **`features/logging`**: `Builder` keeps its own fields and builds an oops builder only when it makes an error, because an `oops.OopsErrorBuilder` cannot be read back without making an error and capturing a stack. Its errors' stack traces start at the caller (`CallerSkip(1)`).
  - `logging.WithContext(ctx, b)` stores a builder for the request, together with the matching oops builder, so `oops.FromContext(ctx)` agrees. `Info(ctx, …)` and the other logging methods layer the builder over the one in `ctx`. `logging.FromContext(ctx)` is for making errors.
  - `logging.Setup` makes slog's default handler a text handler (`serve --log-format text`, the default) or JSON lines with VictoriaLogs' names (`json`), from `--log-level` (`info` by default). It turns off oops' automatic trace IDs, since every request has one.
  - Fields are flat dotted keys (`user.id`, `attr.files`, `error.stack`). Each log call checks the level before building its record.
- **The request context lives in `ServiceArgs`**. `ServiceArgs` holds `Ctx` (the request's context: cancellation, trace and log context) and `Principal`. The `Ctx` fields that `chatservice`, `embedservice`, `indexservice` and `collabservice` had in their own args moved there, and a service calling another passes its `ServiceArgs` on, so the trace and the principal follow the work. Work no request started runs with `features.BackgroundArgs(ctx)`.
- **`Fail` is a method**: `args.Fail(status, err) *features.ServiceResponse`, in place of `features.Fail(&resp.Common, status, err)`. It needs the context for the trace and for logging, and returning a pointer matches the existing `Common: *fail(…)` idiom and helpers such as `volumeservice.Resolve` that return `*ServiceResponse`. `features.OK()` is the success response.
- **Messages**: `Message` is the oops `Public()` text when there is one; otherwise a 4xx shows the error's own text, and a 5xx only its status text. 4xx errors therefore name nothing private, as before (folders relative to the root). Server paths in 5xx errors no longer reach the browser.
- **Provider errors**: `providers.Wrap(err)` keeps showing users the provider's message, as `Public`, with the code `provider_failed` or `provider_not_configured`.
- **`features/rest`**: `rest.Middleware()` replaces gin's logger (`gin.New()` with the middleware and `gin.Recovery()`). It takes `X-Trace-Id` when it is 8 to 64 characters of `[A-Za-z0-9_-]` and makes one otherwise, echoes it in the response, and logs one record per request: `info`, or `warn` for a 5xx, since `Fail` has logged the error itself. `rest.Args(c)` is the request's `ServiceArgs`, and `rest.Bind(c, &req)` decodes the body or answers 400 with code `bad_request`. Response DTOs embed `rest.Status{Message, ErrorCode}`, the error event of the chat stream carries `errorCode`, and request DTOs take the arguments: `req.ToArgs(args)`.
- **The browser**: the shared axios client sets `X-Trace-Id` on every request (and `Chat.stream` on its `fetch`), and DevTools' http records carry it as their `trace`. `ServiceError` gains `errorCode` and `trace`, and response DTO interfaces extend `ResponseStatus`. The collab WebSocket cannot set headers, so it gets a server-made trace.

## Built in slice 3
- **Storing: `logging.Victoria`** (`features/logging/Victoria.go`). Its `Handler()` is slog's JSON handler with VictoriaLogs' names and the server's stream fields (`app=foundation`, `instance=<host>`, `source=server`), writing each record as one line into a bounded queue (10,000 records, dropping the oldest and counting them). A worker posts batches of up to 1000 every second to `/insert/jsonline`, and retries a failed post with backoff from 1s to 30s; a failing or recovered target is reported once on stderr, never through slog, so storage cannot log about itself. `Enqueue(line)` queues a line that did not come through slog (forwarded browser records). `SetTarget` and `SetLevel` apply live, `Status()` reports the queue, drops and last error, and `Close` flushes on shutdown.
  - **Fan-out**: `logging.Setup(w, format, level, sinks...)` sends each record to stderr and to every sink enabled for its level. The stderr level (`--log-level`) and the stored level are separate, and the Victoria handler is disabled while there is no target, so nothing is built for it.
  - Posts must be sent as `Content-Type: application/stream+json`: VictoriaLogs reads any other body as a form and silently stores nothing.
- **The `logging` section** (`features/logs/settings`, Settings › System › Logging, app scope): `victoriaLogs.enabled` (off by default, so a fresh install without VictoriaLogs stays quiet), `host` (`localhost`), `port` (9428), `tls`, `username` and `password` (a `secret`) for an instance behind an auth proxy, `storedLevel` (`info`) and `browserLevel` (`warn`). `--vlogs-url` or `$FOUNDATION_VLOGS_URL` (user info becomes the credentials) wins over the section, and is applied before anything else, so startup is stored too. `serve.go` re-reads the section on `config.Changes`.
- **Reading: `logsservice`** (`/api/v1/logs`): `Status` (`GET /status`), `Query`, `Hits`, `FieldNames` (`/fields/names`), `FieldValues` (`/fields/values`), `Streams` (stream picker; with `field`, one stream field's values) and `Tail` (`POST /tail`, Server-Sent Events: `ready`, then a `row` per record), and `Ingest`.
  - **Tenant scoping** uses VictoriaLogs' `extra_filters=tenant.id:="<id>"` rather than wrapping the user's query, so queries with pipes keep working. It applies unless `logs.readAll` allows; with the allow-all `authz.Check` nothing is scoped yet.
  - **`Query` is an ordinary JSON response**, not a stream: rows are bounded by `limit` (500 by default, 5000 at most; one more is asked for to set `limited`). They come newest first, unless the query has its own `sort` pipe. Only `Tail` streams.
  - **Ranges** default to the last hour and may span 31 days (`range_too_long`); they travel as `start`/`end`, never in the query text.
  - **Errors**: a LogsQL parse error is a 400 `bad_query` whose `queryError` holds the message without VictoriaLogs' prefix and the byte `offset` where parsing stopped (from its `context: […]`). Storage that is off fails with 503 `logs_not_configured`; storage that cannot be reached, with 503 or 502 `logs_unavailable`.
  - **`Ingest`** takes up to 200 browser records, drops those below `browserLevel`, and stores the rest with `source=browser`, the server's `app` and `instance`, and the user and tenant from the principal; anything a record says about them is kept only as an attribute. It accepts nothing, without failing, while storage is off.
  - `stats` queries work through `Query` and show as a table.
- **Browser forwarding** (`src/logging/forwarder.ts`, started by `RequireSession` once there is a session): a log sink (`addLogSink`) queues records from the `logging` section's `browserLevel` up (read through `configStore`, so a change applies live), and posts them in batches of up to 50 every 2 seconds through `Logs.ingest`, which is not captured in DevTools' Network tab. Up to 500 wait while the server is unreachable; `pagehide` sends what is left with `sendBeacon`. The forwarder's own failures are logged at debug and never forwarded. A record logged from an error that carries a trace (`log.fromError(serviceError)`) takes that trace, so it lands next to the server's records of the same request.
- **The Log Explorer** (`src/editors/logs/`, Administration › Log Explorer):
  - The **query bar**: a time range (the four presets, or an absolute range), the Builder/LogsQL toggle, Run, and Live tail.
  - The **builder** (`builder.ts`, `BuilderPanel.tsx`): stream fields as chips with values from `Streams`, a level toggle, words, filter rows (field, operator, value, NOT) in AND/OR groups with values and counts from `FieldValues`, and a Pipes section (`stats by (…) count() hits`, `uniq by`, `sort by`, `limit`, `fields`). `toLogsQL` generates the query, shown under the builder; `fromLogsQL` reads back what the builder can show and refuses anything else, in which case the builder says "Edited as LogsQL" and offers Reset. A query that parses opens in the builder, anything else in LogsQL.
  - The **LogsQL editor** (`logsql.ts`, `QueryEditor.tsx`): Monaco with a Monarch tokenizer and completion of pipe names, stats functions, stream fields and values inside `{…}`, field values after `field:`, and field names; a parse error is a marker on the word where parsing stopped; Ctrl+Enter runs. Typing `(` or a space never accepts a suggestion.
  - **Results**: the hits histogram, stacked by level (`Hits` with `field=level`, over the query's filters before its first pipe); dragging across it sets an absolute range and runs again. Records render with the same `LogRow` as DevTools' Log tab (`src/components/logs/`), virtualized with `@tanstack/react-virtual`, plus any fields picked as columns. Expanding a record lists every field with filter and exclude buttons, its error stack, *Show this trace*, and *Surrounding records* (`_stream_id:<id> _time:<t> _msg:="…" | stream_context before 10 after 10`). Clicking a domain, tag, code or column value also adds a filter. Rows without `_time` and `_msg` (stats, uniq) show as a table.
  - **Live tail** prepends new records, keeping up to 2000, with Pause (which buffers: "Resume (n new)") and Auto-scroll. VictoriaLogs refuses to tail some queries, such as `stats`, and the Explorer shows its reason.
  - **Deep links**: every run puts the query and range in the URL (`/workbench?logs=<query>&range=1h`, or `&start=…&end=…`), and opening such a URL opens the Explorer on it. Other views open it through `openLogs(view)` (`editors/logs/openLogs.ts`); DevTools' Log tab does this for a record with a trace (*Show in Log Explorer*). A query and range opened again focus their existing tab.
  - When storage is off the Explorer says how to turn it on, and when storing is failing it shows the last error and the queue.
- **Running it**: `backend/scripts/victorialogs.sh [--data-dir dir] [victoria-logs flags]` downloads VictoriaLogs v1.53.0 (checksum-verified) into `backend/third_party/victorialogs/` once, and runs it on `:9428` with data in `<data-dir>/victorialogs`.
- **Known limits of what was built**:
  - The histogram counts only the filters before the query's first `|`, so it ignores pipes (a `stats` query has no histogram), and a `|` inside a quoted string cuts the query short for the histogram.
  - The builder writes only the five pipes it shows (`stats … count()`, `uniq by`, `sort by`, `limit`, `fields`); other pipes need LogsQL mode, as the plan intends.
  - Live tail cannot follow every query: VictoriaLogs refuses some, such as `stats`, and the Explorer shows its reason.
  - The Explorer's own requests are logged like any other, so an unfiltered query over the last minutes is mostly `http` records of `/api/v1/logs/*`.

## Built in slice 10
**Full logging for one user**
- **The record**: `logs.UserLogLevel` (`UserID` unique, `Level` = `debug`, `SetBy`, `ExpiresAt`) lives in the application database ([MultiTenant.md › Built in slice 10](MultiTenant.md#built-in-slice-10)).
- **`logsservice`**:
  - `SetUserLevel` (`POST /logs/user-level {userId, level: "debug" | "", hours}`: 24 hours by default, at most 168) and `UserLevels` (`POST /logs/user-levels`). Both need `logs.setUserLevel`, which is admin-only until RBAC.
  - `Level` (`GET /logs/level`) is the level the principal's browser forwards from, and `until`.
- **On the server**: the active levels are kept in memory by `logging.Victoria` (`SetUserLevels`, `UserLevel`; loaded at start and on every change, with expired rows purged). Its handler stores a record below the stored level when the request's user has their own level, so that user's server `debug` records are stored. `Ingest` accepts their browser records from that level too.
- **In the browser**: the forwarder asks `Logs.level()` when the session starts and every 5 minutes, and forwards from the lower of that and the `logging` section's `browserLevel`. Forwarding now starts once there is a session (`RequireSession`), not on the login page.
- **The UI** is the *Full logging* switch in Administration › Users, with the expiry on hover.
- **Decisions to review after seeing it**:
  - The browser polls for its level every 5 minutes rather than through the config event stream.
  - The level is always `debug`, for 24 hours.
  - Stderr is unchanged: only stored records follow the user's level.

## Relation to other features
- [DevTools.md](DevTools.md): the browser-side log model these fields mirror. The DevTools Log tab stays local.
- [Configuration.md](Configuration.md): the `logging` section.
- [Usage.md](Usage.md): SQLite is the Dashboard's source of truth for cost; whether usage events also go to VictoriaLogs is open ([plans/Usage.md](plans/Usage.md)).
- [Cognition.md](Cognition.md): run traces stay in SQLite (they hold pin values).
- [RBAC.md](RBAC.md): `logs.read` (your tenant's logs), `logs.readAll` and `logs.setUserLevel`.
- [MultiTenant.md](MultiTenant.md): `tenant.id` is a stream field.

## Built in slice 11
- With roles (see [RBAC.md › Built in slice 11](RBAC.md#built-in-slice-11)), `logs.read` is an `admin`-class action: a tenant's logs are for its *Owner* and *Administrator*, and the Log Explorer command shows only to them. `logs.readAll` and `logs.setUserLevel` are held on the application (*Super Administrator*). `logs.ingest` needs no grant.
