# Foundation

Foundation mono-repo for Sgt Flame's Project Veritas, "The Right Answer".

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md) for the tech stack. Use `yarn`, never `npm`, for the frontend.

### Services, handlers and DTOs

Every backend service follows the same service → handler → DTO layout, with a matching frontend client. A feature `<f>` holds one or more services `<s>`; its main service is named after the feature (`<s>` = `<f>`). `kb` (Knowledge Base) is the reference, with two services: `kbservice` and `volumeservice`. For a service `<s>` with an operation `<Op>`:

**Service** - `backend/features/<f>/services/<s>service/`
- `Service.go` - the `Service` interface plus each operation's `<Op>Args` (embedding `features.ServiceArgs`), `<Op>Response` (with `Common features.ServiceResponse`) and, for async operations, `<Op>Request` holding the response channel. No implementation lives here.
- `impl/factory.go` - `CreateService(opts ...Option) <s>service.Service` returns a singleton built on first call. Configuration has defaults and is overridden only through `Option` functions named `With<Setting>` (e.g. `WithRoot`); options passed after the first call are ignored.
- `impl/<Op>.go` - one file per operation. `Common.Code` is an HTTP status code (`net/http` constants) and `Common.Message` the error text.

- Other files in `<s>service/` hold shared types and helpers other services may use, such as `kbservice/VolumePath.go` and `volumeservice/Marker.go`.

**Handlers** - `backend/features/<f>/rest/<s>handlers/`
- `register.go` - `Register(router gin.IRouter, service <s>service.Service)` stores the service on the handler struct and maps routes under `/<f>` (a secondary service under `/<f>/<resource>`, e.g. `/kb/volumes`); `router` is the `/api/v1` group, so routes are served at `/api/v1/<f>/...`.
- `<Op>.go` - one gin handler per operation: bind the request DTO (400 on failure), call the service with `req.ToArgs()`, and respond with `Common.Code` as the HTTP status and `<Op>ResponseFrom(resp)` as the body. Handlers only translate; logic belongs in the service.

**DTOs** - `backend/features/<f>/rest/<s>dto/`
- `<Op>Request.go` - the JSON request, with `ToArgs() <s>service.<Op>Args`.
- `<Op>Response.go` - the JSON response, with `<Op>ResponseFrom(<s>service.<Op>Response) <Op>Response`. The code travels as the HTTP status, not in the body.
- Types shared between DTOs (e.g. `VolumePath.go`, `FolderEntry.go`) get their own file with their own conversion. A DTO package may reuse another's shared types (`volumedto` uses `kbdto.VolumePath`).
- JSON fields are camelCase. Every `uint64` (IDs, TSIDs) is tagged `,string` so it travels as a JSON string. New IDs come from `features.NewTSID()`.

**Wiring** - `backend/cli/cmd/serve.go` creates each service with its `impl.CreateService(...)` (imported as `<s>impl`), then each handler, and calls `Register(api, service)` with the `/api/v1` router group.

**Frontend client** - `frontend/axiom/src/services/<Service>.ts`, one per backend service (`KnowledgeBase.ts` for `kbservice`, `Volume.ts` for `volumeservice`)
- Declares its own DTO interfaces mirroring the backend `<s>dto` JSON exactly, importing shared ones from the client that declares them (`Volume.ts` imports `VolumePath` from `KnowledgeBase.ts`). Every `uint64` is a `string` in TypeScript, since a JavaScript number cannot hold every uint64; never convert IDs to `number`.
- Exports one object named after the service with one method per handler (`KnowledgeBase.openVolume(request)`), returning a `Promise` of the response DTO and throwing `ServiceError` (status + message) on a non-2xx status.
- Requests go through the shared axios instance `http` from `src/services/http/client.ts` (e.g. `http.post<Resp>('/kb/volumes/open', request)`), whose baseURL is `API_BASE` (`VITE_API_BASE`, default `/api/v1`). In dev, `vite.config.ts` proxies `/api/v1/<f>` to the backend on `localhost:8080`; a new feature adds its own proxy entry, while a new service in an existing feature is already covered.
- `http` records every exchange into DevTools' Network tab by default. Opt out per request with `{ capture: false }` in the axios config, or per client with `createHttpClient({ capture: false })`.
- Components call these clients; they never `fetch` or use axios on the backend directly.

Adding an operation touches all five: service interface + `impl/<Op>.go`, handler `<Op>.go` + route in `register.go`, `<Op>Request.go`/`<Op>Response.go`, and a method + DTOs in the frontend client.

### Frontend: `frontend/axiom`

- `src/services/` - backend clients, one per backend service (see above); `src/services/http/` holds the shared axios client and the network capture.
- `src/logging/` - structured logging modelled on Go's `samber/oops`. Use the `log` builder (`log.In('kb').Tags('volume').With('volumeId', id).info('opened %s', name)`), never `console.*`; `Errorf`/`Wrap` build an `OopsError` carrying that context, and `log.fromError(err).error(...)` logs one. Records go to an in-memory ring buffer shown in DevTools' Log tab, and are mirrored to the console in dev builds.
- `src/tabs/<name>/` - tabs that are not editors, such as `devtools/` (`DevToolsTab`, a nested flexlayout with the Network and Log viewers, in the workbench's right border).

- `src/pages/` - routed pages (`/home`, `/login`, `/workbench`), wired up with `react-router` in `src/App.tsx`. A page with multiple one-use components gets its own subdirectory (e.g. `pages/workbench/`).
- `src/components/` - common components shared across pages or editors; one-use components live with the page or editor that uses them. `components/folders/` holds the folder-browsing pieces both volume editors use: `useFolderListing` (listing, errors, `run` an operation then reload), `FolderBreadcrumbs`, `NameDialog` and `ToolbarAction` (an icon button whose tooltip shows while disabled and which carries the title as its accessible name).
- `src/editors/<name>/` - one directory per editor, holding the editor component (`<Name>Editor.tsx`) and the object type it edits (`<Name>Object.ts`).

### Workbench

The workbench (`src/pages/workbench/`) is a `flexlayout-react` host with a `LeftMenuSidebar` of collapsible sections. Editors are never opened directly:

1. A sidebar icon creates an object (e.g. `new VolumesObject('volumes')`) and calls `EditorRegistry.edit(object)`.
2. `EditorRegistry` derives the tab id `<kind>:<id>`; if that tab is open it is focused, otherwise a new tab is added with the object in its `config`. The tab text is the object's `name` if it has one (else `<Editor name> <id>`), and its hover text the object's `helpText`.
3. `ComponentFactory.tsx`'s `createFactory(registry)` maps the tab's component (the object's `kind`) to its editor. Editors that open other editors (e.g. `VolumesEditor` opening a `VolumeEditor`) receive the `registry` as a prop.

Adding an editor: create `src/editors/<name>/`, add the object to the `EditableObject` union and `editorNames` in `EditorRegistry.ts`, add a case to `ComponentFactory.tsx`, and add an icon to a `LeftMenuSidebar` section.

Current sidebar sections:

- **One** - Foo (placeholder)
- **Two** - Bar (placeholder)
- **Knowledge** - Volumes: manages Knowledge Base Volumes. `VolumesEditor` browses the knowledge base root through the `Volume` client: double-click a folder to open it, breadcrumbs to go back, and toolbar actions to make a folder, remove an empty non-volume folder, or convert the selected folder to a volume. Double-clicking a volume does not enter it but opens it (`openVolume`, via `KnowledgeBase.openVolume`) in a `VolumeEditor`.

`VolumeEditor` (`src/editors/volume/`) edits a `VolumeObject`: id `volume-<tsid>`, plus the `tsid` and the `path` of folders from the root. Its tab text is the volume's folder name and its hover text the full path, the root being `/` (e.g. `/projects/veritas`). It browses the volume's subfolders and makes folders and files; the new-file dialog always asks for the file type, from `fileTypes` in `Volume.ts`. Double-clicking a markdown file opens it in a `MarkdownEditor` (`openMarkdown`, via `Collab.openFile`).

`MarkdownEditor` (`src/editors/markdown/`) edits a `MarkdownObject`: id `markdown-<fileId>`, where `fileId` is the file's collaboration room. It is a nested flexlayout (one `Model` per editor) with three views of one shared Yjs `Y.Text`: **Live Preview** (Obsidian-style, CodeMirror 6), **Source** (Monaco) and **WYSIWYG** (Milkdown); see `documentation/plans/markdown-editor/MarkdownEditor.md`. `collab/CollabSession.ts` owns the `Y.Doc`, awareness and connection (`start`/`stop` from `useCollabSession`); `MarkdownStatusBar` shows connection, save state and peers. Views are lazy-loaded chunks. **Source** (`source/SourceView.tsx`) bundles Monaco locally (`source/monacoSetup.ts`, no CDN) and binds it with `y-monaco`; `vite.config.ts` dedupes `monaco-editor`/`yjs` and aliases the `editor.api.js` path y-monaco imports, which monaco-editor 0.57's exports map no longer allows. **Live Preview** (`live/`) is CodeMirror 6 with `y-codemirror.next`: `livePreview.ts` hides markdown syntax on lines the selection is not on and renders checkboxes, bullets, links, images, rules and callouts in place; `obsidianSyntax.ts` adds `[[wikilinks]]` and `#tags` to the parser; `obsidianTheme.ts` styles it from the MUI theme. The two bindings publish cursors under different awareness fields (`cursor` vs `selection`); `collab/cursorBridge.ts` records which is current in `cursorFrom` and the provider copies it across for remote states, so cursors show in every view. **WYSIWYG** (`wysiwyg/`) is Milkdown (commonmark, gfm, history, listener) with a formatting toolbar. Milkdown's collab plugin needs a `Y.XmlFragment`, so `yTextBridge.ts` syncs through markdown instead: local edits go to the `Y.Text` as a diff-match-patch patch from the last synced serialization (so Milkdown's normalisation never rewrites text the user did not touch), and remote changes reload the document outside the undo history, keeping the cursor by textblock and offset. It shows no remote cursors.

### Volumes

A volume is a folder under the knowledge base root (`--kb-root`, default `~/.foundation/kb`) containing a `.volume.json` marker (`volumeservice.Marker`) with its TSID. Folder names cannot contain `.`, so the marker never collides with a folder and dotted entries are never listed. `kbservice.OpenVolume` returns the marker's ID. `RemoveFolder` only removes empty folders that are not volumes, so it never deletes content. `CreateFile` creates an empty file only inside a volume, of a type in `volumeservice.FileTypes` (currently only `markdown`, `.md`); the frontend's `fileTypes` mirrors that list. `ListFolder` returns folders and files of known types (`kbservice.SplitFile`). Error messages name folders relative to the root, never by server path.


### Collaboration

`collabservice` (`/kb/collab`) lets several users edit one volume file. `OpenFile` (`POST /kb/collab/files/open`) returns the file's room ID, the same for everyone; `GET /kb/collab/files/:fileId` upgrades to a WebSocket (`github.com/coder/websocket`) speaking the y-websocket protocol (sync 0, awareness 1) plus our control messages (100, JSON: `role`, `seeded`, `ready`, `save`, `saved`, `compact`; 101 compacted state). The server relays Yjs updates and keeps them as an opaque log; it never decodes the CRDT. The oldest connection is the **writer**: it seeds a fresh room from the file and autosaves (1s debounce); the server writes the file atomically via a dotted temp file. Empty rooms are dropped after 30s; connecting to a dropped room closes with 4404 and the file must be opened again. Services share the volume path helpers in `volumeservice/Files.go`. The frontend side is `src/services/Collab.ts` and `src/services/collab/CollabProvider.ts`; WebSocket traffic is logged under the `collab` domain, not captured in DevTools' Network tab.
