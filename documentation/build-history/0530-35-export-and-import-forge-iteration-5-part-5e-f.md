35. **Export and import** (Forge iteration 5, part 5e, [forge 005e-ExportImport.md](../../forge/documentation/plans/005e-ExportImport.md)). *Built* ([Apps.md](Apps.md#built-in-slice-35)).
    - **Built**:
      - the export document (`apps.Document`: manifest, own parts, tenant parts, required apps' parts, primitives, opt-in sample records) and its check, `apps.CheckDocument`, the blueprints-only check;
      - `appservice` (`POST /apps/export|check|import`) and the `app.export`, `app.check` and `app.import` tools; import plans each part (`new`, `same`, `duplicate` with both sides' versions and dates), refuses until every duplicate is decided (`overwrite`, `keep`), and sends the author's commands;
      - `entity.import`, `app.setManifest`, `app.setSeed`; `Version.Seed` and the `apps.seed` follower creating sample records at install;
      - the browser's `Apps.export|check|import`.
    - **Blockers**: none. Forge iteration question 3 (opt-in seed data) and 05e questions 1 (the app's id) and 2 (tenant parts copied, duplicates asked about), with a follow-up on part ids (kept), were asked before building and answered.
    - **★ review pending**: the export document, read as a person would.
    - **Decisions to review after seeing it**: in [Apps.md](Apps.md#decisions-to-review-after-seeing-it-4).
