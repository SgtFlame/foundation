55. **What the Mail app's sync needed** (Forge iteration 7, part 7n, [forge MailApp.md](../../forge/documentation/MailApp.md)). *Built* ([Connectors.md](Connectors.md#built-in-slice-55)).
    - **Built**:
      - `connectors/connections`: connections held by an app's own records (`Held`, `Deps.CheckUse`, `UseConnection`), used by every `use` check an app meets, and the `connectors.holders` follower that reconciles event sources when such records change;
      - `records/data`: `Deps.UseConnection` (setting a connection field checks its sender may use it) and `HeldFiles` (file references naming a holder record the caller may read);
      - `connectors`: `Dispatcher.Files`, the `Cx-Files` header and `adapter.Context.Files`/`Vouched`;
      - `serve.go` (Foundation's and Forge's): `UseConnection` and the dispatcher's `Files` wired.
    - **Blockers**: none. Forge 07n questions 3 (files held by a readable record) and 4 (connections held by the app's records, a gap found while reading) answered by the user before building (2026-11-21).
    - **Decisions to review after seeing it**: in [Connectors.md](Connectors.md#decisions-to-review-after-seeing-it-5).
    - **Deferred failure**: `records/data`'s known flake, seen again as `TestARecordFieldIsEditedInARoom` ([forge 099-Cleanup.md](../../forge/documentation/plans/099-Cleanup.md#foundation-recordsdata-the-package-fails-about-half-its-runs)).
