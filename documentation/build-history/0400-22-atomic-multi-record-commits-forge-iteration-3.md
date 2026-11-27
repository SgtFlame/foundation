22. **Atomic multi-record commits** (Forge iteration 3, part 3h, [forge 003h-Transactions.md](../../forge/documentation/plans/003h-Transactions.md)). *Built.* No ★ of its own: seen through the task list (3i) ([Live.md](Live.md#built-in-slice-22)).
    - **Built**:
      - in `live`, events on other targets of the command's area (`NewEvent.Target`), `Decision.Read`, and `Event.Batch`;
      - in `live/project`, `PublishBatch` (JetStream's atomic batch publish, headers sent by hand), streams and projection buckets that allow it, and projectors that write a batch's values in one atomic batch;
      - in `live/bus`, several events committed as one batch, each expecting its target's sequence; retries holding every target in key order; resent batches acknowledged as duplicates.
    - **Blockers**: none. Forge 003-DataModel question 4 (a batch or one subject per period, for a ledger) is needed before forge 14, not here.
    - **Decisions to review after seeing it**: in [Live.md](Live.md#decisions-to-review-after-seeing-it-1).
    - **Pending, carried forward**: a batch's changes in one frame; `records.import` as one command per batch.
