46. **The connector registry and bus** (Forge iteration 7, part 7b, [forge 007b-ConnectorRegistry.md](../../forge/documentation/plans/007b-ConnectorRegistry.md)). *Built* ([Connectors.md](Connectors.md#built-in-slice-46), [Cognition.md](Cognition.md#changed-in-slice-46), [Apps.md](Apps.md#changed-in-slice-46), [Live.md](Live.md#changed-in-slice-46); Forge's palette in [forge BlueprintEditor.md](../../forge/documentation/BlueprintEditor.md)).
    - **Built**:
      - `features/connectors`: versioned contracts, connector types, typed errors, the metadata check with the tool shape, `ToolSpec`/`MCPTool`/`FunctionSpec`/`OpenAPI` with golden tests, the registry over the builtin points and `cx_adapters` announcements, discovered contracts in `cx_discovered_<tenant>`, the dispatcher, the `connectors.*` queries;
      - `connectors/adapter`: the adapter runtime, in-process or external, with the interfaces declared for 7c, 75c and 26b and surfaces announced;
      - `connectors/nodes`: *Call connector* and *On connector event*, pins normalized from the contract; `NodeType.SampleFor`; `bp.import` normalizing what it can;
      - `forge.catalog`'s `connectors` area; `natsd.Server.Connect`, `ClientURL` and `StartTestListening`;
      - Forge's *Connectors* palette and connector details.
    - **Blockers**: none. Forge 07b questions 1, 2, 4 to 10 answered with their proposals, and 3 by the user's direction, before building (2026-11-11).
    - **Decisions to review after seeing it**: in [Connectors.md](Connectors.md#decisions-to-review-after-seeing-it).
    - **Pending, carried forward**: connections, sessions with settings, the ledger and event sources (7c); scripted connectors (7d); the mail contracts (7e); the inbound gateway, MCP and hooks (15).
