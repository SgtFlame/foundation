# Cognition: the Cognition Engine and the Cognition Editor

## Context
The chat pipeline in [Chronicle.md](Chronicle.md) is a fixed sequence:
1. the window;
2. retrieval;
3. stage 1;
4. stage 2;
5. the memory update.

Hard-coding it means every new mode, persona or agent is a code change. **Cognition** makes that logic data: a graph designed visually, like Unreal Engine Blueprints, and run by a deterministic engine.

The same graphs define:
- **Chat modes**: the default *Personal* mode, which reads and updates Chronicle memory, becomes a built-in graph that users can copy and change.
- **Agents**: personas built from prompt templates, tools and a level, callable from other graphs.
- **Multi-agent collaboration** through a shared **Nexus**. Agents read and write named streams in the Nexus and reconcile them over iterations; for example, product owner, database designer and UI/UX designer agents converging on one design.
- **Chronicle's custom-type summarizing modes**, which [Chronicle.md](Chronicle.md) called "blueprint execution graphs".

The Cognition Engine (backend, `backend/features/cognition`) runs graphs; the Cognition Editor (frontend, React Flow, `frontend/common/src/editors/cognition/`) shows them. Slice 7 built the **skeleton**: the engine with linear exec flow and pure nodes, Chronicle's steps as nodes, *Personal* as a built-in graph that chat runs, and the read-only Cognition Editor with the Log Viewer and the details panel over real traces. Slice 8 added the `Agent` node. The full design (drafts and publishing, the node library, prompt templates, the Nexus, loops, debugging, Replay, chat modes, editing) and what remains are in [plans/Cognition.md](plans/Cognition.md).

## Decisions
- **Collaborative graph editing** (Cognition 2): drafts will be **shared Yjs documents**, like markdown: a draft is a collab room that several people edit at once, relayed by the collab server, and publishing snapshots the room into an immutable version. Decided before slice 7; the room arrives with the editing pass.
- **Template helpers** (Cognition 4): **global**. The safe helpers (`join`, `indent`, `json`, `truncate`, `default`) form one global set, and domain helpers such as `GetFeature` wait until a graph needs one.
- **Trace retention** (Cognition 5): **both**: a trace is kept while it is among the last N runs of its graph *or* younger than N days, both configurable in the `cognition` section.

## Built in slice 7

**The engine** (`backend/features/cognition`)
- **Model** (`cognition`): `Graph` (`id`, `name`, `description`, `kind`, `version`, `nodes`, `edges`), `Node` (`id`, `type`, `title?`, `position`, `config?`), `Edge` (`from`/`to` as `{node, pin}`). This JSON is the graph's document, so the REST layer sends it as it is.
- **Three extension points**, declared in `serve.go`:
  - `cognition.node` (`cognition.Nodes`): a `NodeType` is a **struct with function fields**, like the other contributions, rather than the interface the plan sketched. It has `Title`, `Category`, `Description`, `Color?`, `Event`, `Config` (a schema), `Pins` (or `PinsFor(config)` when pins follow the config, as Return's do) and `Run(x *Exec, in Values) (Values, execPin, error)`.
  - `cognition.graph` (`cognition.Graphs`): built-in graphs, read-only. This point is new: the plan seeded built-ins into a table, but slice 7 has no table of graphs.
  - `cognition.type` (`cognition.Types`): the data types and their colours. Each feature contributes its own: core `string`, `number`, `bool`, `json`, `any`; Chat `Round`, `Round[]`, `RoundSegment[]`, `ContextItem[]`; Chronicle `EntryRef[]`, `Candidates`, `Selection`, `Changes`.
- **Values** pass between nodes as Go values (`cognition.Values`), with no serialization. `cognition.Get[T]` reads a pin and converts it through JSON only when its Go type differs, such as an event input decoded from JSON.
- **Validation** (`cognition.Resolve`): unknown types; edges to missing pins, exec joined to data, or a type mismatch; an input fed twice or an exec output leading twice; a required input left unconnected; a cycle among pure nodes; no event, or two of the same event. A warning: an exec node no event reaches. `serve` logs the built-ins' problems at start, and a run refuses a graph with errors. **Implicit conversions are not built**: only equal types and `any` connect.
- **Execution** (`cognition/engine`): `Engine.Run(RunArgs{GraphID, Event, Inputs, Trigger})` runs synchronously:
  - It follows exec flow from the event node of that type. The event's outputs are the run's inputs.
  - Before a node runs, its inputs are pulled. A pure source is evaluated once per run; an exec source must already have run (else `input_not_ready`).
  - A node leaving by an exec pin with no edge ends the run. Return nodes set the run's outputs.
  - **Limits** come from the new `cognition` section: `maxSteps` (1000) and `maxWallSeconds` (300). A run that exceeds one fails on the offending node. The token and cost limits wait.
  - Each node's calls carry `Meta.RunID` and `Meta.NodeID`, so usage records name the run and node (Usage.md).
- **Trace**: tables `cognition_runs` (`cognition.Run`: graph and version, event, trigger, status, error and failed node, conversation and round, start, duration, steps, outputs) and `cognition_run_steps` (`cognition.Step`: seq, node, type, context, inputs, outputs, exec pin taken, start, duration, error). Both are tenanted, and runs are owned.
  - Values are captured as JSON per pin. A value larger than `maxValueKB` (256) becomes `{"$truncated": bytes, "preview"}`; **there are no blob files yet**. Integers beyond 2^53 become strings, so IDs survive the browser.
  - The steps are written in one transaction when the run ends, so a running run has no live trace yet.
  - **Retention** (question 5): `Engine.Purge(keepRuns, keepDays)` deletes runs that are neither among the last `keepRuns` (50) of their graph in their tenant nor younger than `keepDays` (7), with their steps. It runs after a run, at most every 10 minutes.
- **`cognitionservice`** (`/api/v1/cognition`), action `cognition.read`:
  - `GET graphs` lists them.
  - `POST graph {id}` returns the graph, each node's resolved `pins` and its `problems`.
  - `GET node-types` returns the node types and the data types.
  - `POST runs {graphId, conversationId?, before?, limit?}` lists the principal's runs, newest first.
  - `POST trace {runId}` returns the run, its steps and `usage` per node (summed from usage records by `run_id`, so it lags the call by about a second).

**Chronicle's steps as nodes, and *Personal*** (seam 3)
- `conversation/nodes` contributes Chat's events, **On user message** (`event.userMessage`: text, window, settings) and **On reply** (`event.reply`: round, segments, settings). `cognition/nodes` contributes **Branch** and **Return**.
- `chronicle/nodes` contributes six node types, wrapping the step functions unchanged:
  - Chronicle settings (pure);
  - Candidate entries (`Candidates`);
  - Match context (`Match`, stage 1);
  - Entries as context (pure: the selection as pinned items);
  - Used entries (pure);
  - Update memory (`Update`, config `maxProposals`).
- It also contributes **Personal** (`personal.json`, embedded, version 1), whose two event chains reproduce slice 6's provider and follower:
  - *On user message* → Candidate entries → Match context → Return items;
  - *On reply* → Branch (*Update memory?*) → Update memory → Return changes.
- **How chat uses it.** `conversationservice.Send` is unchanged. The `chronicle` context provider runs Personal's On user message event, and the `chronicle` round follower runs its On reply event. Each round is therefore **two runs** of Personal, both recorded with the round. The window, the reply, the title and the other providers stay Go code in `Send`. The plan's "Send runs the conversation's mode graph" waits for `chat.modes` and the Stream reply node.
- **Behaves identically**: `chronicle/nodes/Personal_test.go` runs Personal against the same fakes and checks that its items equal what calling the steps directly gives. It also checks that the On reply event updates memory only when the settings allow, and that stage 1's usage names the run and the `match` node. The step tests are unchanged.

**The Cognition Editor** (`frontend/common/src/editors/cognition/`)
- **Opening it**: from the **Cognition** side view (the Explorer: graphs by kind) or **AgentOS › Cognition** in the Editors view, which opens Personal. `CognitionObject` has id `graph-<id>`.
- **Layout**: a nested flexlayout with the **Logic Editor** and **Log Viewer** tabs and a **Details** tab on the right. They share state through `CognitionContext` (`cognitionState.ts`).
- **Logic Editor** (`GraphCanvas.tsx`): React Flow, **read-only** (no dragging or connecting).
  - Each node is drawn from its type and resolved pins: a header in its category's colour, exec pins as arrows, and data pins as circles in their type's colour.
  - Data edges take their type's colour, and exec edges are thicker and grey.
  - **Run overlays** for the shown run: edges that ran are animated and the rest fade; each node that ran shows a badge with ×count, duration, tokens in→out and cost; the failed node is outlined in red.
  - Problems show as an icon on the node and in red on the pin.
  - There is a minimap and zoom controls.
- **Log Viewer** (`LogViewer.tsx`): the graph's runs, paged, with status, event, time, duration and steps, beside the shown run's steps in order. A step expands to its inputs and outputs (`JsonView`); it can filter to the selected node; and the run's error and outputs are shown. Selecting a step selects its node.
- **Details** (`DetailsPanel.tsx`):
  - with nothing selected, the graph and its problems;
  - **Node**: description, id, type, category, source, config, and its pins with type chips;
  - **Handle**: node, handle, direction and type;
  - **Handle data**: a run picker, then the pin's captured value (a cut-short value shows its preview), or whether an exec pin was taken.
  - Selecting a node the shown run never reaches switches to the newest run that does. On user message and On reply runs reach different halves of Personal.

**Verification of slice 7**
- **Go tests**:
  - `cognition` (validation);
  - `cognition/engine`: linear flow, pure nodes pulled once, Branch, failures recorded on their node, the step limit, an unknown graph or event, retention, and big integers captured as strings;
  - `chronicle/nodes`: Personal is valid and behaves as the steps do;
  - the whole suite passes through `scripts/build.sh --test`.
- **Against a stub Gemini** (as in slices 5 and 6): two chat rounds gave four runs of Personal.
  - Their traces showed each node, the Branch taken, per-node usage and cost (stage 1 at 120→17 tokens), and the first round's memory update in the second round's items.
  - Without a reachable model, stage 1 fell back to the candidates' order and the run still succeeded.
- **In the browser** (Playwright, light and a dark preference): the Explorer, the canvas with overlays and minimap, node and handle selection, Handle Data following the node to a run that reached it, and the Log Viewer with a step's values.

### Decisions to review after seeing it
1. Chat runs Personal **from the `chronicle` provider and follower**, two runs per round, rather than `Send` running a mode graph.
2. Built-in graphs are **code** (the `cognition.graph` point, embedded JSON), not seeded rows. There is no `CognitionGraph` table until drafts exist.
3. `NodeType` is a struct with function fields, and data types are a contribution point (`cognition.type`) with their colours.
4. Values pass as Go values, converted through JSON only when types differ. Traces cut values over 256 KB to a preview rather than writing blob files.
5. Traces are written when the run ends, not as it goes.
6. Edges connect only equal types or `any`; implicit conversions wait.
7. The canvas is read-only and its layout is the graph's stored positions; Handle Data follows the selected node to a run that reached it.

## Built in slice 8
- **The `Agent` node** (`llm.agent`, category LLM, `tools/nodes`), the tool loop as a node ([Tools.md › Built in slice 8](Tools.md#built-in-slice-8)).
  - Config: `level` (the default level when empty) and `tools` (names). Inputs: `prompt`, and `system` (optional). Outputs: `reply`, and `calls` (each call's name, status, summary and error).
  - It runs unattended, so it has no client tools and cannot ask. With no tools listed it offers every tool that does not ask; a write listed by name is offered and denied, so the trace says why.
  - Its model calls and tool calls carry the run's `RunID` and `NodeID`; `ToolCall` records do too.
  - No built-in graph uses it yet, and it cannot be placed until editing arrives.
