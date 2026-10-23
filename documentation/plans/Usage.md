# Usage: token and cost tracking, and the Dashboard

## Context
Every AI call costs money: chat replies, Chronicle's internal calls, and the embeddings the index makes in bulk. We want to see tokens and cost per day, level, model, feature and conversation.

The earlier app stored this in VictoriaLogs. That sidecar does not exist here yet, so usage goes into SQLite and the design keeps a seam for a log sink later.

Prices come from the model catalog (`ai.models` in [Configuration.md](Configuration.md)).

Status: partly built. Slice 2 built recording, with no UI beyond a DevTools tab; see [Built in slice 2](#built-in-slice-2). The Dashboard and `Summary` come with slice 5.

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

## Open questions
Moved here from [TODO.md](TODO.md):
1. **Currency**: one display currency with conversion, or each model's own currency? The proposal is USD only for now. *Decision*:
2. **Prompt text in usage records**: the proposal is never, references only. An opt-in debug capture with short retention could come later. *Decision*:
3. **Retention**: how long raw records are kept before daily roll-up. The proposal is 90 days. *Decision*:
4. **Budgets**: warn only, or also block with 429? The proposal is warn by default, with blocking configurable per scope. *Decision*:
5. **VictoriaLogs**: it is now planned as the log store ([Logging.md](Logging.md)). Should usage events also go there? The proposal is SQLite as the Dashboard's source, with a VictoriaLogs sink on `UsageEvent` later. *Decision*:
