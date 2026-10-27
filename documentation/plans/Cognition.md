# Cognition: what remains

The Cognition skeleton is built: the engine with linear exec flow and pure nodes, traces, *Personal* as a built-in graph that chat runs, the `Agent` node, and the read-only Cognition Editor with the Log Viewer and Details; see [Cognition.md](../Cognition.md). This plan is the full design, which the later passes build. Where the skeleton already differs from it (node types as structs, built-in graphs as a `cognition.graph` point, data types as `cognition.type`, values as Go values, previews in place of blob files), the documentation records why, and the design below is read with those changes.

Cognition makes chat logic data: a graph designed visually, like Unreal Engine Blueprints, and run by a deterministic engine. The same graphs define chat **modes**, **agents**, **multi-agent collaboration** through the shared **Nexus**, and Chronicle's custom-type **summarizing modes** ([Chronicle.md](Chronicle.md)).

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

## TODO
### Open questions
1. **Nexus storage**: the proposal is streams as markdown files in a Volume (see [Nexus](#nexus)). Or should they be database rows only? This must be answered **before** the Nexus slice (slice 9, skipped for now: its plan is not fleshed out yet). *Decision*:
3. **Background runs**: can graphs run without a chat (schedules, Nexus triggers), and should they be durable across server restarts? A durable job queue would revive the NATS question. *Decision*:

(Questions 2, 4 and 5 are decided; see [Cognition.md › Decisions](../Cognition.md#decisions).)

### Review after seeing it ★
- **The editor's look and feel** (slice 7), with a real model and with use. Runs were made against a stub of the Gemini API, so traces show canned stage 1 choices and memory updates. It was seen in a headless browser, light and dark, but not yet judged by a person, and not tried on a graph larger than Personal. Chat for a while with a key set, then open AgentOS › Cognition (or the Cognition side view) and review.
- **The decisions made while building**, in [Cognition.md › Decisions to review after seeing it](../Cognition.md#decisions-to-review-after-seeing-it).

### Not built yet
- **The Nexus slice** (slice 9): streams, the `Reconcile` node, the Nexus tab and `nexusservice`, with the `nexus.read`/`nexus.write` tools ([Tools.md](Tools.md)). ★ how streams and reconciliation feel. Needs question 1 first.
- **Editing passes**:
  - editing as shared Yjs drafts (question 2's decision), publishing, and a graph table (`CognitionGraph`, `CognitionGraphVersion`); duplicate, export and import;
  - the Prompt Editor and the prompt library (`PromptTemplate`, `promptservice`), with the global helpers of question 4;
  - Debug and Single-step, breakpoints, and editing captured values while paused;
  - loops (`For each`, `Loop`, `Parallel`) and the iteration picker;
  - Replay with pinned LLM outputs.
- **`Send` as a mode graph**: `chat.modes`, the `Stream reply` and `Emit …` nodes ([ChatCompletion.md](ChatCompletion.md)).
- **Engine**: the run token and cost limits (through usage records); implicit conversions; blob files for large values; live traces while running (`step`, `paused`, `done` over SSE); running from the editor (`Run`, async); `Call graph`, `Wait for user` (on the tool confirmation mechanism), the rest of the node library, and every tool as a node.
- **Editor**: the Palette side view, `cognition.nodeView` custom node bodies, the toolbar (Run, Debug, Step, Continue, Stop, Validate, Publish, the version picker), editing tools (reroute, comment frames, collapse to sub-graph, undo/redo, copy and paste), tool calls in the Log Viewer, and a link from a chat round to its runs.
- **Logging**: each `RunStep` logging a summary record with `trace` = the run ID, so runs show in the Log Explorer ([Logging.md](Logging.md)).
- **Access**: `graph.execute` and `graph.publish`, shares on graphs and Nexuses, and narrowing a run to the resources a graph lists ([RBAC.md](RBAC.md)).
- Refusing client tools in graphs that run in the background ([WorkbenchContext.md](WorkbenchContext.md)).
