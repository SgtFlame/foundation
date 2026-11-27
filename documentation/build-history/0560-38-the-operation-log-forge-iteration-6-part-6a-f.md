38. **The operation log** (Forge iteration 6, part 6a, [forge 006a-OperationLog.md](../../forge/documentation/plans/006a-OperationLog.md)). *Built* ([Apps.md](Apps.md#built-in-slice-38), [Live.md](Live.md#changed-in-slice-38)).
    - **Built**:
      - in `live`: every definition command an op (`Command.Logged`), its inverse or why it has none filled by `Decide` (`Decision.Op`, `Undo`, `NoUndo`) and carried on its first event (`Event.Op`), `Event.Actor`, `Projector.KeyOf`; the bus's `request` (defaulting to the command id), `app` and `actor`, on the command frame and in the ack; command tools logged per chat round or Cognition run;
      - inverses for every `bp.*` (by difference), `prefab.*`, `entity.*` and `app.*` command and the page overlays, installs and publishing marked irreversible;
      - `apps/ops`: the `ops.log` projection keyed by app and sequence, the `ops.list` query grouped by request with list changes, `ops.Entries` and `ops.Replay`;
      - the browser's `CommandOptions` and `OpLog` types; in Forge, the Applications editor naming its app and the seed as one request.
    - **Blockers**: none. Forge 06a questions 1 (client request ids), 2 (inverse ops in `Decide`), 3 (definitions only) and 4 (the client-named app for unowned parts) were asked before building and answered with their proposals.
    - **Decisions to review after seeing it**: in [Apps.md](Apps.md#decisions-to-review-after-seeing-it-7).
    - **Pending, carried forward**: undo and redo by request in every editor (forge 6b), restorable deletes for prefabs, graphs and apps, after which making a prefab or app is undoable.
