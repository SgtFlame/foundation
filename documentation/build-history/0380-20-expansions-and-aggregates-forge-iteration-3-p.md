20. **Expansions and aggregates** (Forge iteration 3, part 3f, [forge 003f-ExpansionsAggregates.md](../../forge/documentation/plans/003f-ExpansionsAggregates.md)). *Built.* ★ the decisions, once a page shows them ([Records.md](Records.md#built-in-slice-20)).
    - **Built**:
      - in `records/data`, `Query.Expand` (related records under `_expanded.<field>`, users read from identity), aggregations (`Summarize`) and the `records.aggregate` query;
      - queries with expansions and aggregates run again, debounced (the `records` section's `debounceMs`), and send the differences as list changes;
      - `recordservice.Aggregate` at `POST /records/records/aggregate`, the `records.aggregate` tool, `expand` on `records.query`, and `Records.aggregate` with `RecordGroups` in the browser;
      - `Deps.Users`, wired in `serve.go` (Foundation's and Forge's) over identity's users and memberships.
    - **Blockers**: none. 03f had no open questions; three gaps found while reading were asked before building (users in expansions, the query's shape, with 3g's rules).
    - **Decisions to review after seeing it**: in [Records.md](Records.md#decisions-to-review-after-seeing-it-4).
    - **Pending, carried forward**: a change to a user does not run an expansion of them again; rollup fields are 3j's computed fields.
