42. **Testing blueprints in a sandbox** (Forge iteration 6, part 6f, [forge 006f-SandboxTests.md](../../forge/documentation/plans/006f-SandboxTests.md)). *Built* ([Apps.md](Apps.md#built-in-slice-42)).
    - **Built**:
      - in `live/bus`: `Overlay` and `Dry`, a dry run that decides commands over the projections with its own events folded in and commits nothing, carried by the context (`WithDry`); a target's expected sequence checked on its first event in a batch;
      - command graphs read their own writes (`livenodes.DecideObserved`), and the records' sandbox (`data.Deps.For`) is behind the record nodes;
      - `cognition.NodeType.External` and `Sample`, and `engine.RunArgs.Stub` and `Steps`;
      - in `apps/tools`: `forge.test`, for command, data.query, server.action, ui.handler and prefab.logic graphs, with seeds, a role, the events, the decided commands, a cut trace and the failed node;
      - Forge's `sandbox_test.go` over the starter library and the Tasks sample.
    - **Blockers**: none. Forge 06f questions 1 (the app's records, with seeds) and 2 (external nodes stubbed with a sample) were asked before building.
    - **Decisions to review after seeing it**: in [Apps.md](Apps.md#decisions-to-review-after-seeing-it-11).
    - **Pending, carried forward**: live queries, search and followers in the sandbox; the Builder mode (forge 6g), which shows the tool to a model.
