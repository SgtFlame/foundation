41. **Applying, checking and previewing** (Forge iteration 6, part 6e, [forge 006e-ApplyCheckPreview.md](../../forge/documentation/plans/006e-ApplyCheckPreview.md)). *Built* ([Apps.md](Apps.md#built-in-slice-41)).
    - **Built**:
      - `bus.Transact` makes new targets; `tools.Tool.Asks` decides per call whether a tool asks; `authz.WithAs` decides checks by a role in the context;
      - `manifests.DraftDocument`, `CheckDraft` and `Lint`: a draft checked as it would be exported, with warnings;
      - in `apps/tools`: `forge.apply` (one atomic batch per call, others' changes since `after`), `forge.changes`, `forge.check`, `forge.preview` (the draft run as a manifest role, summarized) and `forge.undo`; `forge.app` gives `after`;
      - Forge's `apply_test.go` over the starter library and the Tasks sample, and `authz`'s `TestAs`.
    - **Blockers**: none. Forge 06e questions 1 (`bus.Transact`), 2 (apply over a stale view, reporting changes) and 3 (a manifest role, on the draft run's records), and iteration question 2 (drafts change without asking), were asked before building.
    - **Decisions to review after seeing it**: in [Apps.md](Apps.md#decisions-to-review-after-seeing-it-10).
    - **Pending, carried forward**: the Builder mode and its prompt (forge 6g), which shows these tools to a model.
