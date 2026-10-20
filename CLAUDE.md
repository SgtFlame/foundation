# Foundation

Foundation mono-repo for Sgt Flame's Project Veritas, "The Right Answer".

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md) for the tech stack. Use `yarn`, never `npm`, for the frontend.

### Services, handlers and DTOs

Every backend feature follows the same service → handler → DTO layout, with a matching frontend client. `kb` (Knowledge Base) is the reference; for a feature `<f>` with an operation `<Op>`:

**Service** - `backend/features/<f>/services/<f>service/`
- `Service.go` - the `Service` interface plus each operation's `<Op>Args` (embedding `features.ServiceArgs`), `<Op>Response` (with `Common features.ServiceResponse`) and, for async operations, `<Op>Request` holding the response channel. No implementation lives here.
- `impl/factory.go` - `CreateService(opts ...Option) <f>service.Service` returns a singleton built on first call. Configuration has defaults and is overridden only through `Option` functions named `With<Setting>` (e.g. `WithRoot`); options passed after the first call are ignored.
- `impl/<Op>.go` - one file per operation. `Common.Code` is an HTTP status code (`net/http` constants) and `Common.Message` the error text.

**Handlers** - `backend/features/<f>/rest/<f>handlers/`
- `register.go` - `Register(router gin.IRouter, service <f>service.Service)` stores the service on the handler struct and maps routes under `/<f>`; `router` is the `/api/v1` group, so routes are served at `/api/v1/<f>/...`.
- `<Op>.go` - one gin handler per operation: bind the request DTO (400 on failure), call the service with `req.ToArgs()`, and respond with `Common.Code` as the HTTP status and `<Op>ResponseFrom(resp)` as the body. Handlers only translate; logic belongs in the service.

**DTOs** - `backend/features/<f>/rest/<f>dto/`
- `<Op>Request.go` - the JSON request, with `ToArgs() <f>service.<Op>Args`.
- `<Op>Response.go` - the JSON response, with `<Op>ResponseFrom(<f>service.<Op>Response) <Op>Response`. The code travels as the HTTP status, not in the body.
- Types shared between DTOs (e.g. `VolumePath.go`) get their own file with their own conversion.
- JSON fields are camelCase. Every `uint64` (IDs, TSIDs) is tagged `,string` so it travels as a JSON string.

**Wiring** - `backend/cli/cmd/serve.go` creates each service with `impl.CreateService(...)`, then each handler, and calls `Register(api, service)` with the `/api/v1` router group.

**Frontend client** - `frontend/axiom/src/services/<Feature>.ts` (e.g. `KnowledgeBase.ts`)
- Declares its own DTO interfaces mirroring the backend `<f>dto` JSON exactly. Every `uint64` is a `string` in TypeScript, since a JavaScript number cannot hold every uint64; never convert IDs to `number`.
- Exports one object named after the feature with one method per handler (`KnowledgeBase.openVault(request)`), returning a `Promise` of the response DTO and throwing `ServiceError` (status + message) on a non-2xx status.
- Paths are relative to `API_BASE` (`VITE_API_BASE`, default `/api/v1`). In dev, `vite.config.ts` proxies `/api/v1/<f>` to the backend on `localhost:8080`; a new feature adds its own proxy entry.
- Components call these clients; they never `fetch` the backend directly.

Adding an operation touches all five: service interface + `impl/<Op>.go`, handler `<Op>.go` + route in `register.go`, `<Op>Request.go`/`<Op>Response.go`, and a method + DTOs in the frontend client.

### Frontend: `frontend/axiom`

- `src/services/` - backend clients, one per feature (see above).

- `src/pages/` - routed pages (`/home`, `/login`, `/workbench`), wired up with `react-router` in `src/App.tsx`. A page with multiple one-use components gets its own subdirectory (e.g. `pages/workbench/`).
- `src/components/` - common components shared across pages only; one-use components live with the page that uses them.
- `src/editors/<name>/` - one directory per editor, holding the editor component (`<Name>Editor.tsx`) and the object type it edits (`<Name>Object.ts`).

### Workbench

The workbench (`src/pages/workbench/`) is a `flexlayout-react` host with a `LeftMenuSidebar` of collapsible sections. Editors are never opened directly:

1. A sidebar icon creates an object (e.g. `new VolumesObject('volumes')`) and calls `EditorRegistry.edit(object)`.
2. `EditorRegistry` derives the tab id `<kind>:<id>`; if that tab is open it is focused, otherwise a new tab is added with the object in its `config`.
3. `ComponentFactory.tsx` maps the tab's component (the object's `kind`) to its editor.

Adding an editor: create `src/editors/<name>/`, add the object to the `EditableObject` union and `editorNames` in `EditorRegistry.ts`, add a case to `ComponentFactory.tsx`, and add an icon to a `LeftMenuSidebar` section.

Current sidebar sections:

- **One** - Foo (placeholder)
- **Two** - Bar (placeholder)
- **Knowledge** - Volumes: manages Knowledge Base Volumes, the root directories of the knowledge base

