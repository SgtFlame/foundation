25. **Prefab drafts, publish and versions** (Forge iteration 4, part 4a, [forge 004-Prefabs.md](../../forge/documentation/plans/004-Prefabs.md)). *Built* ([ServerUI.md](ServerUI.md#built-in-slice-25)).
    - **Built**:
      - `ui.Prefab` (props, state, events, slots, queries, a template of `ui.Node`s with `slot` and `prefab.<id>` nodes, graphs, preview) and `ui.CheckPrefab`, every problem at a path with a fix;
      - `ui/prefabs`: drafts on the `def` stream (`ui.prefab`, `ui.prefabPublished`), the `prefab.*` commands (tree edits by key paths, `updateNode` merging per prop), each checking the whole definition, `prefab.describe` and `prefab.list`, publishing with the breaking-change check, and the versions follower into `ui_prefabs` and `ui_prefab_versions`;
      - `prefabservice` (`/ui/prefabs`), the `prefabs` resource type, the `prefab.describe`, `prefab.list` and `prefab.listTypes` tools, and `Prefabs.ts`;
      - in `live`, `Command.NewTargetFor`;
      - the wiring, in Foundation's `serve.go` and Forge's.
    - **Blockers**: none. Forge 04 question 3 (per app and a tenant library) was answered before; 04a questions 1 (access), 2 (two designers on one node), 3 (where definitions live) and 4 (breaking changes) were asked before building and answered with their proposals.
    - **Decisions to review after seeing it**: in [ServerUI.md](ServerUI.md#decisions-to-review-after-seeing-it-1).
    - **Pending, carried forward**: deleting a prefab; checking pages' instances at publish (forge 005-Applications); rendering prefabs (forge 4b).
