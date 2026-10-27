# Usage: token and cost tracking, and the Dashboard

## Context
Every AI call costs money: chat replies, Chronicle's internal calls, and the embeddings the index makes in bulk. We want to see tokens and cost per day, level, model, feature and conversation.

The earlier app stored this in VictoriaLogs. That sidecar does not exist here yet, so usage goes into SQLite and the design keeps a seam for a log sink later.

Prices come from the model catalog (`ai.models` in [Configuration.md](Configuration.md)).

Status: partly built. Slice 2 built recording, with no UI beyond a DevTools tab; see [Built in slice 2](#built-in-slice-2). Slice 4 added the opt-in debug capture; see [Built in slice 4](#built-in-slice-4). Slice 5 built `Summary` and the Dashboard with its *Usage & cost* widget; see [Built in slice 5](#built-in-slice-5).

## Recording: `backend/features/usage`
**`UsageRecord`** (GORM, `db.Model`; the TSID also gives the time):

| Field | Notes |
|---|---|
| `Feature` | `chat`, `chat-stage1`, `memory-update`, `doc-chat`, `embed-index`, `embed-query`, … |
| `Provider`, `Model`, `Level` | `Level` is empty for calls outside the chat levels |
| `ConversationID`, `RoundID`, `FileID` | nullable references; no prompt or reply text is stored |
| `InputTokens`, `CachedTokens`, `OutputTokens`, `ThinkingTokens` | from the provider. For Gemini these are `PromptTokenCount`, `CachedContentTokenCount`, `CandidatesTokenCount` and `ThoughtsTokenCount` |
| `CostMicros`, `Currency` | priced **when the record is written**, from the model's pricing and tier, so later price edits never rewrite history. `int64` micro-units avoid float drift |
| `LatencyMs`, `Status` | `Status` is the `Common.Code` of the call, so failed calls that still billed are visible |
| `Tenant`, `User` | from `ServiceArgs` |

**Where it is recorded**
- A decorator wraps the chat and embed **providers** inside `chatservice`/`embedservice` impl. Every call is counted, whichever service or feature made it.
- The call's purpose travels in a small `features.Meta` that callers set on `ServiceArgs`: `Feature`, `Level`, and the conversation, round or file references.
- **The provider interfaces need more usage detail**: add cached and thinking token counts to `Usage`. Today only input and output are returned.
- Writes go through a buffered channel and a batch writer, so recording never blocks a reply. They are flushed on shutdown.
- Each record is also published on an `events.Bus[UsageEvent]`.
  - A **VictoriaLogs sink** can subscribe later without touching callers.
  - **Budget checks** subscribe to it too (below).

## `usageservice`
| Method | Purpose |
|---|---|
| `Summary{From, To, GroupBy, Filters}` | `GroupBy` is any of `hour`, `day`, `month`, `feature`, `model`, `level` or `conversation`. Returns totals (requests, each token kind, cost) and one series per group. |
| `Records{From, To, Filters, Page}` | The raw records, paged, for drill-down. |

REST lives under `/api/v1/usage`, with a `vite.config.ts` proxy entry and a `src/services/Usage.ts` client.

## Dashboard
**Opening it**: a `DashboardObject` editor, opened from the AgentOS sidebar section.

**Widgets**
- The Dashboard is a responsive grid of `dashboard.widget` contributions ([Contributors.md](Contributors.md)).
- Each widget declares a default size and can be hidden.
- The layout is saved per user under `ui.dashboard`.

**First widget: Usage & cost**
- Totals for today, 7 days and 30 days: cost, requests, input and output tokens.
- Cost per day, stacked by level (or by model or feature, through a toggle).
- Tokens in vs out, with cached tokens shown as a share of input.
- Top conversations by cost, linking to the conversation.
- The embedding cost of indexing, separate from query embeddings, so a reindex shows as a spike.

**Later widgets**
- Index health: files, chunks, pending queue, last error.
- Chronicle stats: entries per type, active entries.
- Model latency.
- Flagged segments: library chunks and entries with many 👎 votes ([ContextRetrieval.md](ContextRetrieval.md)).

**Live updates**: the dashboard refreshes on focus and every minute. SSE is not needed for v1.

## Later
- **Budgets**: an `ai.budget` section with daily and monthly limits per scope. A subscriber to `UsageEvent` warns in the UI near a limit and, if configured, makes calls return 429 once it is exceeded.
- **Retention**: roll records older than N days into daily aggregates.
- **Export**: CSV or JSON export of records.
- **VictoriaLogs**: a sink once the sidecar exists. SQLite remains the Dashboard's source unless queries outgrow it.

## Built in slice 2
- **`features/usage`**
  - **`usage.Record`**: table `usage_records`, with `db.Tenanted` and a plain `UserID` (0 for background work). Its fields are as above, plus two.
    - `EmbedTokens`.
    - `Estimated`: the Gemini API reports no token count for embeddings, so the decorator estimates about four characters a token and marks the record.
  - **Columns**: the model column is `model` (the Go field is `ModelName`, beside `db.Model`). `Currency` is empty, and the cost 0, when the model is not in the catalog.
- **Meta**: `features.Meta{Feature, Level, ConversationID, RoundID, FileID}` is a field of `ServiceArgs`.
  - `args.CallContext()` carries the principal and Meta down to the providers, and `features.CallFrom(ctx)` reads them back. `chatservice` and `embedservice` pass the call context to their provider.
  - The index sets `embed-index` and `embed-query`. Unset features default to `chat`, `embed` or `embed-query`.
- **The decorators**: `usage.ChatProvider` and `usage.EmbedProvider` wrap the provider `serve.go` hands to `WithProvider`, rather than living inside each service's impl. The effect is the same: every call is counted.
  - **Failed provider calls** are recorded with their status.
  - **Calls rejected before the provider** (validation, no API key) are not, since nothing was billed.
- **`usage.Recorder`**
  - **Pricing**: each record is priced as it is queued, from `ai.models` through `config.Read`, by `aisettings.Pricing.CostMicros`. Input includes cached tokens, and thinking bills as output. The tier with the highest threshold the prompt exceeds wins.
  - **Writing**: a queue of 1024, written in batches of 100 or every second. When the queue is full, a record is dropped with a warning rather than blocking. `Close` flushes on shutdown.
  - **Events**: every stored record is published on `usage.Events`.
- **`usageservice.Records`**: `POST /usage/records` (`from`, `to`, `feature`, `page`, `pageSize`), newest first, within the principal's tenant. The browser client is `src/services/Usage.ts`.
- **DevTools › Usage** lists the records with tokens, cost, latency and status.
- **Provider usage**: `chatservice.Usage` gained `CachedTokens` and `ThinkingTokens`, and `OutputTokens` no longer includes thinking. The chat REST responses carry both.

## Built in slice 4
The debug capture decided in question 2.
- **The setting**: the section **`usage.capture`** (Settings › System › Debug capture, app scope), declared by `features/usage/settings` (`usagesettings`). It holds `enabled` (off by default) and `retentionHours` (24, at most 168).
- **The table**: **`usage.Capture`**, table `usage_captures`, with `db.Tenanted`, `RecordID` (unique), `System`, `Messages` (JSON `[{role, content}]`) and `Reply`. The record gains `Captured`.
- **Only chat calls**: the decorator captures nothing else. Embedding input is volume text, which is already stored.
  - **Capturing**: when `Recorder.Capturing(ctx)` is true, `chatProvider` calls `RecordCaptured`. The record's TSID is assigned up front so the capture can name it.
  - **Writing**: the record and its capture are written in one transaction.
  - **Settings**: `NewRecorder` takes the capture settings as its fourth argument; nil captures nothing.
- **Retention**
  - The recorder deletes captures older than the retention when it starts and every 10 minutes.
  - Turning capture off stops new captures, and the existing ones still expire on time.
- **Reading a capture**: `usageservice.Capture` (`POST /usage/capture {recordId}`, action `usage.readCapture`) returns the text, or 404 `capture_not_found` once it has expired. In DevTools › Usage, a captured record has an icon that opens the prompt and reply.
- **Logs never hold the text**: the capture lives only in SQLite.
- **Verification**: covered by `usage/Providers_test.go`, including the purge. The DevTools dialog has not been seen with a real captured call yet: that needs a Gemini API key, because calls refused before the provider record nothing.
- **Chat rounds** copy their tokens and cost from the call's response, priced like the record ([ChatCompletion.md](ChatCompletion.md#built-in-slice-4)). Their usage records carry `Level`, `ConversationID` and `RoundID`; title calls are `chat-title`.

## Open questions
Moved here from [TODO.md](TODO.md):
1. **Currency**: one display currency with conversion, or each model's own currency? The proposal is USD only for now. *Decision*:
2. **Prompt text in usage records**: the proposal is never, references only. An opt-in debug capture with short retention could come later. *Decision*: references only in `usage_records`, plus an **opt-in debug capture now** (slice 4). When the `usage.capture` setting is on, each chat call's system prompt, messages and reply go to a separate `usage_captures` table and expire after a short retention (24 hours by default). See [Built in slice 4](#built-in-slice-4). Logs still never carry prompts or replies.
3. **Retention**: how long raw records are kept before daily roll-up. The proposal is 90 days. *Decision*:
4. **Budgets**: warn only, or also block with 429? The proposal is warn by default, with blocking configurable per scope. *Decision*:
5. **VictoriaLogs**: it is now planned as the log store ([Logging.md](Logging.md)). Should usage events also go there? The proposal is SQLite as the Dashboard's source, with a VictoriaLogs sink on `UsageEvent` later. *Decision*:

## Built in slice 5
- **`usageservice.Summary`** (`POST /usage/summary`, action `usage.read`)
  - **Args**: `from`, `to`, `bucket` (`hour`, `day`, `month`), `groupBy` (`feature`, `model`, `level`, `conversation`), `feature`, `tzOffsetMinutes` and `limit`.
  - **Buckets are local days**: they are SQLite `strftime` over `created_at`, shifted by the viewer's offset.
  - **Limit** keeps the costliest groups, ranked by their total across the range.
  - **Response**: the range's totals (requests, each token kind, cost) and one row per bucket and group.
  - **Grouping** is one dimension at a time (the plan allowed several). `GroupBy` and `Bucket` are allow-listed, never interpolated from the request.
- **Cost is summed in micro-units whatever the currency.** Every catalog model is priced in USD, so this holds until question 1 is decided.
- **The Dashboard** (`src/editors/dashboard/`)
  - **The editor**: `DashboardObject` (`dashboard:main`) has an icon in the AgentOS sidebar section, beside Chat.
  - **The grid**: a responsive grid of **`dashboard.widget`** contributions (`points.ts`: `{ title, size: 'full' | 'half', component }`, with props `{ refresh, registry }`).
  - **Refreshing**: on window focus and every minute.
  - **Not yet**: hiding widgets and saving the layout (`ui.dashboard`). Browser-only settings cannot be stored yet.
- **Usage & cost** (`dashboard/usage/`)
  - **Tiles**: today, 7 days and 30 days, each with cost, requests, tokens in (with the cached share) and out. A fourth tile shows the 30-day cost and tokens of indexing (`embed-index`), apart from query embeddings.
  - **Cost per day over 30 days**, stacked by level, model or feature (a toggle).
    - It shows the seven costliest groups. The rest fold into a grey *Other*, so there is never a ninth colour.
    - The colours are the fixed order of a categorical palette validated for colour-blind separation, with separate steps for light and dark themes (`palette.ts`).
    - It has a legend, a per-day tooltip, and a **Table** view of the same numbers. Three light-theme colours are under 3:1 contrast, so the table matters.
  - **The costliest conversations**, with titles, requests and cost.
    - Rows do not link to the conversation yet: Chat opens on the latest conversation and has no way to jump to another.
- **Tests**: `usageservice/impl/Summary_test.go` covers buckets, groups, the time zone shift, the limit and tenant isolation. The widget was seen in headless Chromium against a stub Gemini, with the chart, tooltip and table.

## Built in slice 6
Chronicle's calls are recorded like every other, under their own features: `chronicle-match` (stage 1), `chronicle-memory` (the memory update) and `embed-chronicle` (embedding entries), each with the round's conversation and round. The Dashboard's *Usage & cost* groups them by feature already; no widget is new.

## Built in slice 7
- `features.Meta` and `usage.Record` gain **`RunID`** (indexed) and **`NodeID`**: a Cognition node's calls carry its run and node.
- `cognitionservice.GetTrace` sums the records by node for the Log Viewer and the canvas badges. Chronicle's calls keep their features (`chronicle-match`, `chronicle-memory`), so the Dashboard is unchanged.
- The run-level token and cost limits (Cognition.md › Execution) are not enforced yet.

## Built in slice 8
- **Every step of a tool loop is a chat call**, and so its own usage record, attributed to the round (`Feature` `chat`) or to the Agent node's run and node (`Feature` `agent`). A round's tokens and cost sum its steps, each priced on its own.
- **A tool's own calls** (the query embedding of `kb.search`) carry `Feature` `tool.<name>`.
- **The debug capture** keeps each message's tool calls and results, and appends the reply's calls to the captured reply.
