# DevTools tab: axios capture, structured logging, Network + Log viewers

## Context
The frontend (`frontend/common`) talks to the backend through hand-rolled `fetch` in `src/services/KnowledgeBase.ts`, and has no logging beyond (future) `console.log`. We want:
- every request/response between frontend and backend captured into a local, in-memory log;
- a structured logging system modelled on Go's `samber/oops` (fluent builder: `In`/`Tags`/`Code`/`With`/`Trace`/`Hint`/`Public`/`User` …) replacing `console.*`;
- a `DevToolsTab` collapsed by default in the workbench's right flexlayout border, itself a nested flexlayout with a **Network** tab (Chrome-DevTools-like request list + Headers/Payload/Preview/Response/Timing, JSON pretty/raw) and a **Log** tab.

Decisions (confirmed): in-memory ring buffers, cleared on reload; log records mirrored to `console.*` only when `import.meta.env.DEV`.

### Answer to "opt-out, on by default"
Yes. Every service uses one shared axios instance from a factory that installs the capture interceptors by default. Opt-out is available at two levels:
- per service: `createHttpClient({ capture: false })`
- per request: `http.post(url, body, { capture: false })` — added to axios' types via module augmentation (`declare module 'axios' { interface AxiosRequestConfig { capture?: boolean } }`).

Since the interceptors live on the shared instance, a new service gets capture for free; nothing to remember.

### Singleton vs React context
**Singleton** for both stores. The interceptors and service clients aren't React code and can't read context, and there's a single global log anyway. React reads them through small hooks built on `useSyncExternalStore`. Context would only add a provider without giving us anything.

## Files

### New: `src/logging/` (oops-style structured logging)
- `LogRecord.ts`: `LogLevel = 'debug'|'info'|'warn'|'error'`; `LogRecord { id, time, level, message, domain?, tags[], code?, trace?, span?, hint?, public?, user?: {id, attrs}, attrs: Record<string, unknown>, error?: {name, message, stack, cause?} }`.
- `LogStore.ts`: generic `RingBuffer<T>` (capacity, `push`, `clear`, `snapshot()`, `subscribe(listener)`). The snapshot array is replaced on each change, which `useSyncExternalStore` requires. Exports singleton `logStore = new RingBuffer<LogRecord>(1000)`.
- `Logger.ts`: immutable fluent builder that mirrors oops. Each method returns a new builder:
  `log.In('kb').Tags('vault').Code('vault_open_failed').With('vaultId', id).Trace(t).Hint('…').User(id, attrs)`
  and terminal methods `.debug/.info/.warn/.error(msg, ...args)` (printf-style `%s %d %v` formatting like `Errorf`). `.error(msg)` / `.Wrap(err)` / `.Errorf(fmt, …)` build an `OopsError` (extends `Error`, carries the builder context and `cause`) like oops. `.Wrap`/`.Errorf` return the error and **don't** log it, matching oops; `log.fromError(err).error('…')` logs an existing `OopsError`, merging its context. Terminal methods push to `logStore` and mirror to `console[level]` in DEV.
  Export a root `log` singleton plus `OopsError`. `index.ts` re-exports them.

### New: `src/services/http/`
- `NetworkStore.ts`: `NetworkEntry { id, startedAt, endedAt?, durationMs?, method, url, status?, statusText?, requestHeaders, requestBody, responseHeaders?, responseBody?, sizeBytes?, error?, state: 'pending'|'done'|'failed' }`. Exports singleton `networkStore`, a `RingBuffer<NetworkEntry>(1000)` from `logging/LogStore.ts` with an added `update(id, patch)`.
- `captureInterceptors.ts`: `installCapture(instance)`. The request interceptor skips when `config.capture === false`; otherwise it assigns an id (stashed on `config.metadata`), records a pending entry, and logs `log.In('http').Tags(method)…debug('→ %s %s')`. Response/error interceptors complete the entry (status, headers, body, duration, size via JSON length) and log at debug/warn/error depending on the status. A network error has no `response`, so it's recorded as `failed`.
- `client.ts`: `createHttpClient({ baseURL = API_BASE, capture = true } = {})` returns `axios.create(...)` with `installCapture` when `capture` is set, plus an interceptor that maps non-2xx into the existing `ServiceError(status, body.message ?? statusText)`. Exports the default shared `http` instance and `API_BASE`. Move `ServiceError` here and re-export it from `KnowledgeBase.ts` so current imports keep working.

### Modify: `src/services/KnowledgeBase.ts`
Delete the private `fetch`-based `post`. `openVault` becomes `http.post<OpenVaultResponse>('/kb/vaults/open', request).then(r => r.data)`. DTOs and the public API stay the same. The CLAUDE.md rule "components never `fetch` directly" still holds.

### New: `src/tabs/devtools/` (per request, `src/tabs` rather than `src/components`)
- `DevToolsTab.tsx`: hosts its own nested `<Layout>` with a module-level `Model` (one tabset: `network`, `log`) and a local factory.
- `useStore.ts`: `useStore(store)` → `useSyncExternalStore(store.subscribe, store.snapshot)`.
- `network/NetworkViewer.tsx`: Chrome Network subset. A toolbar (filter text, method/status filter, Clear, record on/off toggle, which sets a `networkStore.paused` flag the interceptor checks). An MUI table with Name (path), Method, Status (coloured), Size, Time, sorted newest-last with auto-scroll. Selecting a row opens a split detail pane.
- `network/RequestDetails.tsx`: MUI `Tabs`: **Headers** (General: URL/method/status; Request and Response headers as key/value lists), **Payload** (request body), **Preview** (`JsonView` tree), **Response** (raw text), **Timing** (start/end/duration).
- `json/JsonView.tsx`: a shared viewer with a **Pretty | Raw** toggle and a Copy button. Pretty is a collapsible tree written in-house (no new dependency): objects and arrays expand and collapse, the top 2 levels are open by default, values are colour-coded by type, and long strings are truncated with expand. Raw is `JSON.stringify(value)` (or the original text when it isn't JSON) in a monospace `pre` with wrap toggle. Network Payload/Preview/Response and Log attribute details all use it.
- `log/LogViewer.tsx`: Chrome Console subset. A toolbar with level toggles (Verbose/Info/Warnings/Errors), text filter (message, domain, tags), domain dropdown, and Clear. Rows show time, level icon/colour, `[domain]`, tags as chips, and message. Expanding a row shows code/trace/hint/user/attrs/error stack through `JsonView`.

### Modify: workbench
- `src/pages/workbench/Workbench.tsx`: add a right border to `json.borders`:
  `{ type: 'border', location: 'right', selected: -1, size: 480, children: [{ type: 'tab', id: 'devtools', name: 'DevTools', component: 'devtools', enableClose: false }] }`. `selected: -1` keeps it collapsed by default.
- `src/pages/workbench/ComponentFactory.tsx`: add `case 'devtools': return <DevToolsTab />`. It isn't an `EditableObject`, so `EditorRegistry` doesn't change.

### Deps / docs
- `yarn add axios` (yarn only, per CLAUDE.md).
- CLAUDE.md: update the "Frontend client" section (shared `http` axios instance, capture on by default, `capture: false` opt-out), add `src/tabs/` and `src/logging/` to the frontend layout, and add the rule "use `log` from `src/logging`, never `console.*`".

## Verification
1. `cd frontend/example && yarn build` (tsc + vite) and `yarn lint` pass.
2. Run backend (`serve`) + `yarn dev`. Open `/workbench`: the DevTools tab sits collapsed in the right border. Click it to expand.
3. Trigger `KnowledgeBase.openVault` (temporarily from the Volumes editor or the browser console via a dev-only hook). The Network tab shows the POST with status, timing, headers, and payload/response in Pretty and Raw. A 4xx/5xx shows red and a `warn`/`error` record appears in Log.
4. Stop the backend and retry. The entry shows as `failed` and an error log appears.
5. Per-request opt-out: a call with `{ capture: false }` does not appear.
6. Log viewer: level and text filters work, and expanding a record shows the oops context (domain, tags, attrs, stack). In dev, the same records show up in the browser console.
7. Ring buffer: push >1000 records (loop in the console) and confirm the oldest are dropped and the UI stays responsive.
