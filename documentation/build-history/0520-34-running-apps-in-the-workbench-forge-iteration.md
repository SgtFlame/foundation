34. **Running apps in the workbench** (Forge iteration 5, part 5d, [forge 005d-AppRuntime.md](../../forge/documentation/plans/005d-AppRuntime.md)). *Built* ([Apps.md](Apps.md#built-in-slice-34)).
    - **Built**:
      - the `ui.appSource` point and `apps/installs/Pages.go`: installed apps, their kinds (`app:<appId>:main`, `app:<appId>:<kind>`) and pages resolved from the install, with their reach, pinned prefab versions, copied graphs and scoped command and query names;
      - the main tab (`ui.AppPage`): the frame around the page at the current route, the tab's own history in view state (`app.navigate`, `app.back`, `app.forward`), root instances' events reaching the page's handlers, the *Navigate* node; an editor kind's tab with its record and the manifest's title and subject templates (`Page.Heading`, sent again when they change); presence on `record_<entity>.<id>`; upgrades reopening sessions (`page_changed`) and removed pages failing to open (`app_page_gone`);
      - the live `ui.editorKinds` query; the Apps section shown by reach, tabs of removed kinds closing, `openApp` at a route, deep links (`/workbench?app=…&route=…`), `AppHost` without a header in a frame, renamed by the page's title;
      - `grow` and `fill` on `mui.box` and `mui.stack`, five icons; `ui.ApplyPatch`;
      - Forge: *App frame* in the starter library, the Records app as a manifest app seeded and installed by `cli seed`.
    - **Blockers**: none. Forge iteration questions 1 (run drafts, built with 5f) and 2 (the app's own history), and 05d questions 1 (Hello stays Go, Records becomes a manifest app) and 2 (*App frame* in Forge), were asked before building and answered with their proposals.
    - **★ review pending**: the Apps section with several apps; the frame, its navigation and back and forward.
    - **Decisions to review after seeing it**: in [Apps.md](Apps.md#decisions-to-review-after-seeing-it-3).
