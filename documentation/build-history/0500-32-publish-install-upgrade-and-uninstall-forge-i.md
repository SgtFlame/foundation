32. **Publish, install, upgrade and uninstall** (Forge iteration 5, part 5b, [forge 005b-PublishInstall.md](../../forge/documentation/plans/005b-PublishInstall.md)). *Built* ([Apps.md](Apps.md#built-in-slice-32)).
    - **Built**:
      - `app.publish {rev}`: the manifest at the rev its publisher saw (`Projections.History`), checked against published parts only (with `ui.CheckPrefab` on every prefab it renders), frozen as an `apps.Version` pinning prefab and entity type versions and copying its graphs; the `apps.version` projection, `apps_versions`, `app.versions`;
      - `apps/installs`: `app.install`, `app.upgrade`, `app.uninstall` on the `appInstall` target, `requires` checked, the `apps.install` projection holding the whole version, `apps_installs`, `app.installs`;
      - an installed app's commands and queries as `<appId>.<name>` (`installs.CommandFor`/`QueryFor` around the graph resolvers), decided and answered by the pinned graphs (`livenodes.Decide`, `livenodes.RunQuery`), a fresh snapshot after an upgrade; app-scoped graph ids (`Kind.IDFor(spec, owner)`); a manifest app's graphs live only through its install;
      - the browser's types in `src/services/Apps.ts`.
    - **Blockers**: none. Forge iteration question 5 and 05b questions 1 to 4 were asked before building and answered with their proposals.
    - **★ review pending**: an upgrade seen by an open session (5d).
    - **Decisions to review after seeing it**: in [Apps.md](Apps.md#decisions-to-review-after-seeing-it-1).
    - **Pending, carried forward**: `app.delete`; the Applications editor (forge 5f); publish and install (5b), where an app's command graphs, already tenant commands once published, must be kept apart (forge 05b question 3).
