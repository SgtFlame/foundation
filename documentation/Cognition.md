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

The Cognition Engine (backend, `backend/features/cognition`) runs graphs; the Cognition Editor (frontend, React Flow; in Forge since slice 44, `forge/frontend/axiom/src/editors/cognition/`) edits them. Slice 7 built the **skeleton**: the engine with linear exec flow and pure nodes, Chronicle's steps as nodes, *Personal* as a built-in graph that chat runs, and the read-only Cognition Editor with the Log Viewer and the details panel over real traces. Slice 8 added the `Agent` node. Slice 14 built the **editing pass**: graph kinds, drafts that several people edit at once by commands, publishing, the Palette, variables, loops, `Call graph`, implicit conversions, undo and copy and paste. Slice 15 built the blueprint kinds on it ([Blueprints.md](Blueprints.md)). The rest of the design (prompt templates, the Nexus, debugging, Replay, chat modes) and what remains are in [plans/Cognition.md](plans/Cognition.md).

## Decisions
- **Collaborative graph editing** (Cognition 2): drafts are **changed by commands** (`bp.*`) on the live core, like every structured object, and several people edit one at once through its live query. Decided before slice 7 as shared Yjs documents, and **revised before slice 14** (forge 02-Blueprints question 5, 2026-11-01), so that every graph, blueprint or chat mode, is edited one way; nothing had been built on Yjs drafts.
- **Graph kinds** (forge 02-Blueprints question 1): a `cognition.kind` point in Cognition proper, with `mode`, `agent`, `function` and `summarizer` registered on it like the blueprint kinds, so the Explorer groups by kind for both. Decided before slice 14.
- **Published versions** (forge 02-Blueprints question 7): **events and KV**, not the `CognitionGraph` and `CognitionGraphVersion` tables the plan drew. `bp.publish` commits a `published` event carrying the graph, and a projector keeps each graph's last published version. Decided before slice 14.
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

**The Cognition Editor** (then `frontend/common/src/editors/cognition/`, in Forge since slice 44)
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

## Built in slice 14
Cognition's editing pass, built for forge 02-Blueprints ([BuildHistory.md](BuildHistory.md)). Its blueprint kinds came in slice 15 ([Blueprints.md](Blueprints.md)).

**The model** (`features/cognition`)
- **Graph kinds**: the `cognition.kind` point (`cognition.Kinds`). A `Kind` has a title, a description, what it returns, the event node types that may start it, `Pure` (it refuses nodes with `Effects`), a `Spec` schema, `IDFor` (the graph id its spec requires) and `Host` (what a test run acts on). `cognitionnodes` registers `mode` (events *On user message* and *On reply*), `agent` and `function` (*On call*) and `summarizer`.
- **What a node type carries now**: `Example`, `Kinds` (the kinds it may be used in; Chronicle's nodes allow `mode` and `summarizer`), `Effects`, `Volatile` (a pure node evaluated at every pull, such as Get variable) and `Normalize` (completes a config as a draft takes it: Call graph copies the called graph's signature).
- **Metadata is required**: `cognition.MetaProblems` lists what a type lacks (a description, an example, a description per fixed data pin), and validation refuses a node of such a type. Chat's, Chronicle's and the Agent's types were completed.
- **A graph** now also has `owner` (e.g. `app:hello`), `spec` (what its kind needs) and `variables` (`{id, name, type, default, description}`).
- **`cognition.Catalog`** (node types, kinds and conversions, from `CatalogOf(registry)`) resolves a graph: as before, plus the kind's rules, metadata, and the conversion each edge makes (`Resolved.Conversions`). `cognition.Resolve(g, types)` is kept for callers with node types only. `SignatureOf` is a graph's inputs (its On call's outputs) and outputs (its Returns' inputs).
- **Implicit conversions**: the `cognition.conversion` point. The core ones are number, bool and json to string, and string, number and bool to json. The editor labels a converting edge.
- **Built-in graphs** may be `Editable`. The first command on one in a tenant starts the tenant's draft from it, and the tenant's published version then takes its place. Others, such as Personal, are read-only, and are changed by duplicating them.

**The engine** (`cognition/engine`)
- **Execution contexts**: a flow node runs the chains leaving it through `Exec.Flow(pin, frame)`, and a loop's iteration is a frame, such as `forEach:n5[2]`. Pure nodes are memoized per context, and a step records its context.
- **Exec**: `Exec.Set` publishes outputs before a node returns (a loop's item), and `Exec.Pull` evaluates an input again in a frame (a loop's condition). `Exec.Call` runs another graph, and `Exec.Host` and `Exec.Vars` are the run's host and variables.
- **New nodes** (`cognitionnodes`):
  - Flow: *Sequence* (`then1…thenN`), *For each* (body, completed, item, index), *Loop* (a *While* input pulled before each iteration, a max), *On call* (declared inputs) and *Call graph* (Effects; its pins are the callee's signature as it was when set);
  - Data: *Get variable*, *Set variable* and *Reroute*;
  - Notes: *Comment*, a frame with a title, colour and size that never runs.
- **Graphs to run** come from a `GraphSource` (`SetSource`): the tenant's published version, else the built-in. A run may also be given a `Graph` (a draft under test).
- **Limits**: `maxLoopIterations` (1000) and `maxCallDepth` (16) join the `cognition` section, and `RunArgs.Limits` narrows a kind's runs further.
- **Tracing**: `RunArgs.Trace` is `TraceAll` or `TraceFailures`. The latter stores a run only when it fails, and otherwise counts it (`Engine.Untraced`, since the server started).

**Drafts** (`cognition/drafts`): a graph's draft is an aggregate on the `def` stream (target type `graph`).
- **Projectors**: `cognition.graph` folds the draft (`{graph, rev, published, publishedRev, updatedBy, updatedAt}`). `cognition.published` keeps the last published graph, which the engine runs (`drafts.Source`).
- **Commands**, with action `cognition.write` (`bp.publish` needs `cognition.publish`; both new, class write). Each checks against the catalog and refuses with problems a model can act on (a `fix`):
  - `bp.create {kind, name, description?, owner?, spec?, from?}` (a kind with one event starts with that event node; `from` duplicates);
  - `bp.import {graph}`;
  - `bp.addNode {type, id?, title?, config?, position?}` (placed right of the others when no position is given);
  - `bp.setConfig {node, config?, title?}`;
  - `bp.moveNodes {moves}`;
  - `bp.connect {from, to}` (it replaces the edge into a data input, or out of an exec output, already there);
  - `bp.disconnect`;
  - `bp.remove {nodes?, edges?}`;
  - `bp.paste {nodes, edges}`;
  - `bp.addVariable`, `bp.removeVariable` and `bp.setInfo {name?, description?, owner?, spec?}`;
  - `bp.publish {rev}`, refused when the draft has moved past the rev its sender saw, or has errors;
  - `bp.delete`.
- **Command reports**: `live.Command.Report` (new) lets a command's tool return more than its sequence. A `bp.*` tool returns the draft's rev and problems once its commit is projected.
- **Queries**: `cognition.graph {id}` is the draft resolved (`graph`, `pins`, `problems`, `conversions`, `rev`, `published`, `builtIn`, `readOnly`, `untraced`); a built-in no one changed is served as built in. `cognition.graphs {kind?}` is every graph by key, built-ins included.
- **Testing**: the `bp.test` call and tool runs a draft once with sample inputs, traced, with its kind's test host. The catalog tools are `bp.listKinds`, `bp.listNodeTypes {kind?, search?}` and `bp.describeNodeType`.
- `GET /cognition/node-types` also returns the kinds and conversions.

**The Cognition Editor** (`src/editors/cognition/`) edits drafts. `GraphEditor` (in `CognitionEditor.tsx`) is the editor without its workbench object, which Forge's Blueprint Editor reuses with tabs of its own (`extraTabs`).
- **Editing** (`editing.ts`, `useDraft`): the draft is the `cognition.graph` query. Each gesture is one or more `bp.*` commands, applied over the query's data at once until it reflects them (`applyOp`, which mirrors the server's fold).
- **Undo and redo** are this user's own stack of inverse commands (`inverse`, computed from the draft before the gesture), so each person undoes only their changes. A refused command shows in a toast. (Replaced in slice 39 by undo by request, below.)
- **The canvas** (`GraphCanvas.tsx`):
  - drag (sent on drop), connect (incompatible pins refuse the drop; a converting edge shows `from→to`), multi-select, Delete, Ctrl+C/V (the clipboard and an in-page copy; pasted nodes get new ids), Ctrl+Z / Ctrl+Shift+Z / Ctrl+Y, and dropping from the Palette or Variables;
  - comment frames are resizable boxes behind the nodes, and reroutes are dots;
  - other people's selections are rings in their colours, from presence in `graph.<id>` (`{selected}`).
- **Toolbar** (`Toolbar.tsx`): Undo, Redo, Delete, Validate (the problems, each selecting its node), Publish (the rev seen), the version (`v2 · changed`), *Duplicate* (`bp.create` from, then opens the copy), *Export* (JSON) and the others' avatars. A read-only built-in says so.
- **New tabs**: **Palette** (`PalettePanel.tsx`: the node types the kind allows, by category, with search; drag, or double-click to add) and **Variables** (`VariablesPanel.tsx`: add and remove, with Get and Set chips to drag).
- **Details** edits a node's title and config (JSON, Apply), and a graph's name, description and spec. It also shows the node type's example and the untraced runs.
- **The Explorer** follows `cognition.graphs` live, with *New graph* (kind and name) and *Import* (a JSON file).
- `loadCatalog`/`useCatalog` (`catalog.ts`) load the node types, kinds and conversions once.

### Verification of slice 14
- **Go tests**:
  - `cognition/engine`: For each running its body per item in its own context with variables, a number converted to text on an edge, Call graph, and trace-on-failure, besides the earlier tests;
  - `cognition/drafts` (on an embedded NATS): commands building a graph, a refused exec-to-data connection with its fix, a stale publish refused, the published version run by the engine, remove and paste, an editable built-in becoming the tenant's draft and then its published version, Call graph's signature, a copy, the graph list, and `bp.test`;
  - `cognitionnodes`: expression identifiers.

  The whole suite passes through `scripts/build.sh --test`.
- **In headless Chromium** (Axiom against Forge's `serve`, two accounts):
  - Ada and Bob had one draft open. Each saw the other's avatar, the nodes the other added, and Ada's selected node ringed in her colour.
  - Bob's Undo removed only his own node, and Delete removed nodes.
  - A drag joined two pins, and a config was applied from Details.
  - *Publish* stayed disabled while the draft had errors.
  - Personal opened read-only, with *Duplicate*.

### Decisions to review after seeing it
1. The Palette and Variables are tabs of the editor, not side views, so they follow the open graph's kind.
2. Undo is client-side, a stack of inverse commands per editor. An inverse that no longer applies (another author removed the node) is refused, and the entry is dropped. *Replaced in slice 39*: undo is by request from the operation log (see *Changed in slice 39* below).
3. Positions stay in the browser while a node is dragged and are sent once, on drop.
4. A node's config is edited as JSON with *Apply*, not through the schema form.
5. Only the last published version is kept in KV; older versions are in the event stream. There is no version picker or diff yet.
6. Call graph refuses pure kinds (it has `Effects`), even when the callee is pure.
7. A failed run of a draft under test is traced like any other, in the graph's Log Viewer.

## Changed in slice 39
Undo by request (forge 06b, [Apps.md › Built in slice 39](Apps.md#built-in-slice-39)) replaced the editor's client-side undo: `useDraft` sends a gesture's commands as one request (`live.gesture`), and *Undo* and *Redo* in the toolbar (and Ctrl+Z, Ctrl+Shift+Z, Ctrl+Y on the canvas) are `useUndo('graph:<id>')`, the caller's own requests on the graph from the operation log, so an undo survives a reload, reaches a change made in another of the user's windows, and leaves others' changes; a change someone else made since to the same node or edge refuses it, naming them, with *Undo the rest*. `inverse` in `editing.ts` is gone. `bp.restore` brings back a deleted graph, its draft and published version kept by the `cognition.trash` projection, so `bp.delete` (and the undo of `bp.create`) is undoable.

## Changed in slice 40
Forge's part 6d ([Apps.md](Apps.md#built-in-slice-40)): a node type may give `Examples` (minimal uses as JSON, `{type, config}`) and `Tags`. `cognition.ExamplesOf` gives its examples, else one made from its config schema's defaults; a config with required fields needs its own (`cognition.ConfigExample(type, config)`). `cognition.TagsOf` adds its category, source, kinds, `event` and `effects` to its own tags. `bp.describeNodeType` gives both, and `bp.listNodeTypes` searches the tags. Every node type's example is checked by `apps/tools`' `TestMetadata`.

## Built in slice 44
Forge's chat modes, part 6i ([ChatCompletion.md › Built in slice 44](ChatCompletion.md#built-in-slice-44)): a `mode` graph runs a whole chat round, and prompts and tools are built in graphs with Go templates.
- **Templates** (`cognition/tmpl`): Go `text/template` with `join`, `upper`, `lower`, `trim`, `indent`, `clip`, `oneLine`, `replace`, `split`, `contains`, `hasPrefix`, `hasSuffix`, `default`, `json`, `list`, `dict`, `tail`, `add`, `sub`, `now` and `setting "section" "path"` (the pipeline's value is each function's last argument: `{{.text | clip 600}}`); a missing key renders as nothing. `tmpl.Fields` lists the fields a template reads at its top (`.x` outside `range`/`with`, `$.x`), which become a node's input pins (`cognitionnodes.TemplatePins`, `TemplateData`). A node renders with `x.Render(text, data)`; `x.Setting(section, path)` reads a setting as the run's principal sees it (`ExecHooks.Setting`, from the engine's config).
- **Nodes**: *Template* (`text.template`; declared `inputs` add typed pins) and *Setting* (`data.setting`) in Data; *Ask model* (`llm.complete`: one call, `feature` and `level`, leaving by *Failed* with the error), and the Agent's optional *Tools* pin, in LLM; the tool set nodes in Tools (the `ToolSet` type, `[]tools.Spec`; [Tools.md › Built in slice 44](Tools.md#built-in-slice-44)); the chat nodes in Chat ([ChatCompletion.md](ChatCompletion.md#built-in-slice-44)).
- **The `mode` kind** starts only from `On user message` (`On reply` is gone) and has a spec, `{settings: [provider ids]}` (`cognition.ModeSpec`), the context settings the composer shows for it.
- **`engine.RunArgs.Wall`** replaces the section's time per run (a chat round's two hours), kept by the graphs it calls.
- **Personal** (`chronicle/nodes/personal.json`) is now the whole round and an editable built-in: Chronicle memory (Sequence then Branch on the Chronicles setting, stage 1, the chosen entries kept in a variable), *Gather context* from `workbench` and `rag`, the *Instructions* template over the `chat` section's prompt and the tools, *Add context*, *Stream reply*, then *Ask for a title* when untitled, and the memory update with *Memory changes* and *Emit followed chronicle*. The prompts of stage 1 and the memory update are in its nodes ([Chronicle.md › Built in slice 44](Chronicle.md#built-in-slice-44)).
- **The editor moved to Forge**: the Cognition Editor's client (`GraphEditor`, `GraphCanvas`, the Palette, Variables, Explorer, Log Viewer and details panel) is now `forge/frontend/axiom/src/editors/cognition/`, beside the Blueprint Editor that reuses it. Foundation's example app has no graph editor; its graphs (Personal among them) run as shipped, and Forge is where they are edited.
