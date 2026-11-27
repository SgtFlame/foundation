47. **Connections, sessions and event sources** (Forge iteration 7, part 7c, [forge 007c-Connections.md](../../forge/documentation/plans/007c-Connections.md)). *Built* ([Connectors.md](Connectors.md#built-in-slice-47)).
    - **Built**:
      - `connectors/connections`: connections as definitions (`connection.*` commands with inverses, `connectors.connection`), secrets sealed into the SecretStore by the bus (`live.Command.Seal`, `NoTool`), the `connection` resource type, `connections.list|describe`, `connection.test`, `cx.resolve` and `cx.status` served in a queue group, status from call errors, `cx_sources` kept by every process, *On connector event* as a trigger event on `CX_<tenant>`;
      - `connectors`: the ledger, exclusive locks, `Type.Exclusive`, the event stream and buckets, `Dispatcher.Failed`;
      - `connectors/adapter`: `cx.resolve` by default, `Context.Accepted`, idle sessions closed, exclusive calls, leased event sources with cursors and fair shares;
      - `jobs`/`live/nodes`: `TriggerEvent.Stream` and `Accept`; `records`: the `connection` field type (exports leave its values out); `ui`: the `connection.form` primitive, the Jobs view's *Event sources*, a secret-safe command option in the browser; Forge's *Connection form* prefab.
    - **Blockers**: none. Forge 07c questions 1 to 3 answered with their proposals before building (2026-11-11).
    - **Decisions to review after seeing it**: in [Connectors.md](Connectors.md#decisions-to-review-after-seeing-it-1).
    - **Pending, carried forward**: the *Connection* picker in *Call connector*'s panel; pushed events and registration (26b); scripted connectors (7d); the mail contracts and types (7e to 7h).
