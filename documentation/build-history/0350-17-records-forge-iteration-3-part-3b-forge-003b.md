17. **Records** (Forge iteration 3, part 3b, [forge 003b-Records.md](../../forge/documentation/plans/003b-Records.md)), with Forge's Data Designer (part 3c). *Built.* ★ in the Data Designer ([Records.md](Records.md#built-in-slice-17), [forge DataDesigner.md](../../forge/documentation/DataDesigner.md)).
    - **Built**:
      - `records/data`: the `records.record` projector, the `record.*` commands, the `records.get` and `records.list` queries, and the converter;
      - value checks and conversion in `features/records`; `entity.delete`, `entity.restore` and type changes in `records/entities`;
      - `recordservice` under `/records/records`, the `records` section, and the `records.query` and `records.import` tools;
      - in the live core, `Command.TypeOf`, `NewTarget` and `Internal`, and `Problem.Code`;
      - the `Records.ts` client;
      - the wiring, in Foundation's `serve.go` and Forge's.
    - **Blockers**: none left. Its **Before** questions were answered before building: forge 003-DataModel 7 (one bucket per tenant), and 03b 1 (convert values) and 2 (hide a deleted type).
    - **Decisions to review after seeing it**: in [Records.md](Records.md#decisions-to-review-after-seeing-it-1).
