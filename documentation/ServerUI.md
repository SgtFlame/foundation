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
  - `editorKinds.ts`: once there is a session, it loads `GET /ui/editor-kinds`. It then contributes a restorable `workbench.editor` per kind (`AppHost` on the kind's page) and an **Apps** command per app opening its main tab. `useInstalledApps()` and `openApp()` serve Forge's Applications editor.
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
