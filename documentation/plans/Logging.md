# Logging: what remains

The builder, the request middleware, VictoriaLogs storage, `logsservice`, browser forwarding, the Log Explorer and full logging for one user are built; see [Logging.md](../Logging.md).

## Saved queries
Name, query, time range and columns, scoped per user or shared with the tenant, stored in a `SavedLogQuery` table, with a list in the Log Explorer.

## Stats over time
`Stats{Query, Time}` and `StatsRange{…, Step}` in `logsservice`, over `/select/logsql/stats_query` and `stats_query_range`, for the Dashboard and for charting a `stats` query over time in the Explorer (today a `stats` query runs through `Query` and shows as a table).

## Links into the Explorer
- **The error toast**: an error toast in the UI links to the Explorer filtered by its request's trace. The UI has no error toast yet; DevTools' *Show in Log Explorer* covers the case for now.
- **Full logging from the Explorer**: setting full logging for one user from a record's user, beside the Users editor's switch.
- **Cognition runs**: each `RunStep` logs a summary record with `trace` = the run ID, so runs show up in the Explorer ([Cognition.md](Cognition.md)).

## Alerts
Alerts on log queries, for example through vmalert, which supports LogsQL.

## TODO
### Review after seeing it ★
- **The decisions of slice 10** in [Logging.md › Built in slice 10](../Logging.md#built-in-slice-10): the browser polls for its level every 5 minutes rather than through the config event stream; the level is always `debug`, for 24 hours; stderr is unchanged.

### Not built yet
- Saved queries.
- `Stats` and `StatsRange`, and stats charts (with the Dashboard, [Usage.md](Usage.md)).
- The error toast's link to the Explorer; setting full logging from the Explorer; Cognition run summaries.
- Alerts on log queries.
- **Known limits of the Explorer**: the histogram counts only the filters before the query's first `|`, so it ignores pipes, and a `|` inside a quoted string cuts the query short for it; the builder writes only its five pipes; live tail cannot follow every query (VictoriaLogs refuses some, such as `stats`).
