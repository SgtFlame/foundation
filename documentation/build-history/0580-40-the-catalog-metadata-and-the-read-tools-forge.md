40. **The catalog, metadata and the read tools** (Forge iteration 6, part 6d, [forge 006d-CatalogMetadata.md](../../forge/documentation/plans/006d-CatalogMetadata.md)). *Built* ([Apps.md](Apps.md#built-in-slice-40), [Cognition.md](Cognition.md#changed-in-slice-40), [Records.md](Records.md#changed-in-slice-40), [ServerUI.md](ServerUI.md#changed-in-slice-40)).
    - **Built**:
      - metadata on every catalog item: node types' JSON examples and tags, field types' titles, tags and examples, prefab tags, every primitive tagged; every command's fields described, with an example; pages' and parts' descriptions;
      - in `apps/tools`: `forge.catalog`, `forge.describe`, `forge.app` (with problems, presence and position) and `forge.read`, beside the existing read tools;
      - tests over every registered item (`TestMetadata`, `TestFieldTypesMetadata`) and, in Forge, over the starter library and the Tasks sample (`TestForgeTools`); the starter prefabs tagged.
    - **Blockers**: none. Forge 06d questions 1 (both sets of tools everywhere, the user's choice), 2 (descriptions optional), 3 (an empty `connectors` area) and 4 (node examples and tags derived, a gap found while reading) were asked before building.
    - **Decisions to review after seeing it**: in [Apps.md](Apps.md#decisions-to-review-after-seeing-it-9).
    - **Pending, carried forward**: `forge.check`'s warning for a missing part description (forge 6e); which read tools the Builder gets (forge 6g).
