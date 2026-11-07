# Server-driven UI

Applications are rendered mostly on the server. A page is a render function. Given one viewer's **UI session** (its view state) and the data of the queries it holds, the function returns a **component tree**. The browser draws the tree with registered renderers in three families: MUI, flexlayout and React Flow. Component events go to the server's handlers, which change view state and send commands. The server answers with **patches**, and pushes patches whenever a query the page holds changes. Everything travels over the live connection ([Live.md](Live.md)). An application may also declare **editor kinds**, pages that open as their own top-level workbench tab.

It was designed in [forge 01-ServerUI.md](../../forge/documentation/plans/01-ServerUI.md) and built as Forge's iteration 1. From [forge 02-Blueprints](../../forge/documentation/plans/02-Blueprints.md) on, handlers become blueprints; from [04-Prefabs](../../forge/documentation/plans/04-Prefabs.md) on, render functions become prefab templates.

## Built in slice 13

### Decisions
Answered before building ([forge 01-ServerUI.md › TODO](../../forge/documentation/plans/01-ServerUI.md#todo)):
- **Binding tracking**, not whole-subtree diffing (question 1).
  - Bound props (`state.*`, `query:<name>[.<path>]`) are resolved on the server. When one's source changes, only a `props` patch for that node is sent, with no rendering.
  - A render function reads view state and query data through `ui.Render`, which records what each `r.Region(key, fn)` read. A change renders again only the regions that read it, outermost first, and diffs each against its old subtree by key. Reads outside any region belong to the root.
- **Sessions in NATS KV** (question 2).
  - A session's record (id, user, tenant, page, params, view state, revision) lives in `ui_sessions_<tenant>`. Records last 10 minutes and are stored again every 3 minutes while open.
  - The browser picks the session id and keeps it in its tab, so a reload, a reconnect or a server restart resumes the view state.
  - The rendered tree and the region index are a cache in memory, rebuilt by rendering.
- **A user's own flexlayout arrangement** is kept per user and page (question 3), in the `ui.layouts` section at the user scope. Each entry is `{id: "<page>/<layout key path>", model}`, and the section is shown as Workbench › Application layouts.
- **No plain route** for pages outside the workbench (question 4).

Decided while building:
- **A stale event.** An event's frame names the key path of the node it came from. When the server no longer has that node, the event runs nothing and the browser gets the whole tree (`ui.tree` with `stale`). Otherwise the event runs even if patches crossed it on the wire, so a click while another user's change arrives is not lost. The plan judged staleness by revision.
- **Validation in the tree.** Every node is validated as it is rendered. A node with an unknown type or invalid props is replaced by a `ui.error` node, which the browser shows as an inline error box. A bound prop counts as present, but its value is not type-checked.
- **Registration needs metadata.** `ui.RegisterComponent` refuses a component without a title, description, category, props schema or example, and `serve` fails to start if a primitive does.
- **Events sent while offline** are queued by the browser's session and sent once it has reopened, so a click while the connection was down applies once.
- **Restorable tabs.** The workbench had not kept its tabs across a reload. A `workbench.editor` contribution may now set `restorable`; app editor kinds always are.
  - Their tabs, with their objects, are saved in this browser's storage for each tenant and user, together with the selected tab, and reopen after a reload.
  - Other editors are not restored, because their objects are not plain data.

### The tree
`ui.Node {type, key, props, bind, on, children, slots}` (`features/ui/Node.go`), built in Go with `ui.N(type, key).P(…).B(prop, path).E(event, handler, payload…).C(children…)`.
- `bind` reads a prop from `state.<path>`, `query:<name>[.<path>]` or `local.<path>`.
- `on` maps an event to `{handler, payload}`. String payload values `local.<path>` and `event.<field>` are filled in by the browser.

Patches (`ui.Patch`, `Diff.go`) address nodes by key path: `replace {path, node}`, `insert {path, index, node}`, `remove {path}`, `move {path, key, index}` and `props {path, props}` (null removes a prop). They are produced by `ui.Diff`, which matches children by key, and applied in order by the browser's `applyPatch` (`src/ui/types.ts`), which copies only the nodes on the path.

Client actions (`ui.Action`) that are built: `toast`, `openEditor`, `rename`, `copy`, `download` and `confirm` (its answer is posted to `handler` as `{confirmed}`). `navigate` and `focus` are logged as not supported yet.

### Backend
- **`features/ui`**:
  - the points `ui.component` (`ui.Component`: title, description, category, tags, props schema, events, slots, children, examples), `ui.app`, `ui.page` and `ui.editorKind`;
  - `ui.Page`: `Queries(params)` (named `QueryRef`s), `Subject(params)` (what its tab reports to the workbench), `OnOpen`, `Render(*ui.Render)` and `Handlers`;
  - `ui.Handle`: `State`, `Set`, `Command(type, target, args)` (decided on the bus as the session's user), `Toast`, `OpenEditor(kind, id, name, helpText, params)` and `Do(action)`;
  - `ui.Session`, the session runtime;
  - `ui.KVSessions`, the session store;
  - `ui.Validate` and `ui.ErrorNode`;
  - `ui.ContributeAccess`, which declares `ui.read`.
- **`ui/components`** (`uicomponents.Contribute`) registers the primitives:
  - **MUI**: `mui.stack`, `grid`, `box`, `card`, `divider`, `text`, `heading`, `markdown`, `icon`, `avatar`, `chip`, `badge`, `textField`, `number`, `select`, `checkbox`, `switch`, `date`, `autocomplete`, `button`, `iconButton`, `menu`, `list`, `table`, `tabs`, `dialog`, `drawer`, `progress`, `alert`, `tooltip` and `empty`;
  - **`layout.flex`**;
  - **`graph.canvas`**;
  - **`ui.error`**.

  Each example validates (`components_test.go`).
- **`services/uiservice`** has `Components` and `EditorKinds` (REST), and the Go-only `Open`, `Event` and `Close`.
  - A session open on another connection moves to the new one.
  - A stored record of another user is refused (`session_taken`).
  - A record is resumed only for the same page and params.
- **`rest/uihandlers`**:
  - `GET /api/v1/ui/components` and `GET /api/v1/ui/editor-kinds`;
  - `ContributeLive`, the `ui` live extension (`ui.open`, `ui.event`, `ui.close`). Opening runs on its own goroutine, because it waits up to 3 seconds for the page's queries' first snapshots. A connection's sessions close with it, and their records stay.
- **`ui/tools`**: `ui.listComponents`, `ui.describeComponent` and `ui.previewTree` (a tree's problems with their key paths).
- **`ui/settings`**: `ui.layouts`.
- **`ui/hello`**, the Hello app, written in Go as the plan asks:
  - **Data**: the aggregates `hello.counter` (command `hello.increment`, event `incremented`) and `hello.graph` (`hello.connect`, `hello.disconnect`, `hello.moveNode`), each with its projector and query, on the `app_hello` stream;
  - **Pages**: `hello/main` (a shared counter, a greeting kept in view state from a text field bound to local state, and docked sub-tabs with the shared graph and a list of items) and `hello/item`;
  - **Editor kinds**: `app:hello:main` and `app:hello:item`;
  - **Access**: the resource type `hello` (`read`, `write`).

### Browser
- **`src/ui/`**:
  - `UiSession.ts`: one session's tree, revision, title, subject, error and status. It also holds `LocalState`, the page's local state. `emit(path, on, data)` sends an event, and the session reopens with the same id after a reconnect.
  - `UiRenderer.tsx`: renders a tree with the `ui.renderer` contributions (`src/ui/points.ts`; a renderer gets `props`, `emit`, `handles`, `setLocal`, `children`, `renderChild(key)` and `slot(name)`). A node re-renders only when its subtree or the local paths it binds change. Unknown types and renderer failures show `ErrorBox`.
  - `renderers/mui/`: the MUI family. An input bound to local state keeps typing in the browser, and its change events still go to the server when the tree maps them. Icons are a fixed map of Material icons (`iconMap.ts`); other names fall back to the icon font.
  - `renderers/flexlayout/FlexLayout.tsx`: `layout.flex`. The model is rebuilt only when the set of tabs changes, and the server's selected tabs are applied. Selecting, closing and moving tabs are events. A user's own arrangement is saved to `ui.layouts` (debounced) and used while its tabs match the server's.
  - `renderers/graph/GraphCanvas.tsx`: `graph.canvas` on `@xyflow/react`. Positions stay in the browser while dragging and are sent on drag end. Node bodies are the children keyed by node id. Selections go to the server and into presence (`{selected}`), and other users' selections are outlined in their colour.
  - `AppHost.tsx`: the editor of every app kind.
    - **The tab**: the page's title, `Offline` / `Reconnecting…`, and `PresenceAvatars` of the others in `app_<appId>_<kind>.<id>`.
    - **Its contents**: the tree, an inline alert for a handler's error, and toasts.
    - **Workbench**: it reports the page's subject, and performs the client actions.
    - **Sessions**: they are shared by id across remounts, and close (forgetting their record when the tab was closed) a moment after the last unmount.
  - `AppEditorObject.ts`: kind `app:<appId>:<kind>`, with id, name, help text, params and the session id.
  - `editorKinds.ts`: once there is a session, it loads `GET /ui/editor-kinds`. It then contributes a restorable `workbench.editor` per kind (`AppHost` on the kind's page) and an **Apps** command per app opening its main tab. `useInstalledApps()` and `openApp()` serve Forge's Applications editor. Since slice 34 it follows the live `ui.editorKinds` query, with installed manifest apps too ([Apps.md › Built in slice 34](Apps.md#built-in-slice-34)).
- **`src/services/Ui.ts`**: the REST client.
- **`vite.ts`**: the dev proxy routes `live` (with WebSocket) and `ui`.

### Verification of slice 13
See [Live.md › Verification of slice 13](Live.md#verification-of-slice-13). For the UI in particular:
- `ui` tests check that a bound prop patches alone without rendering, that only the region that read a change renders again, that a stale event gets the tree, and that `Diff` patches turn one tree into another.
- In headless Chromium, the Hello app's MUI cards, docked sub-tabs and React Flow graph drew in Foundation's example app and in Axiom.
  - A greeting typed into local state appeared after *Greet*.
  - An item opened as its own top-level tab.
  - After a reload, both tabs came back with the selected one, and the Hello tab resumed its view state (greeting and sub-tab) from NATS KV.

### Decisions to review after seeing it
- The look of the primitives in a workbench tab. The app's dark workbench and light MUI surfaces show cards as white panels on black, and secondary text is hard to read: the known dark-preference issue from slice 7.
- How responsive a round trip per click feels; local state covers typing.
- How other users' avatars and selection outlines look.
- Restorable tabs live in this browser's storage, not in the user's settings.

## Changed in slice 15
For blueprints ([Blueprints.md](Blueprints.md)):
- A handler a page has no Go handler for runs as the published `ui.handler` graph with that id (`ui.HandlerRunner`, `ui.Options.Graphs`, `uiimpl.WithGraphs`); `Handle.Value(path)` reads the payload and `Handle.Page()` the page.
- **Tree overlays**: `ui.insertNode` and `ui.removeNode` add nodes to a page's tree by command (action `ui.design`); every session holds its page's `ui.overlay` query.
- The Hello app's handlers, its counter's command and query are graphs.

## Changed in slice 23
For the Hello task list (forge 03i, [Records.md › Built in slice 23](Records.md#built-in-slice-23)):
- **Query params bound to view state.** `QueryRef.Bind` maps a param to `state.<path>` (`{"where": "state.where", "sort": "state.sort"}`). When a handler changes that state, the session subscribes again with the new params; the old data stays shown until the new snapshot replaces it, and results from the replaced subscription are dropped. A nil state value leaves the param as `Params` has it.
- **A new session's `OnOpen` runs before its queries open**, so bound queries open with its view state.
- **`Page.Setup`** runs every time a session opens, before its queries: for what the page needs to exist (Hello seeds *Task* there). A failure fails the opening.
- **`Handle.Query(query, params)`** reads a query once, as the handle's principal.
- **`mui.table` columns** take a `type` (`text`, `number`, `bool`, `date`, `dateTime`, `enum`), `editable` (a `bool` cell's checkbox sends the new `edit` event, `{key, field, value}`), `options` (an `enum` column's labels) and `sortField` (what `sort` names when `field` is a path). `field` is a dotted path, so a column can show `_expanded.assignee.displayName`.
- **A row updated in place reaches the page.** `live.Apply` changed the held data in place, and the rendered props shared it, so the bound prop never looked changed; it now copies (see Records.md).
- **The Hello app** has the task list: the page `hello/tasks`, the editor kind `app:hello:tasks` (icon `checklist`, added to the icon map) and an *Open task list* button on its main page.

## Built in slice 25
Forge's part 4a ([forge 04-Prefabs.md](../../forge/documentation/plans/04-Prefabs.md)): what a **prefab** is, as a definition edited by several people at once. Nothing renders a prefab yet; part 4b (the template evaluator) does.

### The definition
`ui.Prefab` (`ui/Prefab.go`) is a reusable component:
```json
{ "id": "taskRow", "name": "Task row", "description": "One task with a checkbox", "owner": "app:hello",
  "props":  { "task": { "type": "record", "entity": "task", "required": true } },
  "state":  { "editing": { "type": "bool", "default": false } },
  "events": { "toggled": { "payload": { "id": "string", "done": "bool" } } },
  "slots":  { "actions": { "description": "Buttons at the row's end" } },
  "queries": { },
  "template": { "type": "mui.stack", "key": "root", "props": { "direction": "row" }, "children": [
      { "type": "mui.checkbox", "key": "checkbox", "bind": { "checked": "props.task.done" },
        "on": { "change": { "handler": "onToggle" } } },
      { "type": "mui.text", "key": "title", "bind": { "text": "props.task.title" } },
      { "type": "slot", "key": "actions", "props": { "name": "actions" } } ] },
  "graphs": { "onToggle": "taskRow-onToggle", "logic": "taskRow-logic" },
  "preview": { "props": { } }, "version": 0 }
```
- **Owner**: `app:<appId>`, or none for the tenant's library (forge 04 question 3). Any prefab of the tenant can use any other.
- **Types** of props, state and payload fields are `ui.PrefabTypes`: `string`, `number`, `integer`, `bool`, `date`, `dateTime`, `enum` (with `options`), `json`, `record` and `records` (with `entity`).
- **The template** is a tree of `ui.Node`s. A node's type is a primitive, `slot` (`props.name` names a declared slot) or `prefab.<id>`, an instance of another prefab (its props, events and slots are that prefab's interface; content goes in its slots, never as children). Nodes are found by **key paths** from the root, joined with `/` (`root/row/title`), so a path stays valid while other nodes move.
- **Graphs** map a name the template's handlers use (`on: {change: {handler: "onToggle"}}`) to a graph id. A graph an event names must be a `ui.handler` graph; the others are `ui.handler` or `prefab.logic` (lifecycle; runs come with part 4b).
- **Preview** holds sample props for the Prefab Editor; it is part of the draft, not of the interface.

`ui.CheckPrefab(prefab, env)` is every problem with a definition, each at a path within it (`template.root/title.props.variant`, `props.task.entity`, `graphs.onToggle`) with a fix a model can act on. `ui.PrefabEnv` gives what it is checked against: the catalog, other prefabs (their last published version, else their draft), the prefabs each one uses (draft and published, to find a prefab that contains itself), entity types, queries (registered, or a `data.query` graph) and graph kinds. It checks names; keys (valid, unique among a node's children and slot content); every node's type; props against the component's schema, prop by prop (a bound prop counts as present, as in `ui.Validate`); required props; events a component emits; children and slots it takes; instances against the other prefab's interface; `bind` paths reading only `props.*`, `state.*`, `slots.*`, `query:<name>` of declared ones, or `local.*`; `on` handlers naming a key in `graphs`; graph kinds; declared types, entity types and query names; defaults and preview values fitting their types. `ui.PrefabsUsed(template)` lists the prefabs a template uses.

### Backend
- **Drafts** (`ui/prefabs`, following entity types): an aggregate on the `def` stream, target type `prefab`. The `ui.prefab` projection is the draft (`prefabs.Draft {prefab, rev, published, publishedRev, updatedBy, updatedAt}`) and `ui.prefabPublished` the last published version. **Every command applies its events to a copy of the draft and runs `ui.CheckPrefab`**, rejecting with problems at `prefab.<path>`, so no draft is ever invalid.
- **Commands** (`prefabs.write`; `prefab.publish` needs `prefabs.publish`):
  - `prefab.create {name, description?, owner?}`: its id is the target, or made from the name when none is sent (*Task row* is `taskRow`, then `taskRow_2`), through the new `live.Command.NewTargetFor`.
  - `prefab.setInfo {name?, description?, owner?}`; `prefab.setInterface {props?, events?, slots?, state?, queries?}`, each section given replacing that section.
  - `prefab.insertNode {parentPath?, slot?, index?, node}` (no `parentPath`: the root of an empty tree; no `index`: the end; a node without a key gets one from its type, `checkbox`, `checkbox_2`, new in the whole template), `prefab.updateNode {path, props?, bind?, on?, key?}` (a merge: each prop, binding and handler given is set, `null` clears it, one left out stays; forge 04a question 2), `prefab.moveNode {path, parentPath, slot?, index?}`, `prefab.removeNode {path}`, `prefab.wrapNodes {paths, wrapper}` (siblings, in their order, into a wrapper that takes the first one's place; wrapping the root alone makes a new root) and `prefab.unwrapNode {path}`.
  - `prefab.attachGraph {name, graph}` and `prefab.detachGraph {name}`; `prefab.setPreview {props}`.
  - `prefab.publish {rev, allowBreaking?}`.
- **Each command's tool reports** `{rev, published, problems, nodes}`, `nodes` being every node's path, so a model sees the key a new node got (`prefabs.Report`).
- **Queries** (`prefabs.read`): `prefab.describe {id, published?, version?}` (the draft, the last published version, or a version from the tenant database, which never changes) and `prefab.list {owner?, search?}` (summaries keyed `prefab.<id>`: id, name, description, owner, rev, published, changed; `owner: "tenant"` is the tenant library; `search` matches name and description). Who is editing a prefab is presence on its topic, read by the editor (part 4c).
- **Publishing**: `prefab.publish` refuses a draft that moved since `rev`, as `bp.publish` and `entity.publish` do, and a draft with problems. **Breaking changes** (`prefabs.Breaking`): removing a prop, event or slot, or changing a prop's type or entity, since the last version. Publish reads every published prefab and lists those whose instances use the changed part (`{change, path, usedBy: [{prefab, path}]}`): it is refused with `prefab_breaking`, a problem per change naming them, unless `allowBreaking: true` (forge 04a question 4); the publication records the breaks it went through with.
- **Versions** (forge 04a question 3): the follower `fol_ui.prefabVersions` (`prefabs.Follow`, started by `prefab.publish` before its event commits and by reads of versions) copies each version into the tenant database, `ui_prefab_versions` (`ui.PrefabVersion`: tenant, prefab id, version, name, owner, the definition as JSON, who published it and when, the event's stream and sequence) and `ui_prefabs` (`ui.PrefabRow`, each prefab's last version), in one transaction. `ui.Models()` are migrated by `serve`.
- **REST** (`prefabservice`, `/ui/prefabs`): `GET /ui/prefabs` (each prefab's last published version), `POST /ui/prefabs/get {id, version?}` (404 `prefab_not_found`) and `POST /ui/prefabs/versions {id}`.
- **Access** (forge 04a question 1): the resource type `prefabs` (`ui.ContributePrefabAccess`) with `read`, `write` and `publish`, on `tenant:<id>/ui/prefab:<prefabId>` (`ui.PrefabResource`); app-owned prefabs follow their app's roles once apps exist.
- **Tools**: every `prefab.*` command is a tool (`livetools`); `uitools.ContributePrefabs` adds `prefab.describe`, `prefab.list` and `prefab.listTypes`.
- **Wiring**: `serve.go`, in Foundation and in Forge, contributes `prefabs` after `records` and before `livetools`, and registers the handler.

### Browser
`src/services/Prefabs.ts` is the REST client and declares the definition's types (`Prefab`, `PropDecl`, `PrefabView` for `useQuery<PrefabView>('prefab.describe', …)`, `PrefabSummary` for `prefab.list`). The Prefab Editor (Forge part 4c) is the first user.

### Decisions
Made while building:
1. **The definition and its check are in `ui`**, not `ui/prefabs`, so the template evaluator (4b, in `ui`) uses them without an import cycle.
2. **The target is the prefab.** The plan's `prefab.insertNode {prefab, …}` names it in the args; like every command, it is the target. `insertNode` and `moveNode` take `slot` for a slot's content, and a left-out `index` is the end.
3. **`live.Command.NewTargetFor`** makes a target's id from the arguments, so `prefab.create` makes the id from the name. `NewTarget` takes no arguments.
4. **Generated keys are new in the whole template**, not only among siblings, so the path a model reads back is unambiguous.
5. **Bindings are only what 4a can check**: `props.*`, `state.*`, `slots.*`, `query:<name>` and `local.*`. `app.*`, `user.*`, `=` expressions, `repeat`, `when` and the `sx` token rule are part 4b's and are refused or unchecked until then.
6. **Instances are checked against the other prefab's last published version**, else its draft, since instances render published versions; a prefab containing itself is found through drafts and published versions both.
7. **A breaking change is refused only when a published prefab uses the changed part** (sets or binds the prop, handles the event, fills the slot). One that breaks nothing publishes without `allowBreaking`.
8. **A command is refused when the draft it leaves has any problem**, whatever caused it. So an interface change is made in steps: add the new prop, rebind the nodes, then remove the old one.

### Verification of slice 25
- `go test ./features/ui/prefabs` (also with `-race`, and three runs in a row), against embedded NATS and SQLite:
  - *Task row* built by commands alone (its id made from its name, keys from types, a slot, graphs attached); a second subscription to `prefab.describe` receives each change as it lands; wrap, move, unwrap, a merged update clearing one prop, preview data and remove by key paths.
  - Two designers: one moves a node while the other updates another node's props, five rounds at once, and both changes land each time; two updates of different props of one node both land.
  - Rejections with problems and paths: an unknown component, an invalid prop value, an unknown prop, a duplicate key, a bind to an undeclared prop and to `user.*`, a handler with no graph, a lifecycle graph as a handler, a graph of another kind, a missing graph, detaching a graph a handler uses, an undeclared slot, an unknown entity type, removing a prop still bound, preview data for no prop, a bad owner, a prefab containing itself directly and through another, an instance missing a required prop, moving the root, a path with no node; none changes the draft.
  - A stale publish is refused; version 1 reaches `ui_prefab_versions` with its sequence, and `ui_prefabs`; `prefab.describe {version: 1}` reads it; removing a prop *Task list* sets is refused at publish with `prefab_breaking`, naming *Task list* at `template.root/row`, and goes through with `allowBreaking`; removing an unused slot publishes.
  - `prefab.list` by owner, the tenant library and search.
  - The tools a chat model gets (`prefab.create` without a target, `setInterface`, `attachGraph`, `insertNode` with its report of node paths, `prefab.describe`, `prefab.list`), run directly in Go.
- `go test` of `live/...`, `ui/...`, `records/entities`, `records/tools` and `cognition/drafts` still pass; Foundation's and Forge's `serve` vet, and the common frontend type-checks.
- Forge's server, run fresh: `GET /api/v1/ui/prefabs` answers `{"prefabs":[]}`, `POST /ui/prefabs/get` a 404 `prefab_not_found`, and the extensions and resource types list the prefab commands, queries, projectors, tools and the `prefabs` type.

### Decisions to review after seeing it
- Decision 7 (a breaking change that breaks no published prefab publishes without the flag); pages are checked once they exist (forge 05-Applications).
- Decision 8 (interface changes in steps), once the Prefab Editor's Interface tab (4c) shows how it feels.

## Built in slice 26
Forge's part 4b ([forge 04-Prefabs.md](../../forge/documentation/plans/04-Prefabs.md)): prefabs render. The **template evaluator** expands a prefab instance's template, props and state into primitives on the server, inside a session's render, so the browser only ever receives primitives. Instances have their own state, raise events their user handles, and run their `prefab.logic` graphs. A page can be a prefab.

### Template constructs
A template's nodes are primitives, `slot`, `prefab.<id>` and two constructs, which are nodes like any other (`ui/Template.go`), so `prefab.*` commands edit them by key paths:
- **`repeat`**: `{"type": "repeat", "key": "rows", "props": {"each": "query:tasks", "as": "task", "key": "task._id"}, "children": [template]}`. Its one child is drawn once per row, keyed `<repeat key>_<row key>` (characters other than letters, digits, `-` and `_` become `_`), in place of the repeat among its parent's children. `each` is any binding of a list; a records query's page (`{records, next}`) repeats its records. The row's name is readable in the row's bindings and expressions. A repeat with no child yet repeats nothing.
- **`when`**: `{"type": "when", "key": "editing", "props": {"test": "state.editing"}, "slots": {"then": [node], "else": [node]}}`. The branch the test picks (truthy: not null, false, 0, "" or empty) is drawn in place; an empty branch draws nothing.
- **`slot`**: the content the instance's user passes in that slot, evaluated in the user's scope, inside a `mui.box` keyed by the slot node.
- **A prefab instance**: `prefab.<id>` follows the last published version, `prefab.<id>@<n>` pins version `n` (forge 04b question 2). `ui.InstanceOf(type)` splits it.
- **Bindings** (`bind`, `each`, `test`): `props.*`, `state.*`, `local.*`, `query:<name>`, `slots.<slot>` (whether the user passed content), `app.*` (`{id, page}` until app settings exist), `user.*` (`{id, tenant, kind}`, ids as text), a repeat's row name, or an **expression**: a value starting with `=` is an `expr-lang/expr` expression over `props`, `state`, `queries` (`queries.<name>`), `app`, `user`, `slots` and the rows (question 5). It cannot read `local`, which only the browser has. An `on` payload value starting with `=` is evaluated too.

`ui.CheckPrefab` checks all of these: the constructs' props and children, row names, binding roots, expressions compiled against the names they may read (and the `props.`/`state.`/`queries.` members they read), pinned versions against that version (`PrefabEnv.Version`), and the **sx rule** below.

### How it renders
- **`ui.PrefabPage(prefab, props)`** (question 1) is a `ui.Page` whose render evaluates a published prefab as the instance at the root (key `root`); the page's params, an object, add to its props. Set its `Descriptor`, `App` and `Title`. It has no tree overlay.
- **Instances are regions.** Each instance renders inside `r.Region(<its key>, …)`, so a change to its state or queries renders only it again. Its props are read in its user's scope, so a change there renders the user, and the instance with it.
- **Bindings stay bindings where they can.** In the evaluated tree, an instance's `state.x` becomes `state.instances.<key path>.x`, `query:q` becomes `query:<key path>#q` and `local.x` becomes `local.instances.<key path>.x`, so a state or query change still patches a bound prop alone, without rendering. Props, rows, `app`, `user`, `slots` and expressions are read at render, and tracked by what they read.
- **Key paths.** An instance is known by its key path from the root, joined with `/` (`root/rows_7310…`). Its view state lives at `instances.<key path>` of the session's (`state.*` in the instance), and so does its local state in the browser (question 6). A prefab whose template's root is another prefab names the inner one `<key path>/<its key>`.
- **Definitions are queries.** A session holds `prefab.describe {id, published: true}` (or `{id, version}` for a pinned one) as `__prefab:<id>` (`__prefab:<id>@<n>`) for each prefab it renders, so a publish renders its instances again at once, while a pinned instance stays. Opening a page waits, up to 3 seconds in all, for the definitions and instance queries rendering holds, and renders again; an instance whose definition has not arrived draws an empty `mui.box` until it does.
- **Problems are `ui.error` nodes in place**: an unknown or unpublished prefab, a missing required prop, a prop of the wrong type or one the prefab does not declare, a binding to nothing, an expression that fails, and an instance beyond the session's cap.
- **Queries per instance.** Each mounted instance holds its declared queries (`QueryDecl`: params bound to props are read at render; params bound to state follow it as `QueryRef.Bind` does), subscribed again when their params change. A session holds at most `ui.MaxSubscriptions` (200) queries (question 4); an instance beyond the cap shows a problem with the fix "pass records down as props". `Session.Queries()` lists the names held.
- **Handlers.** An instance's template's events send `prefab:<key path>#<graph name>` (`ui.InstanceHandler`). `Session.Event` runs that graph of the instance's prefab with a handle scoped to the instance: `State`/`Set` are its own state, `Prop` its props, `Held` its queries' data.

### Events and lifecycle
- **Raising events** (question 3). `Handle.Raise(event, payload)` raises one of the prefab's declared events: the handler its user's `on` maps it to runs at once, in the user's scope, with the user's mapped payload under the raised one, and its client actions join the event's. An event nobody handles, or one raised by the page's root, does nothing. Raises nest at most 16 deep.
- **Lifecycle** (question 7). `prefab.logic` graphs run through `ui.LogicRunner` (`uinodes.Runner.RunLogic`): *On mount* after the instance is first drawn, *On props changed* when its props differ after its user rendered again, *On query changed* when one of its declared queries changes, and *On unmount* when it is no longer drawn. Each gets `props`. Runs are queued while rendering and run, in order, after the tree or the patches are sent; what they change is sent in turn (8 rounds at most). One session's runs are one at a time, as all its work is.
- **Unmounting** ends the instance's queries and subscriptions and removes its state; its unmount run reads the state it had, cannot change it and shows nothing (its client actions are dropped).
- **Subscriptions.** `Handle.Subscribe(path, query, params)` keeps a query's data at a path of the instance's state while it is mounted, so bindings read it as `state.<path>`; subscribing the same path again with other params replaces it.

### Nodes (`ui/nodes`)
- *Raise event* (`ui.raiseEvent`; `ui.handler` and `prefab.logic`): config `event` and `inputs` (payload fields, each a pin), plus a `payload` pin. It is not the command graphs' *Emit event*.
- *Subscribe to records* (`ui.subscribe`; `prefab.logic`): config `path` and `query` (`records.find` when empty), pin `params`.
- *Get prop* (`ui.getProp`) and *Get query data* (`ui.getQuery`).
- *On mount*, *On props changed*, *On query changed* and *On unmount* now run.
- These need a `uinodes.PrefabHost` (`Host` with `Prop`, `Held`, `Raise`, `Subscribe`), which `*ui.Handle` and the test host are; other hosts are unchanged. A `prefab.logic` graph's *Test* runs on the test host, which reports `raised` and `subscriptions` too.

### Styling
`sx` takes only theme tokens (`ui.CheckSx`, `ui.SxTokens`): spacing (`m`, `mt`, `mr`, `mb`, `ml`, `mx`, `my`, `p`…, `gap`, `rowGap`, `columnGap`) as theme units from 0 to 16 in halves, or `auto` for margins; `color`, `bgcolor` and `borderColor` as palette roles (`primary.main`, `text.secondary`, `background.paper`, `action.hover`, `divider`, …); `typography` as a variant. `ui.CheckPrefab` and `ui.Validate` refuse anything else, with the tokens as the fix. Go render functions are not checked, since they predate the rule.

### Preview, tools and REST
- **`ui.Preview(args, options, prefab, props, draft)`** evaluates a prefab once in a throwaway session (its draft, or its last published version; with no props, its preview data): its queries are read once and no logic runs. It returns the tree and its problems at their key paths (`ui.TreeProblems`).
- **Tools**: `prefab.preview {prefab, props?, draft?}`. `ui.listComponents` lists the published prefabs as `prefab.<id>` (category *Prefabs*), and `ui.describeComponent` and `ui.previewTree` read them as components (`ui.PrefabComponent`: its props as the props schema, its events and slots, no children). `uitools.Contribute` takes the subscriptions for this.
- **REST**: `POST /ui/prefabs/preview {prefab, props?, draft?}` → `{tree, problems}` (`prefabservice.Preview`, needing `prefabs.read`; `prefabimpl.WithRegistry` and `WithSubscriptions`), and `Prefabs.preview` in `Prefabs.ts`.

### Decisions
Answered before building ([forge 04b-TemplateEvaluator.md › TODO](../../forge/documentation/plans/04b-TemplateEvaluator.md#todo)), each with its proposal: 1, `ui.PrefabPage`; 2, `prefab.<id>` follows the last published version and `prefab.<id>@<n>` pins; 3, *Raise event* (`ui.raiseEvent`); 4, queries per instance, capped at 200 per session; 5, `=` expressions; 6, `local.*` scoped per instance; 7, what a `prefab.logic` run can do.

Made while building:
1. **`repeat` and `when` are nodes** with their settings as props and their content as a child and slots, not the plan's `{repeat, template}` and `{when, then, else}` fields, so every `prefab.*` command, key path and check works on them unchanged. An empty repeat is allowed, since no command may leave a problem and a repeat is inserted before its row's template.
2. **Rows and branches take their place** among their parent's children (a row keyed `<repeat>_<row key>`), so a list's items are its children; slot content is wrapped in a `mui.box` keyed by the slot node.
3. **`state.*`, `query:` and `local.*` stay bindings**, rewritten to the instance's scope, so the props-only patch of slice 13 holds inside prefabs; everything else is read at render.
4. **An instance is known by its key path**, which is where its state lives; a row's key comes from its record id, so a row keeps its state while the list reorders.
5. **Definitions are held as queries** by every session that renders them; an unknown or unpublished prefab is a problem in place, not an error frame.
6. **Unmounting removes the instance's state**, as React does, and its unmount run reads a copy.
7. **Expressions read queries as `queries.<name>`** and cannot read `local`; `app` is `{id, page}` until applications have settings.
8. ***Get prop* and *Get query data*** were added, since handlers see their instance's props and queries; *Raise event* takes its payload as declared input pins.
9. **`uinodes.PrefabHost`** is a separate interface, so hosts that only run handlers need nothing new.
10. **The sx rule exempts Go render functions** (Hello's pages use `height`, `flex` and `minHeight`); the token set is strict (no sizes, flex or borders) until forge 04 question 4 is answered.
11. **`prefab.preview` is a throwaway session**, so it renders exactly as a page does.

### Verification of slice 26
- `go test ./features/ui/prefabs` (`render_test.go`; also with `-race`, three runs in a row), against embedded NATS and SQLite, with graphs published by `bp.import`/`bp.publish` and prefabs by `prefab.*` commands, and a session opened with `ui.Open` on `ui.PrefabPage("taskList", …)`:
  - the page renders only primitives: *Task list*'s heading from its page props, a pinned *Badge*, and a row per task from a `records.find` repeat, in order and keyed by record id, each a *Task row* with a nested *Badge* labelled by an expression, a `when` showing nothing, and the list's slot content reading the list's props; the checkbox's handler names the instance's graph;
  - another user renaming a task patches only that row's instance, and a state change (*Edit*) only its own;
  - a row's toggle raises `toggled` with the task's id, the list's handler gets the payload and sends `record.update`, and the row shows it done;
  - republishing *Badge* draws every row's badge again, while the pinned one stays;
  - *On mount* runs for each row and *Subscribe to records* fills its state (its count shows 2); a record change runs the list's *On query changed* and the row's *On props changed*; deleting the task runs its *On unmount* with its last props, and its subscription and queries are no longer held;
  - an `sx` with a raw colour and a width is refused with the tokens as the fix, and `ui.Validate` refuses one; `ui.Preview` of a draft uses its preview data, and reports an instance of a prefab that is only drafted, and a required prop bound to nothing, at their key paths.
- The rest of `go test ./features/ui/...`, `live/...`, `records/...`, `cognition/...` and `tools/...` pass; 4a's test now expects `user.*` to be a binding. Foundation's and Forge's `serve` vet, and the common frontend type-checks.
- Forge's server, run fresh: `POST /api/v1/ui/prefabs/preview {prefab: "nope"}` answers a `ui.error` tree with the problem at `["root"]`, and the extension points list `prefab.preview`, `ui.raiseEvent` and `ui.subscribe`.

### Decisions to review after seeing it
In the Prefab Editor's canvas ([forge 4c](../../forge/documentation/plans/04c-PrefabEditor.md)) and with Hello's task page as *Task list* ([forge 4d](../../forge/documentation/plans/04d-TaskListPrefabs.md)):
- rows in place and slot content in a box (decision 2), and how they lay out;
- an instance's state removed when it is no longer drawn (decision 6);
- the strict sx token set (decision 10, forge 04 question 4).

## Built in slice 27
Forge's part 4c ([forge 04c-PrefabEditor.md](../../forge/documentation/plans/04c-PrefabEditor.md)) builds the Prefab Editor in Forge ([forge PrefabEditor.md](../../forge/documentation/PrefabEditor.md)). What it needs from Foundation is a page that draws a prefab's draft for design, a renderer that can select instead of click, and dry runs.

### The design page
- **`ui.DesignPage()`** (`ui/Design.go`, page `prefabs/design`, `ui.DesignPageID`, registered by `prefabs.Contribute`, needing `prefabs.read`) draws a prefab's **draft** with params `{prefab}`: the root instance reads the draft (`__prefab:<id>~draft`), nested prefabs their published versions, and its props are the draft's **preview data** (`prefab.setPreview`, shared by everyone editing it; forge 04c question 2). Every command on the draft or its preview draws it again, as any query change does.
- **`Node.Src`** (`src` on the wire): in a design session each evaluated node carries the key path of the template node it was drawn from (`root/row/title`), so the browser can select it. A repeat's rows carry their template child's path, a `when`'s branch its branch's, slot content its own, and a nested prefab's nodes nothing but its root, which carries the instance's path. Only design sessions set it; `Diff` replaces a node whose `src` changes.
- **Dry pages.** `Page.Dry` runs a page's handlers and logic as dry runs (forge 04c question 1): `Handle.Command` sends nothing and adds a `dryRun` client action (`name` the command, `id` its target, `params` its args) instead, and `uinodes.Runner` runs **drafts** of the graphs (`Handle.Dry()`, `engine.Draft`, which reads `drafts.Source.Draft` when the source is an `engine.DraftSource`, else the published graph). The design page is dry, so its *On mount* logic and, with *Interact*, its handlers run without committing anything. Queries are read as usual.

### Browser
- **`UiRenderer`'s `design` mode** (`'select' | 'interact'`): every node with a `src` is wrapped in `<div data-src style="display: contents">`, which takes no part in layout, so the host can find the node under the pointer (`closest('[data-src]')`) and outline it (a `Range` over the wrapper's contents gives its box). In `'select'` the nodes' events are not sent; in `'interact'` they are.
- `UiNode.src` and the `dryRun` action in `ui/types.ts`.

### Fixed while building
1. **A patch at the root had `"path": null`** (a nil slice), which the browser's `applyPatch` could not read; `Diff` now sends `[]`, and `applyPatch` reads a missing path as the root. No page had patched its root's own children before.
2. **A move or insert to index 0 sent no index** (`omitempty`), so the browser put the node last. `Patch.Index` is always sent.
3. **A prefab whose template's root is another prefab**, made while the page was open, stayed an empty box: the outer instance's region and the inner one's share a node, and the outer one replaced the inner one's entry, losing what it read (the inner prefab's definition). `Render.Region` and `Session.update` now give the outer region what the inner one reads.

### Decisions
Answered before building ([forge 04c-PrefabEditor.md › TODO](../../forge/documentation/plans/04c-PrefabEditor.md#todo)), each with its proposal: 1, the Canvas is a dry preview session over the draft, a click selects and *Interact* runs handlers dry; 2, preview data is shared, in the draft; 3, no undo in this part.

Made while building:
1. **A registered page, not a new frame**: the Canvas is an ordinary UI session on `prefabs/design`, so it resumes, patches and shows problems as every page does.
2. **Template paths travel on nodes** (`src`) rather than as a side map, so a patch carries them with the nodes it replaces.
3. **The whole design page is dry**, not only with *Interact*: logic runs on mount in its draft, so the Canvas shows what an instance's logic does, and nothing it sends is committed.
4. **Dry runs read graph drafts** of every graph the page runs, nested prefabs' too.
5. **Wrappers use `display: contents`** so the renderers and their layout are untouched; the Canvas measures contents, not the wrapper.

### Verification of slice 27
- `go test ./features/ui/prefabs` (`render_test.go`): `TestTheDesignPageDrawsTheDraftDry` opens `ui.DesignPage()` over *Task list*'s draft: the nodes carry their template paths (the root, the heading, the pinned badge, a repeat's rows as `root/rows/row`, slot content as `root/rows/row/by`, a nested prefab's inner nodes none), the shared preview data are its props and a `prefab.setPreview` patches the heading, and a row's toggle raises `toggled`, whose handler's `record.update` comes back as a `dryRun` action while the record stays unchanged. `TestARootInstanceAddedLaterIsDrawn` inserts a published *Badge* as an empty prefab's root while its design page is open, and the chip is patched in.
- `go test ./features/ui`: root patches have the path `[]`, and a move or insert to the first place sends index 0.
- The rest of `go test ./features/ui/...` and `./features/cognition/...` pass; `go vet` passes; the common frontend type-checks.
- In Forge's Axiom in headless Chromium, against Forge's server: see [forge PrefabEditor.md › Verification](../../forge/documentation/PrefabEditor.md#verification).

### Decisions to review after seeing it
- Logic running dry on the Canvas, and *Interact*'s dry-run log (decisions 3 and 4).

## Built in slice 28
Forge's part 4d ([forge 04d-TaskListPrefabs.md](../../forge/documentation/plans/04d-TaskListPrefabs.md)), the iteration's demo: the Hello app's task page is the *Task list* prefab, which repeats *Task row*, and both are made only by `prefab.*` commands. Its Go render function is gone.

### The prefabs
- ***Task row*** (`taskRow`, owner `app:hello`): prop `task` (a record of *Task*); events `toggled {id, done}` and `renamed {id, title}`; slot `actions`. Its template is a row of a checkbox bound to `props.task.done` (`change` → `onToggle`), the title in a text field (`blur` and `submit` → `onRename`), a `when` drawing the due chip (`mui.chip`, colour `primary`, outlined) when the task has a due date, a `when` drawing the assignee's name from `_expanded.assignee`, and the `actions` slot. `onToggle` (`taskRow-onToggle`) raises `toggled` with the task's id and the opposite of its `done`; `onRename` (`taskRow-onRename`) raises `renamed` with the trimmed title when it is not empty and differs.
- ***Task list*** (`taskList`, owner `app:hello`): prop `heading` (`Tasks`); state `show`, `where`, `sort` and `sortShown`; queries `tasks` (`records.find` on *Task*, expanding `assignee`, `where` and `sort` bound to its state) and `open` (the `hello.openTasks` graph). Its template is the heading with the open count, the add form (*New task*, *Due*, *Assign to me*, *Add*, its values in local state), the tools (*Show*; *Sort by* Due, Title or Created and a direction button whose label, icon and payload are `=` expressions over `state.sortShown`; *Clear done*), and a `repeat` of *Task row* over `query:tasks`, with a `when` showing an empty state when there are none. Its handlers are Hello's graphs: `onAdd` (`hello-onAddTask`), `onShow` (`hello-onFilterTasks`), `onSort` (`hello-onSortTasks`), `onClearDone` (`hello-onClearDone`), and, for its rows' events, `onToggled` (`hello-onTaskToggled`, sending `task.toggle`) and `onRenamed` (`hello-onTaskRenamed`, sending `record.update`).

### How Hello gets them
- **Seeded by commands** (forge 04d question 1). The page `hello/tasks` is `ui.PrefabPage("taskList", nil)`; its `Setup` seeds *Task* and then the prefabs (`hello/prefabs.go`): when a prefab has no published version, Hello sends its `prefab.*` commands as the tenant's service principal (a draft left unpublished is published as it is), then publishes *Task row* and *Task list* in that order and waits until they can be read. The Prefab Editor edits them like any other prefab afterwards, and a published edit reaches every open list.
- **No overlay** (forge 04d question 2): a prefab page holds no `ui.overlay` query; tree overlays stay for Hello's Go pages (main and item).
- **A new field is shown by prefab ops**: with the table gone, a field added to *Task* no longer becomes a column by itself; `prefab.insertNode` puts it in *Task row* (the test adds a `priority` chip).

### Changed while building
- **A prefab page takes only the params its prefab declares.** `ui.PrefabPage` added every page param to the root's props, and a tab's params always hold its `id` (`AppEditorObject`), so opening *Task list* as the `app:hello:tasks` tab drew "Task list has no prop id". The root now takes a param only as a prop it declares and was not given (`evaluator.params`); others are left out.
- **Prefabs can use built-in graphs.** A prefab's check found graphs only among drafts and published graphs, so it refused Hello's built-in ones; `graphKind` (`ui/prefabs`) now also finds built-in graphs (`cognition.Graphs`), and so does the query check for `data.query` graphs such as `hello.openTasks`.
- **An input given a value by the server can be typed in.** A text field, number, date or select whose `value` came from the server rather than a binding (a prefab's prop is read at render, so *Task row*'s title arrives as a plain value) showed that value and ignored typing; it now holds the typed value, starting from the server's and following it when the server's changes (`useValue` in `renderers/mui/components.tsx`).
- **`ui.Open` holds its queries under the session's lock.** A query held first could deliver its snapshot, which reads the session's queries, while the next was being held: a data race the race detector found once the task page held more queries.
- **Seeding reads a just-made draft again.** Right after `entity.create` (or `prefab.create`) the draft's projection may not show it yet; the publish loop (`publishDraft`, shared by *Task* and the prefabs) now reads it again rather than failing the page's opening.
- The icons `arrow_upward` and `arrow_downward` are in the icon map.
- Hello's graphs: `hello-onEditTask` (the table's cell edits) is gone; `taskRow-onToggle`, `taskRow-onRename`, `hello-onTaskToggled` and `hello-onTaskRenamed` are new; 23 in all.

### Decisions
Answered before building ([forge 04d-TaskListPrefabs.md › TODO](../../forge/documentation/plans/04d-TaskListPrefabs.md#todo)): 1, Hello's `Setup` sends the prefabs' commands as the service principal on first open; 2, overlays stay for Go pages only; 3, *Sort by* (Due, Title, Created) and a direction toggle; 4, everything the Go page had carries over, the title edited in its row, raising `renamed`.

Made while building:
1. **The prefabs' graphs are Hello's built-in graphs**, like all of Hello's (editable, a tenant's published version replacing them), rather than graphs Hello makes with `bp.*` commands on first open. Question 1's "`prefab.*` and `bp.*` commands" is met by the prefabs alone; the graphs are edited by `bp.*` as before.
2. **Events are handled by `ui.handler` graphs** named in the row's `on`, as 4b built them; the plan's "its `prefab.logic` graph handles `toggled`" is that handler.
3. **The existing handlers are reused** (`hello-onAddTask`, `-onFilterTasks`, `-onSortTasks`, `-onClearDone`): in an instance they read and set the instance's own state, so they needed no change. The sort controls send `-onSortTasks`'s `{field, direction}`, the direction button's computed by expressions at render.
4. **The due chip's colour is a fixed palette role** (`primary`); the demo changes it by `prefab.updateNode` on `root/hasDue/due`.
5. **A page param the prefab does not declare is left out**, not a problem, since every tab sends `{id}`; a prefab that wants the id declares a prop `id`.

### Verification of slice 28
- `go test ./features/ui/hello` (`TestTheTaskListPrefabsWithTwoUsers`, two users over the live connection, the page opened with a tab's `{id}` params): opening the list seeds *Task* and publishes *Task row* and *Task list* (owner `app:hello`), with no `ui.error` and no table, an empty state while there are no tasks; Ada adds a task with *Assign to me* and a due date and Bob's rows show it with her name and the `primary` due chip, and the count `1 open`; rows sort by due; an empty title toasts; Bob shows open tasks while Ada ticks one done; Bob sorts by title, then flips the direction (the button's label, icon and payload come from expressions), while Ada's order stays; Ada renames a task in its row and Bob sees it (an empty title changes nothing); both toggle one task at once and it ends open, two events on; *Clear done*; `prefab.updateNode` of the due chip's colour and a publish reach both open lists; a `priority` field added to *Task* and shown by `prefab.insertNode` of a `when` and chip into *Task row*, published, shows `medium` on a new task and nothing on an old one. Without the params fix it fails with "Task list has no prop id". `TestTheGraphsAreValid` resolves all 23 graphs. Three runs in a row pass, and with `-race` 100 runs had one failure, a timeout in the older counter test that fails the same way without these changes (see below).
- `go test ./features/ui/...`, `live/...`, `cognition/...`, `records/...` (but `records/search`, which needs the CGO build) and `tools/...` pass; `go vet` passes; Forge's Axiom builds and lints.
- **The race in `ui.Open`**: before the fix, `-race` runs of the two older Hello tests failed 14 times in 100, 12 of them data races; the two timeouts (a counter patch not arriving within 5 s) remain, about one run in a hundred, and predate this slice.
- **In headless Chromium**, Forge's server and Axiom run fresh, two users: Ada opened *Hello › Open task list* (2.4 s the first time, seeding *Task* and both prefabs), Bob opened it too; Ada's new task reached Bob in about 330 ms and the count followed; Bob's tick reached Ada in about 150 ms (`1 open`); Ada's rename in the row reached Bob; Bob's *Sort by Title* and *Descending* changed his order alone; then Ada opened *Forge › Prefabs › Task row*, selected the due chip on the Canvas, set its colour to *secondary* in the Inspector and published: Bob's open list showed the secondary chip by the time the click returned, and Ada's own *Tasks* tab too.

### Decisions to review after seeing it
- The prefabs' graphs as Hello's built-ins rather than made by `bp.*` on first open (decision 1).
- A new *Task* field shown only once *Task row* is changed to show it, where the table showed it as a column at once.
- The rows' layout (a row of a checkbox, a text field, the chip and the assignee, rather than a table with headers), and *Sort by* with a direction button in place of sortable headers.

## Built in slice 29
Forge's part 4e, the starter library ([forge 04e-StarterLibrary.md](../../forge/documentation/plans/04e-StarterLibrary.md)): Foundation gets what the starter prefabs need, and Forge keeps and seeds the prefabs themselves ([forge StarterLibrary.md](../../forge/documentation/StarterLibrary.md)).

### Importing a prefab
`prefab.import {prefab}` (`ui/prefabs`, event `imported`) replaces a prefab's whole draft with a definition as `prefab.describe` gives it, or makes it (its id from the definition's name when no target is sent). It is checked whole like every other command; an import identical to the draft commits nothing; the draft keeps its published versions until it is published again. Seeding a tenant with ready-made prefabs uses it.

### Bindings
- **`presence:<binding>`** reads who is in an object: the binding after the prefix (`props.object`, a row, an `=` expression) gives `"<type>.<id>"`, and the instance holds the `live.presence` query of that object under `<key path>#presence:<type>~<id>` (no `.` in a query's name). On a primitive it stays a `query:` binding, so a change patches that prop alone; on a prefab instance it is read at render. `ui.CheckPrefab` checks the inner binding (`ui.PresencePrefix`). Being *in* an object is the browser's: `presence.avatars`'s `join`.
- **A query param may be an `=` expression over the props** (`bind: {where: "={_id: props.id}"}`), read when the instance's props change; `ui.CheckPrefab` refuses one that reads anything but `props`.

### Primitives
- **`record.fieldEditor`** (`field`, `value`, `problem`, `label`, `entity`, `disabled`; event `change {value}`): the input for a field by its type, values in the record's format: text, long text and rich text (a plain-text editor making a ProseMirror document), number, decimal and money (with or without a fixed currency), bool, date, date and time (local, sent in UTC), duration, enum and multi-enum, tags, relation (a picker over the target type's records, named by its title field, live from `records.find`; a relation to `user` takes ids), user (an id), file (upload through `Records.upload`), and JSON for json, list, object and volume file; a computed field shows its value read-only. On its own, a text-like field sends `change` on blur or Enter, the rest at once.
- **`record.fieldValue`** (`field`, `value`, `expanded`, `entity`, `record`): a value shown by its type (dates, money with its currency, an enum's option as a chip, related records by their titles, a file as a link, rich text as text, JSON as a block); a formula or rollup is shown as what it returns.
- **`record.form`**, the form-merge primitive (forge 04e question 2): a field editor per visible field with Save and Cancel. It keeps what this user typed per field until saved (`renderers/record/formMerge.ts`): a field they have not touched follows the `record` prop, so others' changes land in place; a touched field another change moves becomes a conflict showing the other value and who made it (*Use theirs* / *Keep mine*; Save waits until each is chosen); an edit the record comes to hold is dropped (the save landed). `save {set, seq}` carries only the fields this user changed; `problems` (field id → message) shows under each field. The name is that of the user whose change moved the field (question 3), from `users` (presence entries) matched to the record's `_updatedBy` at that change.
- **`presence.avatars`** (`users`, `join`, `max`, `self`): who is in an object, one avatar per user in their colour, this window left out unless `self`; with `join`, this window is present in that object while the node is shown.
- **`mui.breadcrumbs`** (`items: [{label, id}]`, event `navigate {id, index}`).
- `mui.textField` takes `debounce` (ms) for its `change`; `mui.table` columns take `recordField` (cells drawn as `record.fieldValue` draws them) and `type: "actions"` with `actions: [{id, label, icon}]`, each button sending `action {key, action}`.

### Elsewhere
- **Expanded records carry `_title`** (`records/data`, `data.MetaTitle`): the related type's title field, or a user's display name, so a view names a related record without knowing its type.
- **Administration › Tenants takes actions**: the browser point `tenants.action` (`editors/tenants/points.ts`: `{title, confirm: {title, text, label?}, run(tenant)}`) adds a button above the selected tenant's members, confirmed in a dialog, its result shown as an alert. Forge's *Seed prefabs* is one.
- **`foundation(backend, more)`**: an app with backend routes of its own names them (Forge: `forge`), and the dev server proxies them too.

### Decisions
Answered before building ([forge 04e › TODO](../../forge/documentation/plans/04e-StarterLibrary.md#todo)): 1, the library is seeded into tenants by Forge (`cli seed` and the Tenants editor's *Seed prefabs*), not built into Foundation; 2, the plan's primitives plus a form-merge primitive (`record.form`); 3, the name of the user whose change moved the field.

Made while building:
1. **Seeding is by commands, whole definitions at a time**: `prefab.import` was added rather than replaying `prefab.insertNode` and friends, so re-seeding resets a changed prefab in one event.
2. **Presence bindings read; the browser joins.** The server cannot keep a connection present (that is the live connection's refresh loop), so `presence.avatars`'s `join` sets the window's presence, and `presence:` only reads.
3. **Who is named for a conflict is resolved in the browser from presence**, since records hold user ids only. A user who changed a field and left before the change arrived shows as *Someone*. User ids in records are JSON numbers, which lose digits past 2^53 in the browser; they are matched to presence's string ids as numbers, which round alike.
4. **Save sends no `expect`.** `record.update` records only the fields that change, and conflicts are settled in the form before Save, so a change to another field never refuses the save.
5. **Rich text is edited as plain text in the form** (a paragraph per line); formatting is kept only for fields the user does not touch.

### Verification of slice 29
- `go test ./features/ui/prefabs` (`TestImportReplacesTheDraft`): an import makes a prefab with its id from the name, an identical import commits nothing, a changed one replaces the draft, a `presence:` binding of an undeclared prop and a param expression reading state are refused at their paths. `go test ./features/ui/...` and `./features/records/...` (but `records/search`, which needs the CGO build) pass.
- Forge's `go test ./features/starter` seeds the library and renders the Record table, form and detail for every entity type with only `entity` and `id` (see [forge StarterLibrary.md](../../forge/documentation/StarterLibrary.md#verification)); `presence:` bindings and param expressions are exercised there, and in headless Chromium two users edit one record in `record.form`.

### Decisions to review after seeing it
- The conflict's look (an alert under the field with *Use theirs* / *Keep mine*) and Save waiting until each conflict is settled.
- Rich text as plain text in the form (decision 5); a relation to `user`, and a `user` field, edited as ids.

## Built in slice 30
Forge's part 4f, variants and the chat acceptance ([forge 04f-VariantsAndChat.md](../../forge/documentation/plans/04f-VariantsAndChat.md)), finishing iteration 4 but for the acceptance's run with a real model. The Prefab Editor's side is in [forge PrefabEditor.md](../../forge/documentation/PrefabEditor.md#variants-built-with-slice-30).

### Variants
- **`ui.Prefab.Variants`** (`variants`, left out when there are none): `name → ui.VariantDecl {description?, props}`, named presets of the prefab's props, set whole by **`prefab.setVariants {variants}`** (event `variantsSet`). `ui.CheckPrefab` refuses a variant name that is not a valid name, a preset of an undeclared prop (`variants.<name>.props.<prop>`) and a value that does not fit its prop's type.
- **An instance picks one with its node's `variant`** (`ui.Node.Variant`, forge 04f question 2), beside `key`, `props` and `bind`: `{"type": "prefab.tag", "variant": "loud"}`. `prefab.insertNode` takes it on the node, and `prefab.updateNode {variant}` sets it (`null` or `""` clears it; the event carries `""`). `ui.CheckPrefab` refuses a variant the instanced prefab does not declare (at `template.<path>.variant`, the fix listing its variants) and one on any node that is not a prefab instance; a required prop the variant presets needs no value of its own.
- **Rendering**: an instance's props are its prop defaults, under its variant's presets, under its own props and bindings (`mount` in `Evaluate.go`; a bound prop that is `null` falls back to the variant's). An unknown variant at render time is a `ui.error` naming the prefab's. **`ui.PrefabPage(prefab, variant, props)`** takes a variant for the page's root (`""` for none; Hello's and Forge's pages pass `""`), and so do **`ui.Preview`**, the `prefab.preview` tool and `POST /ui/prefabs/preview` (`variant`).
- **Removing a variant a published prefab's instance uses is a breaking change** (`prefabs.Breaking`: `removing variant <name>` at `variants.<name>`), refused with `prefab_breaking` unless `allowBreaking`, like a removed prop. Changing a variant's presets is not breaking.
- `ui.PrefabComponent` gives an example per variant (`{type, key, variant, props}`), so `ui.describeComponent` shows a model each one.

### For the chat
Found while running the acceptance with a real model (see *Verification*), and fixed:
- **A failed tool's content reaches the model** (`tools.Loop`): the result is the tool's content with `error` beside it, where it was `{error}` alone. A refused command's tool returns its `problems`, so the model now sees each problem's path and fix (`ui.CheckPrefab`'s fixes were written for it, and never reached it).
- **A command tool refuses arguments put beside `args`** (`live/tools`): `{target, parentPath, args: {node}}` used to run the command without `parentPath` (here inserting a second root); now it fails with "parentPath belong inside args: call prefab.insertNode with {target, args: {parentPath, ...}}".
- **"There is no node at …" lists the template's node paths** (up to 40) in its fix, or says the tree is empty. Models assume the root's key is `root`; a root inserted without a key is named from its type (`card`).
- **`prefab.publish`'s report waits for the published projection** as well as the draft, so a `prefab.preview` sent right after renders the new version instead of "there is no published prefab".

### Decisions
Answered before building ([forge 04f › TODO](../../forge/documentation/plans/04f-VariantsAndChat.md#todo)): 1, the acceptance runs through Gemini from the Chat, plus a Go test of the same commands; 2, an instance names its variant in a node field, not a prop.

Made while building:
1. **`prefab.setVariants` replaces them all**, like a section of `prefab.setInterface`; it is its own command, since variants are not part of what an instance's props are checked against.
2. **Variants are left out of the JSON when there are none** (`omitempty`), so prefabs without them, the starter library's among them, are unchanged.
3. **A dropped variant instance starts without the preview values its variant presets** (the Palette, in Forge), so the variant's presets show rather than the preview data's.

### Verification of slice 30
- `go test ./features/ui/prefabs` (`TestVariants`): a variant presetting an undeclared prop, or a value of the wrong type, is refused at its path; instances pick variants, one presetting a required prop needs no label, an unknown variant and a variant on a primitive are refused; `updateNode` sets, clears (`null`) and sets again; rendered, the variant's props show under the instance's own (*mine* keeps its own colour over *loud*'s); a page and a preview take a variant, an unknown one is a problem; the component has an example per variant; removing a variant two published instances use is refused naming both, and publishes once it is back.
- `go test ./features/ui/prefabs` (`TestTheChatBuildsAContactCard`), the acceptance's commands through their tools as a model calls them: `prefab.list`, `prefab.listTypes`, a *Contact* entity type created and published, `prefab.create` (*Contact card* → `contactCard`), its interface (a required `contact` record prop, the `edit` event with `id`), a card with the name and email bound, a `ui.handler` graph built by `bp.create`/`bp.addNode`/`bp.connect` (*Get prop* `contact._id` into *Raise event* `edit`) and published, attached, an Edit button in the card's `actions` slot, preview data, `prefab.preview` of the draft with no problems and the name, email and Edit in its tree, published, and previewed published with no problems. Arguments put beside `args` are refused. Three runs in a row pass.
- `go test` of `./features/tools`, `./features/ui/...`, `./features/live/...` and `./features/conversation/...` pass; Forge's `./features/starter` passes; the common frontend type-checks and Axiom builds and lints.
- **In headless Chromium**, Forge's `serve` and Axiom: *Tag* (with *loud* and *done*) imported and published, and *Tags* holding a plain and a *loud* instance: on the Canvas the *loud* chip is primary beside the default one, the Inspector shows *Variant: loud* with its description, and the Components tab lists *done* and *loud* under *Tag*.
- **The chat acceptance with a real model is not finished.** Gemini answered from Forge's Chat only at ⚡ Fast (`gemini-3.5-flash-lite`); 💡 Standard was refused as overloaded on every try but one, which failed midway. Over several rounds the model listed the prefabs and components, made `contactCard` with `name` and `email` string props (not a contact record) and the `edit` event, inserted a card, a stack and the two texts, and, once the fixes reached it, made a `ui.handler` graph for the button. Every round ended early: at first after the `tools` section's 8 steps per round (raised to 40 for the run), then by Gemini's 429s, which were the free quota running out. It never published. Its mistakes, each refused with a fix: paths starting with `root/`, `h6` for `mui.text`, `children` for a button's label, a handler before its graph, and arguments beside `args`.

### Decisions to review after seeing it
- Variants as a node field, presets under the instance's props, and a dropped variant leaving out the preview values it presets.
- Whether `prefab.preview`'s problems are enough to fix layout without a screenshot (forge 04 question 2): not yet seen, since the model never reached a preview.
