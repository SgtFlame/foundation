31. **App drafts and the manifest** (Forge iteration 5, part 5a, [forge 005a-AppDrafts.md](../../forge/documentation/plans/005a-AppDrafts.md)). *Built* ([Apps.md](Apps.md#built-in-slice-31)).
    - **Built**:
      - `apps.Manifest` and `apps.CheckManifest` (routes and their params against the prefab's props, nav, home, frame, editor kinds, entities, command and query graphs defining their names, roles' actions and row rules, settings, triggers, requires, and the owner of every part);
      - drafts on the `def` stream (`apps/manifests`, the `apps.app` projection) changed by `app.create`, `app.setInfo`, `app.setNav`, `app.setSettingsSchema` and `app.add|update|remove<Item>` per keyed list, an update a merge patch on the item; a renamed route followed in home, nav and editor kinds;
      - `app.describe` (with the parts it names or owns, following their drafts) and `app.list`; their tools; `apps_apps` kept by a follower; the `app` resource type;
      - `src/services/Apps.ts`, the manifest's types for the browser.
    - **Blockers**: none. Forge 05a questions 1 (KV drafts, the app in SQLite by a follower), 2 (owned parts, others as requirements), 3 (`read`, `write`, `publish`, `install`) and 4 (per item, per field) were asked before building and answered with their proposals.
    - **Decisions to review after seeing it**: in [Apps.md](Apps.md#decisions-to-review-after-seeing-it).
