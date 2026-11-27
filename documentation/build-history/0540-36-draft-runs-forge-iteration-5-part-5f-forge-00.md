36. **Draft runs** (Forge iteration 5, part 5f, [forge 005f-ApplicationsEditor.md](../../forge/documentation/plans/005f-ApplicationsEditor.md)), with Forge's Applications editor ([forge ApplicationsEditor.md](../../forge/documentation/ApplicationsEditor.md)). *Built* ([Apps.md](Apps.md#built-in-slice-36)).
    - **Built**:
      - `ui.Page.Drafts` (prefabs rendered and graphs run in their drafts, commands sent) and `Handle.Drafts`;
      - `<appId>/draft` in the `apps` app source, following the draft manifest live through its own `app.describe`; `mainPage` over a manifest source, shared with installed apps;
      - `draft.<appId>.<name>` commands and queries resolved by `installs.CommandFor`/`QueryFor` from the draft's graphs, needing `app.write`; the app id `draft` refused;
      - `AppHost` naming its app by its page for other kinds of tab.
    - **Blockers**: none. Forge iteration question 1 (draft runs) was answered before; 05f questions 1 (no undo), 2 (parts edited in their editors) and 3 (the draft run's shape, and publishing entity types alone) were asked before building and answered.
    - **★ review pending**: the Applications editor's tabs with two authors (forge 5f).
    - **Decisions to review after seeing it**: in [Apps.md](Apps.md#decisions-to-review-after-seeing-it-5).
