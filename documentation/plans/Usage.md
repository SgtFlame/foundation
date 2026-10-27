# Usage and the Dashboard: what remains

Recording, DevTools' Usage tab, the debug capture, `Summary`, the Dashboard and its *Usage & cost* widget are built; see [Usage.md](../Usage.md).

## Budgets
An `ai.budget` section with daily and monthly limits per scope. A subscriber to `usage.Events` warns in the UI near a limit and, if configured, makes calls return 429 once it is exceeded (question 4). A **Budgets** editor shows limits and alerts per tenant and user. Cognition's run-level token and cost limits are enforced through the same records ([Cognition.md](Cognition.md)).

## Records
- **Retention**: roll records older than N days into daily aggregates (question 3).
- **Export**: CSV or JSON export of records.
- **VictoriaLogs**: a sink on `usage.Events` once wanted (question 5). SQLite remains the Dashboard's source unless queries outgrow it.
- **Grouping** by several dimensions at once (`Summary` groups by one today).

## Dashboard
- **Layout**: each widget can be hidden, and the layout is saved per user under `ui.dashboard` (needs browser-only settings, [Configuration.md](Configuration.md)).
- **Usage & cost**: the costliest conversations link to the conversation (needs Chat to open an earlier conversation, [ChatCompletion.md](ChatCompletion.md)).
- **More widgets**:
  - index health: files, chunks, pending queue, last error;
  - Chronicle stats: entries per type, active entries;
  - model latency;
  - flagged segments: library chunks and entries with many 👎 votes ([ContextRetrieval.md](ContextRetrieval.md));
  - log stats charts, with `Stats`/`StatsRange` ([Logging.md](Logging.md)).

## TODO
### Open questions
1. **Currency**: one display currency with conversion, or each model's own currency? The proposal is USD only for now (every catalog model is priced in USD, and `Summary` sums micros whatever the currency). *Decision*:
3. **Retention**: how long raw records are kept before daily roll-up. The proposal is 90 days. *Decision*:
4. **Budgets**: warn only, or also block with 429? The proposal is warn by default, with blocking configurable per scope. *Decision*:
5. **VictoriaLogs**: should usage events also go there? The proposal is SQLite as the Dashboard's source, with a VictoriaLogs sink on usage events later. *Decision*:

(Question 2 is decided; see [Usage.md › Decisions](../Usage.md#decisions).)

### Review after seeing it ★
- **A captured call in DevTools › Usage** with a real model: calls refused before the provider record nothing, so the capture dialog has not been seen with a real call yet.

### Not built yet
- Budgets and alerts; retention and roll-ups; export; the VictoriaLogs sink.
- Hiding Dashboard widgets and saving their layout; linking a costly conversation to Chat; the widgets above.
- `usage.viewAll` enforcement ([RBAC.md](RBAC.md)).
