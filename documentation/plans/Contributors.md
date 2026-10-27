# Contributors: extension points for the workbench

## Context
The workbench is wired by hand today:
- `Workbench.tsx` lists the right-border tabs;
- `ComponentFactory.tsx` is a `switch` on component names;
- `EditorRegistry.ts` has a closed `EditableObject` union;
- `LeftMenuSidebar.tsx` hard-codes its sections.

Each new feature edits all four. We want features to **contribute** to named **extension points** instead, the way Eclipse plugins contribute to extension points declared by other plugins. A feature then lives in its own folder and plugs itself in.

Two contributor kinds come first:
- **ConfigurationContributor** declares application settings ([Configuration.md](Configuration.md)).
- **ChatContextSettingsContributor** adds controls to the chat composer's settings popover ([ChatCompletion.md](ChatCompletion.md)).

Status: partly built. Slice 1 built the registry on both sides, `workbench.borderTab` and `configuration`; see [Built in slice 1](#built-in-slice-1). Slice 4 converted `workbench.editor` and added `chat.contextSettings`, `chat.command` and, on the server, `chat.contextProvider`; see [Built in slice 4](#built-in-slice-4). Slice 5 added `dashboard.widget` and, on the server, `retrieval.source`; see [Built in slice 5](#built-in-slice-5). Slice 8 added `chat.clientTool` and, on the server, `tools.tool`; see [Built in slice 8](#built-in-slice-8). Slice 11 added `authz.resourceType` on the server; see [Built in slice 11](#built-in-slice-11).

Decision: contributors are **compile-time and auto-discovered**. The registry API itself is dynamic and never assumes compile-time loading, so runtime-loaded plugins (a manifest plus `import()`) can be added later without changing contributors.

## Frontend: `src/workbench/extensions/`
- **`ExtensionPoint.ts`**
  - `defineExtensionPoint<T extends Contribution>(id, description)` returns an `ExtensionPoint<T>`. It is a typed token, so `contribute` and `useContributions` are type-checked per point.
  - `Contribution` is the base shape:
    - `id`: unique within the point;
    - `order?`: sort key, default 1000;
    - `source`: the contributing feature, for example `'chat'`, shown in Settings and DevTools;
    - `when?(ctx)`: an optional visibility predicate over a small context object, for example `{ surface: 'document' | 'chat' }`.
- **`ExtensionRegistry.ts`**: a singleton `extensions`.
  - `contribute(point, c)` returns a `dispose` function. A duplicate id replaces the earlier contribution and logs a warning through `log.In('extensions')`, which keeps Vite HMR working.
  - `get(point, ctx?)` returns the contributions sorted by `order` and filtered by `when`.
  - `subscribe(listener)` notifies on changes. The snapshot array is replaced on each change, as DevTools' `RingBuffer` does.
  - `useContributions(point, ctx?)` reads the registry through `useSyncExternalStore`.
- **Discovery** in `main.tsx`, before the first render:
  ```ts
  import.meta.glob('./**/contributions.ts', { eager: true })
  ```
  - Each feature folder has a `contributions.ts`, which plays the role of Eclipse's `plugin.xml`.
  - Importing it calls `contribute(...)` for everything the feature provides.
  - Heavy components are referenced with `lazy(() => import(...))`, so discovery does not pull them into the main chunk.
- **Extension points declare themselves** in the feature that owns them, for example `src/editors/chat/points.ts` exports `chatContextSettings`. Contributors import the point from its owner.
- **DevTools** gets an **Extensions** tab that lists every point with its contributions, their source and their order.

### Extension points
Only the first two are built in the first implementation. The others are listed so that the registry is designed for them, and are converted one at a time.

| Point | Contribution | Owner | Replaces |
|---|---|---|---|
| `configuration` | ConfigurationContributor: a settings section (schema, defaults, optional custom editor) | `settings` | – |
| `chat.contextSettings` | ChatContextSettingsContributor: a control in the chat settings popover | `editors/chat` | – |
| `workbench.editor` | `{ kind, name, icon, component }` for an `EditableObject` kind | workbench | the `ComponentFactory` switch and the `EditableObject` union |
| `workbench.sideView` | a left vertical tab (Chronicles, Volumes) | workbench | `LeftMenuSidebar` sections |
| `workbench.borderTab` | a right-border tab (DevTools, Settings) | workbench | `Workbench.tsx`'s `borders` JSON |
| `workbench.command` | a command with an id, title, icon and keybinding, used by menus and the command palette | workbench | – |
| `dashboard.widget` | a Dashboard card ([Usage.md](Usage.md)) | `editors/dashboard` | – |
| `chat.composerAction` | a button in the composer, such as the level toggle or attachments | `editors/chat` | – |
| `chat.command` | a `/slash` command in the composer | `editors/chat` | – |
| `chat.clientTool` | a tool that runs in the browser ([WorkbenchContext.md](WorkbenchContext.md)) | `editors/chat` | – |
| `workbench.subjectDescriber` | describes what an editor kind is editing, for editors that cannot call `useWorkbenchSubject` ([WorkbenchContext.md](WorkbenchContext.md)) | workbench | – |

**`workbench.editor`** needs one change to the editor model, made when it is converted:
- `EditableObject` becomes an interface `{ kind: string; id: string; name?: string; helpText?: string }`, in place of the closed union.
- `EditorRegistry.edit` looks up the editor contribution by `kind`.
- `ComponentFactory` becomes a lookup. Unknown kinds render an "editor not available" placeholder, not `null`.

## Backend: `features/extensions`
The backend has the same idea for things the server must know about: configuration sections, chat context providers, and later tools.
- `Point[T]` is a typed token, `NewPoint[T](id)`.
- `Registry` has `Register(point, c)` and `All(point)` (sorted by `Order()`).
- Each feature exposes `Contribute(reg *extensions.Registry)` in its package, and `serve.go` calls each one explicitly after creating the services, in the same place it registers handlers.
- There are **no `init()` side effects**. Registration order is visible in `serve.go`, and tests build their own `Registry`.

Points used first:
- `config.Section` ([Configuration.md](Configuration.md)); the ai feature contributes `ai.providers`, `ai.models` and `ai.chat.levels` since slice 2 (`aisettings.Contribute`)
- `chat.ContextProvider` ([ChatCompletion.md](ChatCompletion.md))

Later points:
- `tools.tool` ([Tools.md](Tools.md)); built in slice 8;
- `cognition.node`, the node types of the Cognition Engine ([Cognition.md](Cognition.md)); built in slice 7, with `cognition.graph` and `cognition.type`;
- `retrieval.Source` ([ContextRetrieval.md](ContextRetrieval.md));
- `authz.ResourceType`, the resource types and actions each feature declares ([RBAC.md](RBAC.md)).

On the frontend, `cognition.nodeView` supplies custom node bodies for the Cognition Editor.

**Pairing across the wire**: a contribution that needs both a UI and server logic uses the **same id** on both sides. For example, a frontend `chat.contextSettings` contribution `rag` and a backend `chat.ContextProvider` `rag`. The frontend hides a contribution whose backend counterpart is missing; the backend lists its ids at `GET /config/sections` and `GET /chat/context-providers`.

## Built in slice 1
**Frontend**
- `src/workbench/extensions/` as above. `main.tsx` globs `./**/points.ts` as well as `./**/contributions.ts`, so a point is declared, and listed in DevTools, before anything contributes to it.
- `useContributions(point, ctx)` subscribes to the point's own sorted array, which only changes when that point changes, and filters by `when` on each render.
- **`workbench.borderTab`** (`src/workbench/points.ts`): `{ name, helpText?, location, size?, component }`, where `component` is usually `lazy(...)`. `pages/workbench/borders.ts` builds the borders from the contributions when the workbench first renders, one border per location that has tabs, since flexlayout draws empty borders. Later contributions join an existing border; one for a location with no border appears after a reload, because flexlayout cannot add a border to a live model. The tab component is `borderTab`, and `BorderTabHost` renders the contribution, or a "not available" placeholder once it is gone.
- **`configuration`** (`src/settings/points.ts`) is declared with the section shape of [Configuration.md](Configuration.md). Nothing contributes yet.
- DevTools is the first contribution (`src/tabs/devtools/contributions.ts`). Its new **Extensions** tab lists the browser's points and the server's.

**Backend** (`features/extensions`)
- Go methods cannot have type parameters, so the typed calls live on the point: `config.Sections.Register(reg, s)` and `config.Sections.All(reg)`. `reg.Declare(points...)` lists a point before anything contributes, and `reg.Points()` describes them all.
- A contribution embeds `extensions.Descriptor{ID, Order, Source}`, which gives it `Describe()`. An order of 0 means 1000, as in the browser.
- The point ids differ by side: the frontend's `configuration` holds every section, the backend's `config.section` (`features/config`) only those the server reads.
- `GET /api/v1/extensions/points` (`extensionsservice.ListPoints`) describes the server's points for DevTools.

## Built in slice 4
**`workbench.editor`** (`src/workbench/points.ts`)
- **The object**: `EditableObject` is the open interface `{ kind, id, name?, helpText? }`. The closed union and `editorNames` are gone.
- **The contribution**: `{ id: <kind>, name, component }`. The id is the kind, and `component` is usually `lazy(...)`.
  - `component` receives `EditorProps<T>`: `{ object, registry, tabId }`.
  - It is typed `ComponentType<EditorProps<never>>`, so an editor declaring its own object type is accepted.
- **Each editor folder** has a `contributions.ts`: foo, bar, volumes, volume, markdown, logs and chat.
- **`ComponentFactory`** special-cases only `borderTab`. Every other tab renders through `EditorHost`, which shows "No editor is available for `<kind>`" when no contribution matches.
- **`EditorRegistry.edit`**: names a tab `<editor name> <id>` when its object has no name, and exports `tabIdOf(object)`.
- **Tab names**: `VolumesObject` gained `name = 'Volumes'`. Its tab had read "Volumes volumes" from the `<editor name> <id>` fallback.
- **Adding an editor** is now a folder with a `contributions.ts` plus a sidebar icon. The sidebar stays hand-written until `workbench.sideView` (slice 6).

**Chat points** (`src/editors/chat/points.ts`)
- **`chat.contextSettings`**: `{ title, icon?, Control({ value, onChange }) }`, filtered by `when({ surface })`.
  - The control's id pairs it with the server's `chat.contextProvider`. Its defaults are the server section `chat.context.defaults.<id>`, so the contribution has no `schema` or `defaults`.
  - The popover hides a control the server has no provider for (`GET /conversation/context-providers`).
- **`chat.command`**: `{ id: <name>, description, usage?, run(arg, ctx) }`. The `ctx` offers `newConversation`, `setLevel`, `levels`, `say` and `commands`.

**Backend**
- **`chat.contextProvider`**: `conversation.ContextProviders`, declared in `serve.go`. `workbenchcontext.Contribute` registers `workbench`.
- **Order matters**: `conversationsettings.Contribute` must run after every provider, because it declares their defaults sections.
- **More sections**: `usagesettings.Contribute` adds `usage.capture`.

## Built in slice 5
- **`dashboard.widget`** (`src/editors/dashboard/points.ts`): `{ title, size: 'full' | 'half', component }`. The component gets `{ refresh, registry }`, where `refresh` changes on focus and every minute. The first widget is *Usage & cost* ([Usage.md](Usage.md#built-in-slice-5)).
- **`chat.contextSettings`** gained its second control, RAG Search (`src/retrieval/contributions.ts`, id `rag`).
- **`retrieval.source`** (backend, `retrieval.Sources`, declared in `serve.go`): `{Descriptor, Title, Axis, VectorSearch, KeywordSearch}`. `indexsource.Contribute` registers `library` ([ContextRetrieval.md](ContextRetrieval.md#built-in-slice-5)).
- **A contribution may need a service that does not exist yet**: the `rag` provider is registered before the retrieval service is created, because configuration needs the finished registry and retrieval needs configuration. Its `Contribute` takes a function returning the service, which `serve.go` fills in later. Prefer this over reordering when a cycle appears.

## Migration path
1. Add the registry and the two new points (`configuration`, `chat.contextSettings`) along with the Settings tab and the Chat UI. *Built: the registry and `configuration` in slice 1, `chat.contextSettings` with the Chat UI in slice 4.*
2. Convert `workbench.borderTab`: DevTools and Settings become contributions. *Built: DevTools in slice 1, Settings in slice 2.*
3. Convert `workbench.editor` and `workbench.sideView`. This removes the switch, the union and the hand-written sidebar. *Built: `workbench.editor` in slice 4, `workbench.sideView` (with `workbench.command`) in slice 6.*
4. Later, runtime plugins: a manifest listing a bundle URL. Loading it with `import()` calls the same `contribute` API. This needs signing or trust decisions before it is built.

## Open questions
1. **Runtime plugins**: do third-party bundles loaded at runtime ever matter? If so, plugin signing and trust need a design first. *Decision* (can wait):

## Built in slice 6
- **`workbench.sideView`** (`src/workbench/points.ts`): `{ title, icon, width?, component }`, the component getting `{ registry }`.
  - `pages/workbench/SideBar.tsx` shows a vertical tab per view and the open view beside them. Clicking the open view's tab hides the side bar; the choice is remembered in the browser.
  - Two views: **Editors** (`pages/workbench/contributions.ts`) and **Chronicles** (`src/chronicle/contributions.ts`). `LeftMenuSidebar` is gone.
- **`workbench.command`**: `{ title, icon?, section?, run({ registry }) }`. A command with a `section` is listed in the Editors view under it, sections in the order of their first command. Each editor's `contributions.ts` now contributes its launcher (`open-<kind>`), so **adding an editor is only a folder with a `contributions.ts`**. The command palette and keybindings wait.
- **`chat.contextSettings`** gains `chronicle` (the Chronicles control).
- **Backend**: `chat.roundFollower` (`conversation.RoundFollowers`, declared in `serve.go`), work that follows a reply; `chroniclecontext.Contribute` registers the `chronicle` provider and follower, taking a function that returns their dependencies (the late binding of slice 5). `chroniclesource.Contribute` registers the `chronicle` retrieval source.
- **The Volumes side view** the plan mentions was not built: Volumes stays an editor, opened from the Editors view.

## Built in slice 7
- **Backend** ([Cognition.md › Built in slice 7](Cognition.md#built-in-slice-7)), declared in `serve.go`:
  - `cognition.node` (`cognition.Nodes`): node types, a struct with function fields;
  - `cognition.graph` (`cognition.Graphs`): built-in graphs, a point the plan did not have;
  - `cognition.type` (`cognition.Types`): data types and their colours.
- **Who contributes**:
  - `cognitionnodes.Contribute`: Branch, Return and the core types;
  - `conversationnodes.Contribute`: the chat events and Chat's types;
  - `chroniclenodes.Contribute`: Chronicle's six nodes, its types and the Personal graph, taking a function that returns the step functions (the late binding again);
  - `cognitionsettings.Contribute`: the `cognition` section.
- **Frontend**: the Cognition editor contributes `workbench.editor` `cognition`, the `workbench.sideView` **Cognition** (the Explorer) and the `workbench.command` `open-cognition` (AgentOS › Cognition). `cognition.nodeView` is not built: every node body is drawn from its type and pins.

## Built in slice 8
- **Backend**: `tools.tool` (`tools.Tools`, [Tools.md › Built in slice 8](Tools.md#built-in-slice-8)), declared in `serve.go`. A tool is a struct with function fields, like a node type.
- **Who contributes**:
  - `toolsbuiltin.Contribute`: `time.now`;
  - `kbtools.Contribute` and `chronicletools.Contribute`: the knowledge base and Chronicle tools, each taking a function that returns its services;
  - `toolssettings.Contribute`: the `tools` section;
  - `toolscontext.Contribute`: the `tools` chat.contextProvider, which gathers nothing and pairs with the browser's `tools` chat.contextSettings (the Tools switch);
  - `toolsnodes.Contribute`: Cognition's `Agent` node, `llm.agent`.
- **Frontend**: `chat.clientTool` (`editors/chat/points.ts`), contributed from `src/tools/contributions.ts`: `workbench.getState`, `workbench.open` and `chat.askChoice`. A client tool has no server contribution: the browser declares it with each message.

## Built in slice 11
- **`authz.resourceType`** (`authz.ResourceTypes`, server): a resource type with its actions, each with a class and where its grant is held (see [RBAC.md › Built in slice 11](RBAC.md#built-in-slice-11)). Each feature registers its types from a `ContributeAccess(reg)`, called from `serve.go` before the policy is installed. The Role Editor's matrix and the built-in roles are built from them.
- Commands' `when` context in the Editors view is now `{ can, tenantId }`: `can` is `useCan`'s check, read with `canIn(ctx, action, resource?)` from `src/session/permissions.ts`. The `admin` flag is gone. Tenants, Users, Roles, Moderation and the Log Explorer show by permission.
