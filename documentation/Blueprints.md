# Blueprints: applications' logic as Cognition graphs

An application's handlers, commands, actions and queries are **blueprints**: Cognition graphs of the kinds below, drawn rather than coded, edited by several people at once and published to take effect at once. They run on the Cognition Engine ([Cognition.md](Cognition.md)), the live core ([Live.md](Live.md)) and server-driven UI ([ServerUI.md](ServerUI.md)). Forge edits them in its Blueprint Editor (forge `documentation/BlueprintEditor.md`).

It was designed in [forge 02-Blueprints.md](../../forge/documentation/plans/02-Blueprints.md) and built as Forge's iteration 2, in two slices: Cognition's editing pass (slice 14, in [Cognition.md](Cognition.md#built-in-slice-14)) and the blueprints themselves (slice 15, here).

## Built in slice 15

### Decisions
Answered before building, in [forge 02-Blueprints.md › TODO](../../forge/documentation/plans/02-Blueprints.md#todo):
- **The expression language** (question 2): `expr-lang/expr`.
- **Hot swapping** (question 3): a republished handler runs from each open session's next event.
- **Tracing** (question 4): a blueprint's run is traced in full while its graph is open in an editor, and otherwise only when it fails. The other runs are counted.
- **Commands as graphs** (question 6): every entity will get generic record commands (forge 03-DataModel). `command` graphs are for writes that need their own checks.
- **`app.trigger`** (question 8): only the kind and its events, until forge 07-Email.

Decided while building:
- **A command or query graph's id follows from its spec**: `cmd-<command, - for .>` and `qry-<query, - for .>` (`Kind.IDFor`, checked by `bp.create` and `bp.setInfo`). The bus and the subscriptions find a tenant's graph by the type alone.
- **A handler's id in a tree is a graph's id**: when a page has no Go handler of that name, the session runs the published `ui.handler` graph with that id, from its On event.
- **Tree overlays**: a page drawn by a Go render function takes added nodes from commands, so a model can add a button before pages are prefabs (forge 04-Prefabs).
- **Test runs are dry runs**: in `bp.test`, a handler's host and an action's sender record the commands they would send, and a command graph's host records its events. Nothing is sent or committed.
- **A handler's failure names the node**: its message is public, `<graph> failed at <node>: <error>. Its run is in the graph's Log Viewer.`
- **Built-in graphs can be editable defaults** (`cognition.BuiltIn.Editable`), as Hello's are. A tenant's published version replaces them in that tenant.

### The kinds
| Kind | Starts on | Returns | Package |
|---|---|---|---|
| `ui.handler` | *On event* (`ui.onEvent`: the payload, and declared fields as pins) | view-state changes, client actions and the commands it sent | `ui/nodes` |
| `command` (pure) | *On command* (`live.onCommand`: args, state, target, declared args fields) | the events to commit (*Emit event*) or problems (*Reject*) | `live/nodes` |
| `data.query` (pure) | *On query* (`live.onQuery`: params, declared fields) | Return's output `result` | `live/nodes` |
| `server.action` | *On call* | its declared outputs | `live/nodes` |
| `prefab.logic` | *On mount*, *On props changed*, *On query changed*, *On unmount* | (no runs until forge 04) | `ui/nodes` |
| `app.trigger` | *On event*, *On schedule*, *On webhook* | (no runs until forge 07) | `live/nodes` |

**Command graphs** (`livenodes.CommandFor`, the bus's new `Resolve`) are found when no feature registered the command.
- The spec `{command, title, description, args, area, targetType, projection, action, events}` makes it a `live.Command`.
- Its `Decide` runs the published graph with the args, the target's state and its id, under tight limits (200 steps, 2 s).
- The tool `live.command {command, target, args}` (new) sends any command, these included.

**Query graphs** (`livenodes.QueryFor`, the subscriptions' new `Resolve`) work the same way: the spec `{query, title, description, params, action}` makes it a `live.Query`.
- Each subscription runs the graph, then watches every key its *Read projection* nodes read, and the graph's published version. On a change it runs the graph again and sends a `replace`.

**Handlers** (`uinodes.Runner`, `ui.HandlerRunner`, `uiimpl.WithGraphs`) run in the session: the event's payload goes to *On event*, and the handle is the run's host. Limits: 500 steps and 3 s.

### Node families
- **Commands** (`live/nodes`):
  - *Send command* (config `command`, `target`; inputs target and args; exec out *OK* or *Rejected*, with `problems` and `message`). In a handler it sends as the session's user; elsewhere, through the bus as the run's principal.
  - *Emit event* and *Reject* (command graphs only).
- **Reading**: *Read projection* (config `projector`; key → value, exists) and *Run query* (a one-time read).
- **UI** (`ui/nodes`, for `ui.handler` and `prefab.logic`): *Get state*, *Set state*, *Get local* (the payload at a path); the effects *Toast*, *Open editor*, *Confirm* (the answer goes to another handler graph), *Download* and *Copy*.
- **Data** (`cognitionnodes`, every kind): *Expression*, *Make object*, *Get field*, *Set field*, *Map*, *Filter*, *Sort*, *Group*, *Reduce*, *Template string*, *Compare*, *Math*, *Format*, *Parse*, *Now* and *New id*.
- **Expression** compiles an `expr` expression once per source. The names it reads become its input pins (`cognitionnodes.Identifiers`: not call names, not `let` names), and an input named like a builtin shadows it. It runs sandboxed, with no I/O.

Every node type has a description, an example and its pins described. `hello_test` checks that every registered type does.

### Tree overlays (`ui/Overlay.go`)
- **Commands**: `ui.insertNode {parent, index?, node}` and `ui.removeNode {key}`, on the target `pageTree.<page id with - for />` (`def` stream). They need the new action `ui.design` (class write). A node is validated against the catalog first.
- **Applying them**: every session with subscriptions holds its page's `ui.overlay` query. The root render adds the inserts under the node at `parent` (a key path joined by `/`), so a change renders the page again.

### The Hello app
`ui/hello` now has no Go handlers.
- **Its graphs** are embedded editable built-ins (`graphs/*.json`, owner `app:hello`):
  - the handlers `hello-onIncrement`, `-onGreet`, `-onTab`, `-onConnect` (branching on *Can connect?*: a node joined to itself is a toast), `-onDisconnect`, `-onDelete` (For each edge), `-onMove`, `-onSelect` and `-onOpenItem`;
  - the command graph `cmd-hello-increment` and the query graph `qry-hello-counter`.
- **Its tree** names them.
- **Still Go**: its render functions and `OnOpen` (until prefabs, forge 04), and the shared graph's commands, query and projectors (until records, forge 03).

### Verification of slice 15
- **Go tests** (`ui/hello`, on an embedded NATS with the engine, drafts and nodes):
  - two users see each other's increments through the graph handler, command and query;
  - `hello.increment` by 2, acks, duplicates and rejections;
  - *On connect*'s branch toasting a node joined to itself;
  - presence, and view state surviving its connection.
- **The plan's acceptance, through commands alone** (what the chat's tools send):
  - `bp.*` built a `hello.reset` command graph and an *On reset* handler, and published both;
  - `ui.insertNode` added a Reset button to the open page;
  - clicking it toasted and set the counter to 0;
  - republished with another message, the handler changed at the next click, in the same tab;
  - a failing handler reported its graph and node.
- **Also tested**: every Hello graph resolves with no problems, and every node type has its metadata. `cognitionnodes` covers expression identifiers and builtin shadowing. The whole suite passes.
- **In headless Chromium** (Axiom against Forge's `serve`, two accounts):
  - Ada wired a *Toast* after *Send command* in *On increment*, configured it and published v2;
  - Bob's open Hello tab showed "Incremented by a blueprint" on his next click, with no reload;
  - the Test panel ran the draft.
- **Not seen**: a real model driving the tools (the acceptance was run through the same commands), and handler latency judged by a person.

### Decisions to review after seeing it
1. Handler ids in trees are graph ids, and a page's own Go handler of the same name wins.
2. Tree overlays as the bridge to prefabs: added nodes go under an existing node by key path, and are kept per page, not per app version.
3. *Send command* reports `problems` and `message` but no sequence: `ui.Handle.Command` does not return one.
4. *Confirm* does not wait for the answer; it names the handler graph the answer runs.
5. Command and query graphs run under tight limits (200 steps, 2 s; handlers 500, 3 s).
6. Hello's built-in graphs are editable defaults rather than seeded drafts, so a tenant that never edits them has no rows.

## Changed in slice 23
For the Hello task list (forge 03i, [Records.md › Built in slice 23](Records.md#built-in-slice-23)):
- **Record nodes** (`records/nodes`, category *Records*): *Create record*, *Update record*, *Delete record*, *Add to* and *Remove from* in `command` graphs, whose events commit with the command's as one batch; *Get record*, *Query* and *Aggregate* in every kind, which a `data.query` graph watches to run again; *On record changed*, an `app.trigger` event (no runs until forge 07). Elsewhere than a command graph, a write is *Send command* with `record.*`.
- ***Current user*** (`live.currentUser`): the run's user id as text.
- **A command graph's host** carries the command's `Decision`, so nodes can read and change other targets in its area.
- **Hello's task graphs** (built-in, editable, owner `app:hello`):
  - the handlers `hello-onOpenTasks`, `-onAddTask` (a title is needed; *Assign to me* uses *Current user*), `-onEditTask` (done sends `task.toggle`, other fields `record.update`), `-onFilterTasks` and `-onSortTasks` (they set the view state the rows query is bound to) and `-onClearDone`;
  - the command graphs `cmd-task-toggle` (*Update record* with the opposite of the task's `done`, so two toggles at once both count) and `cmd-task-clearDone` (*Query* the done tasks, then *For each* *Delete record*, up to 50, one batch);
  - the query graph `qry-hello-openTasks` (*Aggregate* count where not done → `{open, label}`).
