19. **Live queries** (Forge iteration 3, part 3e, [forge 003e-LiveQueries.md](../../forge/documentation/plans/003e-LiveQueries.md)). *Built.* ★ the changes a page sends, once a page binds to them in 3i ([Records.md](Records.md#built-in-slice-19)).
    - **Built**:
      - in `records/data`, the `records.find` query: a sorted page that follows the records with insert, update, move and remove, from an ordered window or every match in memory;
      - `reached` changes, and snapshots at the projector's position, for `records.find` and `records.list`;
      - in the live core, list changes on `live.Delta`, `live.Apply` (used by UI sessions) and `Projections.Folded`;
      - in the browser, list changes in `applyDelta` and `useQuery`, and `RecordPage` in `Records.ts`.
    - **Blockers**: none. 03e had no open questions.
    - **Decisions to review after seeing it**: in [Records.md](Records.md#decisions-to-review-after-seeing-it-3).
    - **Pending, carried forward**: binding a query to a component prop, and the session's patches, are forge 3i.
