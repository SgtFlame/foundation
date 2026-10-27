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

This document has two parts: the **Cognition Engine** (backend) and the **Cognition Editor** (frontend, React Flow).

Status: **skeleton built in slice 7**: the engine with linear exec flow and pure nodes, Chronicle's steps as nodes, *Personal* as a built-in graph that chat runs, and the Cognition Editor with a read-only canvas, the Log Viewer and the details panel over real traces. See [Built in slice 7](#built-in-slice-7). Slice 6 built seam 3, Chronicle's pipeline as Go step functions (`backend/features/chronicle/steps`), which slice 7 wrapped as nodes.

## Part 1: the Cognition Engine (`backend/features/cognition`)

### Graphs
- A **graph** has:
  - a name and description;
  - a **signature**: typed inputs and outputs, so a graph can be called as a node from other graphs;
  - **variables**: per-run state;
  - **nodes**: `{ id, type, position, config }`;
  - **edges**: `{ from: node.pin, to: node.pin }`.
- **Storage**:
  - `CognitionGraph`: name, kind (`mode` | `agent` | `function` | `summarizer`), owner and scope (see [MultiTenant.md](MultiTenant.md)).
  - `CognitionGraphVersion`: an immutable JSON snapshot with a version number. A **draft** is edited in place. **Publishing** freezes a version, and modes and callers always reference a published version, so editing never breaks a running chat.
- **Built-in graphs**:
  - *Personal* (the Chronicle pipeline) and *Document chat* are seeded as read-only built-ins.
  - "Duplicate" makes an editable copy.
  - Export and import use JSON.

### Pins and types
There are two kinds of pin, as in Blueprints:
- **Exec pins** (white arrows) order side effects. A node with exec pins runs when the flow reaches it.
- **Data pins** (coloured circles) carry typed values. A node with only data pins is **pure**, such as the *Prompt* node. Pure nodes are evaluated lazily when a downstream pin pulls them, and memoized once per execution context, which means once per loop iteration.

**Data types and their colours**
- `string`, `number`, `bool`
- `json`, carrying an optional JSON Schema
- `ContextBlock` (magenta): text plus metadata (source, citations, token count)
- `Message[]`
- `Segment[]`, from the retrieval engine
- `EntryRef` and `Entry[]`, for Chronicle
- `StreamRef` (blue): a Nexus stream
- `ToolResult`
- `List<T>`
- `any`, for generic nodes

**Rules for edges**
- Edges type-check while the user drags them.
- Safe implicit conversions are allowed and shown on the edge, for example `ContextBlock → string` and `Entry → ContextBlock`.
- Generic `any` pins resolve their type from the first connection.

### Node types: an extension point
Node types are contributed on the backend point `cognition.Node` ([Contributors.md](Contributors.md)):

```go
type NodeType interface {
    ID() string                       // "llm.complete", "nexus.getStream", …
    Meta() NodeMeta                   // title, category, description, colour, config schema
    Pins(config json.RawMessage) Pins // pins may depend on config, e.g. a Prompt's template variables
    Run(ctx *Exec, in Inputs) (Outputs, ExecPin, error) // pure nodes return no ExecPin
}
```

- **Config**: `config` uses the same schema language as [Configuration.md](Configuration.md), so the editor's details panel renders node settings with the Settings form renderer.
- **Pins**: `Pins()` is also served to the editor (`GET /cognition/node-types`), so the palette and the validator use the same definitions the engine uses.

**Built-in node library.** These are categories with examples, not a final list.

| Category | Nodes |
|---|---|
| **Events** | `Start` (manual run), `On user message` (chat modes), `On Nexus change`, `On schedule`; `End` / `Return` (graph outputs) |
| **Flow** | `Branch`, `Sequence`, `For each` (body + completed), `Loop` (max iterations), `Parallel` (fork/join), `Gate`, `Call graph` (sub-graph/agent), `Wait for user` (confirmation or input) |
| **Prompts** | `Prompt` (pure): renders a template from the prompt library. **Its template variables become input pins automatically.** |
| **LLM** | `Complete` (level, temperature, …), `Stream reply` (deltas go to the chat), `Structured` (a JSON Schema whose properties become typed output pins), `Agent` (a tool-calling loop; see [Tools.md](Tools.md)) |
| **Retrieval** | `Retrieve context` (the engine in [ContextRetrieval.md](ContextRetrieval.md), with its axes as input pins), `Find in volumes` |
| **Chronicle** | `Window`, `Active entries`, `Match context` (stage 1), `Get/Create/Update entry`, `Link entries`, `Record round` |
| **Nexus** | `Get stream`, `Set stream`, `Split stream` (into components, e.g. by heading), `Merge`, `Reconcile` (the multi-agent iteration node, below) |
| **Tools** | every registered tool also appears as a node ([Tools.md](Tools.md)) |
| **Data** | `Get/Set variable`, `Template string`, `JSON path`, `Concat context`, `Count tokens`, `Truncate to budget` |
| **Chat output** | `Emit citations`, `Emit entries`, `Emit status` (shown in the transcript as "thinking…" steps) |
| **Debug** | `Log`, `Assert`, and a breakpoint on any node through the editor |

### Prompt templates
- **Library**: a prompt library table `PromptTemplate`, holding name, description, role (`system` | `user`), template, version and scope.
- **Template language**: Go `text/template` (`{{ if .IsFeatureStream }}`), with:
  - a small set of safe helper functions (`join`, `indent`, `json`, `truncate`, `default`);
  - domain helpers that node authors register, such as `GetFeature`.
  - Templates cannot do I/O. Data comes in through pins, so the trace shows every input.
- **Variables → pins**: the variables a template references (`.IsFeatureStream`, `.FeatureListIn`, …) are found by parsing it, and they become the `Prompt` node's input pins.
- **Prompt Editor**: renders a live preview from the captured inputs of a chosen past run.

### Nexus
The **Nexus** is the Cognition component where agents collaborate: a shared, structured store that every agent in a run can read, write and reconcile against.
- **Streams**: a Nexus holds named **streams** (*Product Owner*, *Data Model*, *UI/UX*, *Features/…*). Each stream is a markdown document split into **components**, which are the sections under its headings.
- **Proposal: streams are Volume files.** Each Nexus is a folder in a Volume, and each stream is a markdown file in it. This gives four things for free:
  - humans can read and edit streams in the Markdown Editor, collaboratively;
  - the index makes streams retrievable;
  - file events trigger `On Nexus change`;
  - nothing new is needed to store them.
  - Writes from the engine go through the collab room when it is open, so they merge with live edits, or else as an atomic file write.
  - A `StreamRevision` table (run, node, iteration, hash, diff) records which agent wrote what, and when.
- **Immutability rules**: per stream or component, such as "section 4 is append-only" on a product owner's requirements. Prompts state them, and the `Set stream` node enforces them.
- **Reconcile**: one node that runs N agent streams against each other for up to M iterations. It has these inputs:
  - the system prompt;
  - the streams;
  - one prompt per agent.

  Each iteration, every agent reads the others' current streams and proposes changes. The node stops at a fixed point (no changes) or when it reaches the iteration limit. The editor shows `Streams` and `Iterations` as inline +/- controls.

### Execution
- **Runs**:
  - **Triggers**: a run starts from a chat message, the editor, a schedule or a Nexus change.
  - **Storage**: `CognitionRun` holds the graph version, trigger, status, start/end, totals (tokens, cost via [Usage.md](Usage.md)) and the conversation/round references.
  - **Concurrency**: each run executes in its own goroutine, and a `Parallel` node fans out with `errgroup`.
- **Semantics**:
  - Exec flow is followed from the triggering event node.
  - Pure inputs are pulled and memoized per **execution context**.
  - The context is a stack of loop frames, such as `forEach:n5[2] / loop:n9[1]`, so the same node in a different iteration is evaluated again.
- **Limits**, configured per graph with defaults in `cognition` configuration:
  - max steps;
  - max wall time;
  - max tokens and max cost per run, enforced through usage records as they accrue;
  - max loop iterations.
  - Exceeding a limit fails the run with a clear error on the offending node.
- **Trace**: a `RunStep` per node execution holds:
  - the node id;
  - the execution-context path (loop iteration);
  - the inputs and outputs (captured pin values; large values go to a blob file, keyed by hash);
  - the exec pin taken;
  - the duration and the usage-record references;
  - any error.

  The trace is what the Log Viewer and the Handle Data panel show. Its retention is configurable.
- **Debugging**:
  - Modes: **Run**, **Debug** (stops at breakpoints) and **Single-step** (step over, step into a sub-graph, step out, continue).
  - While paused, every captured pin value can be inspected, and **edited before continuing**.
  - Progress streams to the editor over SSE: `step`, `paused` and `done` events.
- **Replay**: re-run a graph or a single node using the captured inputs of a past run. **Pinned LLM outputs** from the trace stand in for model calls, so changing a downstream prompt or branch does not pay for the upstream calls again. This keeps iterating on a late prompt fast and cheap.
- **Validation** runs before publishing and before each run:
  - type mismatches;
  - required inputs left unconnected;
  - cycles among pure nodes;
  - unreachable exec paths;
  - a missing event node.

### How chat uses it
- **Modes**: `chat.modes` ([Configuration.md](Configuration.md)) points each mode at a published `mode` graph.
- **`Send`**: Chronicle's `Send` becomes "run the conversation's mode graph with the `On user message` event". Its SSE events (deltas, entries, sources, usage) come from the graph's `Stream reply` / `Emit …` nodes.
- **Context settings**: the ChatContextSettingsContributors' values ([ChatCompletion.md](ChatCompletion.md)) are passed as graph inputs. A mode graph declares which ones it uses, and the composer hides the others.
- **Agents**: an agent is a published `agent` graph with a persona prompt, allowed tools and a level, callable through `Call graph` and selectable as a chat mode.

### Services and REST
- **`cognitionservice`**:
  - graphs: `ListGraphs`, `GetGraph`, `SaveDraft` (409 on a stale revision), `Publish`, `Validate`;
  - runs: `Run` (async), `Step`/`Continue`/`Pause`/`Cancel`, `ListRuns`, `GetTrace`, `Replay`;
  - `NodeTypes`.
- **Other services**: `promptservice` (the prompt library) and `nexusservice` (Nexuses, streams, revisions).
- **REST and client**: under `/api/v1/cognition`, with `src/services/Cognition.ts`, `Prompts.ts` and `Nexus.ts`.

## Part 2: the Cognition Editor (`frontend/axiom/src/editors/cognition/`)

### Layout
- **Opening**:
  - The **Cognition** sidebar entry (AgentOS) opens a `CognitionObject` editor per graph.
  - The **Explorer** side view lists graphs (by kind), prompts and Nexuses.
  - The **Palette** side view lists node types by category, with search, and supports dragging nodes onto the canvas.
- **The editor** is a nested flexlayout with these tabs:
  - **Logic Editor**: the React Flow canvas (`@xyflow/react` is already a dependency).
  - **Log Viewer**: the run trace as a tree of steps, nested by loop iteration and sub-graph. It filters by node, shows inputs and outputs, and syncs selection with the canvas.
  - **Prompt Editor** tabs, opened from a Prompt node or from the Explorer. Monaco with Go-template highlighting, variable autocomplete from the node's pins, and the live preview against a chosen run.
  - **Nexus** tab: the streams as a list with their revisions and a diff, and a link to open a stream in the Markdown Editor.
- **The right details panel**:
  - **Node**: the config form (generic schema renderer, or a contributed custom view), description and pins.
  - **Handle**: node id, handle id, direction, data type and description.
  - **Handle Data (Debugging)**: a run picker, then an **iteration picker** when the node ran inside loops (one picker per loop frame), then the captured value. The value is rendered according to its type: markdown for context blocks, a JSON tree, or a table for lists.

### Canvas
- **Pins and edges**: typed, coloured pins. Exec edges are white and data edges take their type's colour.
  - Dragging from a pin opens the quick-add menu filtered to compatible nodes, as Unreal does.
  - Incompatible pins dim while dragging.
- **Taming the wiring**: large graphs turn into wiring tangles, so the editor has these tools:
  - **reroute** nodes;
  - **comment frames** (groups with a title and colour, collapsible);
  - **collapse to sub-graph** (select nodes, and they become a `function` graph with an inferred signature);
  - **variables** in place of long wires;
  - align and distribute.
- **Run overlays**:
  - the executing node glows;
  - executed edges animate;
  - each node shows a badge with its execution count, duration, tokens and cost;
  - failed nodes are outlined in red with the error;
  - breakpoints show as red dots.
- **Toolbar**: Run, Debug, Step, Continue, Stop, Validate, Publish, and a version picker with a diff against the published version.
- **Editing**:
  - **Undo/redo**, copy and paste (also between graphs), multi-select, minimap and zoom.
  - **Saving**: drafts autosave with a revision, so there is no explicit save. Old run traces expire under the retention policy or are deleted per run.
- **Node views**: node bodies are rendered from `Meta()` and `Pins()`. A frontend contribution `cognition.nodeView` can supply a custom body, such as Reconcile's +/- controls or a Prompt node's template selector.

## Relation to other plans
- [Chronicle.md](Chronicle.md): the `Send` pipeline becomes the built-in *Personal* graph. Custom-type summarizing modes are `summarizer` graphs.
- [ContextRetrieval.md](ContextRetrieval.md): retrieval is a node, and its axes are graph inputs.
- [Tools.md](Tools.md): tools are nodes, and the `Agent` node runs the tool loop.
- [Usage.md](Usage.md): usage records carry `RunID` and `NodeID`, so cost is visible per run and per node.
- [MultiTenant.md](MultiTenant.md): graphs, prompts and Nexuses are scoped.

## Open questions
1. **Nexus storage**: the proposal is streams as markdown files in a Volume. Or should they be database rows only? *Decision*:
2. **Collaborative graph editing**: should drafts be shared Yjs documents, like markdown, or single-editor with revision checks (409)? The proposal is single-editor for v1. *Decision*: **shared Yjs documents**, like markdown: a draft is a collab room that several people edit at once, relayed by the collab server. Publishing snapshots the room into an immutable `CognitionGraphVersion`. Decided before slice 7, which builds the read-only canvas; the room itself arrives with the editing pass.
3. **Background runs**: can graphs run without a chat (schedules, Nexus triggers), and should they be durable across server restarts? A durable job queue would revive the NATS question. *Decision*:
4. **Template helpers**: which domain functions (`GetFeature`, …) are needed, and do node authors register them per graph or globally? *Decision*: **globally**. The safe helpers (`join`, `indent`, `json`, `truncate`, `default`) form one global set, and domain helpers such as `GetFeature` wait until a graph needs one. Decided before slice 7.
5. **Trace retention and size**: keep the full pin values for N days, or only for the last N runs per graph? *Decision*: **both**: a trace is kept while it is among the last N runs of its graph *or* younger than N days, both configurable in the `cognition` section. Decided before slice 7.

## Built in slice 7
**Decided before building** (Open questions 2, 4 and 5 above): drafts will be **shared Yjs documents**; template helpers are **global**, with domain helpers deferred; a trace is kept while it is among its graph's **last N runs or younger than N days**.

**The engine** (`backend/features/cognition`)
- **Model** (`cognition`): `Graph` (`id`, `name`, `description`, `kind`, `version`, `nodes`, `edges`), `Node` (`id`, `type`, `title?`, `position`, `config?`), `Edge` (`from`/`to` as `{node, pin}`). This JSON is the graph's document, so the REST layer sends it as it is.
- **Three extension points**, declared in `serve.go`:
  - `cognition.node` (`cognition.Nodes`): a `NodeType` is a **struct with function fields**, like the other contributions, rather than the interface sketched above. It has `Title`, `Category`, `Description`, `Color?`, `Event`, `Config` (a schema), `Pins` (or `PinsFor(config)` when pins follow the config, as Return's do) and `Run(x *Exec, in Values) (Values, execPin, error)`.
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
  - Drafts, publish, validate-on-save and running from the editor are not built.

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

**The Cognition Editor** (`frontend/axiom/src/editors/cognition/`)
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
- **Not built**: the iteration picker (there are no loops yet), the Palette side view, the Prompt Editor, the Nexus tab, editing, the toolbar (Run, Debug, Step, Validate, Publish, the version picker), `cognition.nodeView` custom node bodies, and links from a chat round to its runs.

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
- **Not verified**: a real model, and how the editor feels for larger graphs. That is the ★ review.

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
- **Not yet**: every tool as its own node, tool calls in the Log Viewer, and `Wait for user` on the confirmation mechanism.
