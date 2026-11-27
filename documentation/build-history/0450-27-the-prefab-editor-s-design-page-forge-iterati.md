27. **The Prefab Editor's design page** (Forge iteration 4, part 4c, [forge 004c-PrefabEditor.md](../../forge/documentation/plans/004c-PrefabEditor.md)). *Built* ([ServerUI.md](ServerUI.md#built-in-slice-27); the editor is Forge's, [forge PrefabEditor.md](../../forge/documentation/PrefabEditor.md)).
    - **Built**:
      - `ui.DesignPage()` (`prefabs/design`): a prefab's draft with its shared preview data, each node tagged with its template key path (`Node.Src`);
      - dry pages (`Page.Dry`): commands come back as `dryRun` actions, and handlers and logic run their graphs' drafts (`Handle.Dry`, `engine.Draft`, `drafts.Source.Draft`);
      - `UiRenderer`'s `design` mode (`select`, `interact`);
      - fixes: root patches' path `[]`, patch index 0 sent, and a prefab whose root is another prefab drawn when made while open.
    - **Blockers**: none. Forge 04c questions 1 (a dry preview session, select on click, *Interact*), 2 (shared preview data) and 3 (no undo) were asked before building and answered with their proposals.
    - **Decisions to review after seeing it**: in [ServerUI.md](ServerUI.md#decisions-to-review-after-seeing-it-3).
    - **Pending, carried forward**: undo of one's own changes (forge 006-AIAuthoring); `GET /ui/components` still lists only primitives (the editor reads published prefabs from `prefab.list`).
