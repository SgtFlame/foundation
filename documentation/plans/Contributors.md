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

Status: partly built. Slice 1 built the registry on both sides, `workbench.borderTab` and `configuration`; see [Built in slice 1](#built-in-slice-1).

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
- `tools.Tool` ([Tools.md](Tools.md));
- `cognition.Node`, the node types of the Cognition Engine ([Cognition.md](Cognition.md));
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

## Migration path
1. Add the registry and the two new points (`configuration`, `chat.contextSettings`) along with the Settings tab and the Chat UI. *The registry and `configuration` are built; `chat.contextSettings` comes with the Chat UI.*
2. Convert `workbench.borderTab`: DevTools and Settings become contributions. *Built: DevTools in slice 1, Settings in slice 2.*
3. Convert `workbench.editor` and `workbench.sideView`. This removes the switch, the union and the hand-written sidebar.
4. Later, runtime plugins: a manifest listing a bundle URL. Loading it with `import()` calls the same `contribute` API. This needs signing or trust decisions before it is built.

## Open questions
1. **Runtime plugins**: do third-party bundles loaded at runtime ever matter? If so, plugin signing and trust need a design first. *Decision* (can wait):
