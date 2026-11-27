52. **The relational projection** (Forge iteration 7, part 7k, [forge 007k-RelationalProjection.md](../../forge/documentation/plans/007k-RelationalProjection.md)). *Built* ([Records.md](Records.md#built-in-slice-52)).
    - **Built**:
      - `records`: `EntityType.Storage` and `entity.setStorage`; `records_tables` and `records_db_instance`;
      - `records/data`: `Tables` (a table per large type in the tenant database, followed from the `rec` stream by a consumer per database, migrated or rebuilt from the stream as versions change), the planner reading it (filters, rules, sorts and keyset pages in SQL, every row checked in Go), aggregates in SQL, `records.find` over it run again on changes;
      - `apps/manifests`: `forge.check` warns about a large table's rule that does not compile to SQL;
      - browser: `Entities.ts` `storage`; Forge's Data Designer *Large table* switch.
    - **Blockers**: none. Forge 07k question 1 answered by the user (GORM and portable SQL; full-text search behind an interface on LanceDB) and question 2 (a consumer per database) with its proposal, before building (2026-11-20).
    - **Decisions to review after seeing it**: in [Records.md](Records.md#decisions-to-review-after-seeing-it-8).
    - **Pending, carried forward**: the full-text budget at 50 000 records (LanceDB's); Postgres (a `C` collation on token columns); a grouped count over a field without an index.
