18. **Indexes and the query planner** (Forge iteration 3, part 3d, [forge 003d-Indexes.md](../../forge/documentation/plans/003d-Indexes.md)). *Built.* ★ query speed with 100 000 records ([Records.md](Records.md#built-in-slice-18)).
    - **Built**:
      - order-preserving sort tokens in `features/records`, and index checks in `records.Check`;
      - in `records/data`, the index entries kept by the record projector in the same fold, the in-memory trees, the indexer, and the query planner (`Find`), which `records.list` uses for its snapshot;
      - `recordservice.Find` at `POST /records/records/find`, `records.query` over it, and `Records.find`;
      - in the live core, `Projector.Keep` with `Projected.Kept`, folds that write with the revision they read, and `Rekeep`, `Side`, `Updates` and `Settle` on the projections.
    - **Blockers**: none left. Its **Before** questions were answered before building: forge 003-DataModel 2 (the same fold) and 03d 1 (order kept in memory, over the KV entries).
    - **Decisions to review after seeing it**: in [Records.md](Records.md#decisions-to-review-after-seeing-it-2).
    - **Pending, carried forward**: enforcing unique indexes. Live sorted pages are forge 3e.
