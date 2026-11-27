29. **The starter library's primitives** (Forge iteration 4, part 4e, [forge 004e-StarterLibrary.md](../../forge/documentation/plans/004e-StarterLibrary.md)). *Built* ([ServerUI.md](ServerUI.md#built-in-slice-29); the prefabs themselves are Forge's, [forge StarterLibrary.md](../../forge/documentation/StarterLibrary.md)).
    - **Built**:
      - `prefab.import` (a whole definition replaces the draft);
      - the `presence:<binding>` binding source, and `=` expressions over props in query params;
      - the primitives `record.fieldEditor`, `record.fieldValue`, `record.form` (the form merge), `presence.avatars` and `mui.breadcrumbs`; `mui.textField`'s `debounce`; `mui.table`'s `recordField` and actions columns;
      - `_title` on expanded records; the `tenants.action` point in Administration › Tenants; `foundation(backend, more)` routes.
    - **Blockers**: none. Forge 04e questions 1 (seeded by Forge, a CLI and a Tenants button), 2 (plus a form-merge primitive) and 3 (the changer's name) were asked before building and answered.
    - **Decisions to review after seeing it**: in [ServerUI.md](ServerUI.md#decisions-to-review-after-seeing-it-5).
    - **Pending, carried forward**: rich-text formatting in the form; a user picker for `user` fields; variants and the chat acceptance (forge 4f).
