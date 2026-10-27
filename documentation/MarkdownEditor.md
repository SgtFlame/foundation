# Markdown Editor: three collaborative views of one file

## Context
Volumes can create `.md` files (`Volume.createFile`), but nothing can open or edit them. We want a **Markdown editor** tab in the workbench. It is a nested flexlayout (like `src/tabs/devtools/DevToolsTab.tsx`) with three tabs, each a different view of the **same** file:
1. **Live Preview**: Obsidian-style, CodeMirror 6.
2. **Source**: Monaco, editing the raw markdown.
3. **WYSIWYG**: Milkdown.

Editing is **multi-user and autosaved** with the common standard for shared text editing: a **Yjs CRDT** synced over **WebSocket** using the y-websocket wire protocol (y-protocols sync + awareness). All three views bind to one `Y.Text`, so edits in any view, in any browser, show up everywhere. Remote cursors come from Yjs awareness.

**Build order:** Common, then B (Monaco), then A (Live Preview), then C (WYSIWYG). Monaco has a native Yjs binding, so it proves the collab pipeline with the least code. Implement one section per session. When implementation starts, copy this plan to `documentation/MarkdownEditor.md`, which is the repo convention.

---

# COMMON (backend service, WebSocket, session, host editor)
**Status: done.**


## Design decisions
- **The Go server is a Yjs relay, not a CRDT engine.** No Go Yjs implementation is mature enough to depend on. The server understands the protocol *framing*, but it never decodes CRDT contents:
  - It stores each room's update log, a list of opaque `[]byte` updates.
  - It answers a client's `syncStep1` with `syncStep2`, which carries the first log entry or the empty update `[0,0]`, followed by the remaining entries as `update` messages. Yjs updates are idempotent, so duplicates are harmless.
  - On connect it sends `syncStep1` with an empty state vector `[0]`, so a reconnecting client pushes any offline edits.
  - It relays `update` and `awareness` messages to the other connections.
- **One elected writer per room** does the seeding and the saving. That avoids both the double-seed race and save conflicts.
  - Seeding: the first connection becomes `writer`. It gets a `role` control message with `seed` (the file's markdown), inserts it into `Y.Text`, and replies `seeded`. The server then broadcasts `ready`. Editors stay read-only until `ready`.
  - Autosave: the writer debounces 1s after any doc change, then sends `save {markdown}`. The server writes the file atomically: a temp file `.<name>.md.tmp` (dotted names are never listed), then a rename. It broadcasts `saved {code, message, at}` to everyone.
  - If the writer disconnects, the oldest remaining connection becomes writer. If the room is dirty it saves right away. If the writer leaves before seeding, seeding moves to the new writer.
  - When the last connection leaves, the room is dropped after a 30s grace period, so a page refresh does not lose it. The `.md` file stays the source of truth.
- **Compaction:** when the log has more than 500 entries or more than 1MB, the server sends `compact` to the writer. The writer replies with `Y.encodeStateAsUpdate(doc)`, which replaces the log.
- **Awareness:** the server keeps the last awareness entry per client. A joiner receives them all. When a connection closes, the server broadcasts its clientIDs with state `null` and `clock+1`. This needs a small Go decoder for awareness updates: varUint count, then per client (clientID, clock, JSON string).
- **Out of scope for now:** detecting external changes to the file on disk (fsnotify later), auth/identity (awareness uses a local guest name), and capturing WebSocket traffic in DevTools (the provider logs through `log` instead).

## Wire protocol (binary, lib0 encoding)
| type | name | direction | payload |
|---|---|---|---|
| 0 | sync | both | varUint subtype (0 step1 / 1 step2 / 2 update) + varUint8Array |
| 1 | awareness | both | varUint8Array awareness update |
| 3 | queryAwareness | c→s | none |
| 100 | control | both | varString JSON: `role{writer,seed?,ready}`, `seeded`, `ready`, `save{markdown}`, `saved{code,message,at}`, `compact` |
| 101 | compacted | c→s | varUint8Array full-state update |

## Backend: new service `collabservice` in feature `kb` (follows the CLAUDE.md layout)
- **Shared helpers:** move the file-path logic out of the private methods in `backend/features/kb/services/volumeservice/impl/` (`resolve`, `statFolder`, `insideVolume`, `isVolume`, `rel`, `fail`, `ok`) into a new exported file `volumeservice/Files.go`. It would hold `ResolveFile(root, path, type)`, `InsideVolume(root, dir)`, `Rel(root, p)` and so on. The volumeservice impl then calls them, and its behaviour doesn't change.
- `backend/features/kb/services/collabservice/Service.go`
  - `Service { OpenFile(OpenFileArgs) OpenFileResponse; Collaborate(CollaborateArgs) CollaborateResponse }`
  - `OpenFileArgs{ServiceArgs, Path kbservice.VolumePath, Type string}` and `OpenFileResponse{Common, FileID uint64}`.
  - `Conn interface { Read(ctx) ([]byte, error); Write(ctx, []byte) error }`, so the service doesn't depend on a WebSocket library.
  - `CollaborateArgs{ServiceArgs, Ctx context.Context, FileID uint64, Conn Conn}` and `CollaborateResponse{Common}`.
- `collabservice/impl/`
  - `factory.go`: `CreateService(opts...)` as a singleton, with option `WithRoot`.
  - `service.go`: rooms are kept by id and by absolute path, behind a mutex.
  - `OpenFile.go`: validates the file the same way `CreateFile` does (inside a volume, of a known type, and it must exist, otherwise 404). It returns the existing room's id for that path, or a new one from `features.NewTSID()`, so every user lands in the same room.
  - `Collaborate.go`: looks up the room (404 if missing), joins it, runs the read loop until the connection closes, then leaves. All the logic lives here and in `room.go`.
  - `room.go`: the room's connections, log, awareness map, writer, `seeded`/`dirty` flags, writer election, save, compaction, and the grace-period teardown.
  - `protocol.go`: lib0 varUint/varString/varUint8Array encoding and decoding, the message constants, and the awareness decode/encode.
  - `protocol_test.go`: round-trip tests for the encoding, plus a room test with two fake `Conn`s that checks seed → relay → save → writer handover.
- **Handlers** `backend/features/kb/rest/collabhandlers/`
  - `register.go`: route group `/kb/collab`, with `POST /files/open` and `GET /files/:fileId`. The second is the WebSocket upgrade, served at `wss://…/api/v1/kb/collab/files/:fileId`.
  - `OpenFile.go`: the standard bind → service → respond pattern.
  - `Collaborate.go`: parses `fileId`, upgrades the connection with `github.com/coder/websocket` (`websocket.Accept`, same-origin by default), and adapts it to `collabservice.Conn` using binary messages. It calls `Collaborate`, which blocks, and closes with status `4000+Common.Code` and `Common.Message`. Handlers only translate.
- **DTOs** `backend/features/kb/rest/collabdto/`: `OpenFileRequest{tenant,path kbdto.VolumePath,type}` with `ToArgs()`, and `OpenFileResponse{message, fileId ,string}`.
- **Wiring** in `backend/cli/cmd/serve.go`: `collabimpl.CreateService(collabimpl.WithRoot(...))` with the `--kb-root` flag, and `collabhandlers.CollabHandler{}.Register(api, svc)`.
- **Dependency:** `go get github.com/coder/websocket`.

## Frontend: shared pieces (`frontend/axiom`)
- **Dependencies** (`yarn add`): `yjs`, `y-protocols`, `lib0`.
- **Vite:** in `vite.config.ts`, change the proxy entry to `'/api/v1/kb': { target: 'http://localhost:8080', ws: true }`.
- `src/services/Collab.ts`: the client for `collabservice`. It has DTOs that mirror `collabdto` (`fileId: string`) and `Collab.openFile(request)` through `http`. It also has `Collab.socketUrl(fileId)`, which builds a `ws(s)://` URL from `API_BASE` and `location`.
- `src/services/collab/CollabProvider.ts`: our own small y-websocket-compatible provider, about 150 lines, built on `y-protocols/sync` and `y-protocols/awareness`. We write our own rather than use `y-websocket`, so we own the control messages without relying on that library's internals.
  - It handles connect/reconnect with backoff, sync, awareness, and control-message callbacks (`onRole`, `onReady`, `onSaved`, `onCompact`).
  - It logs via `log.In('collab')`.
- **Editor** `src/editors/markdown/`
  - `MarkdownObject.ts`: `kind='markdown'`. It holds `id = markdown-<fileId>`, `fileId`, `path` (the folders plus the file name), `type`, `name = <file>.md` and `helpText = /a/b/<file>.md`.
  - `openMarkdown.ts`: the same shape as `editors/volume/openVolume.ts`. It calls `Collab.openFile` and then `registry.edit(new MarkdownObject(...))`.
  - `collab/CollabSession.ts`
    - It owns the `Y.Doc`, `ytext = doc.getText('markdown')`, the `Awareness` and the `CollabProvider`.
    - It keeps a store `{status: connecting|syncing|ready|offline, writer, saving, lastSavedAt, saveError, peers}` with `subscribe`/`snapshot`, matching `RingBuffer` in `src/logging/LogStore.ts`, so `useStore` (in `src/tabs/devtools/useStore.ts`) works on it.
    - Writer duties: seeding, debounced autosave, the compact reply, and a final save on `pagehide` or when the session is destroyed.
  - `collab/localUser.ts`: a guest name and colour kept in `localStorage`, wrapped in try/catch. It is set as the awareness `user` field.
  - `collab/useCollabSession.ts`: creates the session in an effect and destroys it on cleanup, which is safe under StrictMode.
  - `MarkdownEditor.tsx`
    - A **per-instance** `Model` (`useMemo(() => Model.fromJson(json))`) with one tabset holding tabs `live` "Live Preview", `source` "Source" and `wysiwyg` "WYSIWYG". `tabEnableClose` is false.
    - The factory passes `session` to each view. A view that isn't built yet renders a "Not implemented yet" placeholder.
    - Views show a spinner until the status is `ready`.
  - `MarkdownStatusBar.tsx`: a connection chip, Saving…/Saved hh:mm/error text, and peer avatars from awareness.
- **Registration**
  - `src/pages/workbench/EditorRegistry.ts`: add `MarkdownObject` to `EditableObject` and `markdown: 'Markdown'` to `editorNames`.
  - `src/pages/workbench/ComponentFactory.tsx`: add `case 'markdown'`.
  - `src/editors/volume/VolumeEditor.tsx`: double-clicking a `markdown` file calls `listing.run(() => openMarkdown(registry, [...path, entry.name], entry.type))`.
- **Docs:** update `CLAUDE.md` (collab service, WebSocket route, Markdown editor, and the double-click-to-open behaviour).

## Common verification
- `cd backend && go vet ./... && go test ./features/kb/...`, which runs the protocol and room tests.
- `cd frontend/axiom && yarn build && yarn lint`.
- Manual test:
  - Run `serve` and `yarn dev`, create a volume and a file, and double-click it. The Markdown tab should open with the status "ready".
  - Open the same file in a second browser window. Both should show two peers.
  - Kill the backend. The status should go to offline, then reconnect once the backend is back.

---

# SECTION A: Obsidian Live Preview (CodeMirror 6)
**Status: done.** Added `collab/cursorBridge.ts`: y-codemirror (`cursor`) and y-monaco (`selection`) use different awareness fields, so each client marks the current one in `cursorFrom` and the provider maps remote states. Links open on Ctrl/Cmd-click; wikilinks are styled only.

- **Dependencies:** `@codemirror/state`, `@codemirror/view`, `@codemirror/commands`, `@codemirror/language`, `@codemirror/lang-markdown`, `@codemirror/language-data`, `@lezer/markdown`, `y-codemirror.next`.
- `src/editors/markdown/live/LivePreviewView.tsx`
  - It creates an `EditorView` in a ref'd div with: `markdown({ base: markdownLanguage, codeLanguages: languages, extensions: [GFM, wikiLink, tag] })`, `yCollab(session.ytext, session.awareness, { undoManager: new Y.UndoManager(ytext) })`, `livePreview()`, `obsidianTheme(muiTheme)` and `EditorView.lineWrapping`.
  - It destroys the view on unmount.
- `live/livePreview.ts`: a `ViewPlugin` that builds `Decoration`s from `syntaxTree` over the visible ranges, and rebuilds on doc, selection or viewport changes.
  - It hides syntax marks (`HeaderMark`, `EmphasisMark`, `CodeMark`, `LinkMark`, `URL`, `QuoteMark`) on any line that doesn't hold the selection. This is Obsidian's "reveal on cursor" behaviour.
  - It adds line classes for headings (`cm-h1`…`cm-h6`), blockquotes and code blocks.
  - It replaces syntax with widgets: task checkboxes (clicking dispatches the `[ ]`/`[x]` toggle), `---` as an `<hr>`, and `![alt](src)` as an inline image.
- `live/obsidianSyntax.ts`: inline-parser extensions for `@lezer/markdown`:
  - `[[Target|alias]]` wikilinks, rendered as a link chip.
  - `#tag`, rendered as a pill.
  - `> [!note] Title` callouts, rendered as a styled block with a line decoration and the type's colour and icon.
  - Wikilinks are styled only. Navigating to the target is a follow-up.
- `live/obsidianTheme.ts`: `EditorView.theme` built from the MUI palette, with light and dark variants. It sets an Obsidian-like readable line width (about 750px, centred), heading sizes, muted mark colours, and remote cursor colours from awareness.
- **Verification:**
  - Type headings, emphasis, tasks, `[[link]]`, `#tag` and a callout. Marks should hide when the cursor leaves a line, and checkboxes should toggle.
  - The second window should show the edits and a remote cursor, and the Source tab should match.

# SECTION B: Source (Monaco)
**Status: done.** Monaco 0.57 required a Vite alias for y-monaco's `monaco-editor/esm/vs/editor/editor.api.js` import; SourceView is a lazy chunk.

- **Dependencies:** `monaco-editor`, `@monaco-editor/react`, `y-monaco`.
- `src/editors/markdown/source/monacoSetup.ts`
  - `loader.config({ monaco })` bundles Monaco locally, with no CDN.
  - `self.MonacoEnvironment = { getWorker: () => new EditorWorker() }`, using `import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'`.
  - In `vite.config.ts`, add `resolve.dedupe: ['monaco-editor', 'yjs']`, so `y-monaco` and the editor share one instance.
- `source/SourceView.tsx`
  - `<Editor language="markdown" path={`inmemory://markdown/${fileId}.md`} theme={mui dark ? 'vs-dark' : 'vs'} options={{ wordWrap: 'on', minimap: { enabled: false }, automaticLayout: true }} />`.
  - In `onMount`: `new MonacoBinding(session.ytext, editor.getModel(), new Set([editor]), session.awareness)`, destroyed on unmount.
  - The editor is read-only until the session status is `ready`.
- `source/remoteCursors.ts`: listens to awareness changes and injects per-client CSS rules for `.yRemoteSelection-<id>` and `.yRemoteSelectionHead-<id>` in the user's colour, with a name label via `::after`.
- **Verification:**
  - Edits in Source show up in the second window within about 100ms.
  - About 1s after typing stops, the `.md` file on disk matches.
  - The status bar shows "Saved".
  - Closing the writer's window hands writing over to the other window, and saving continues.

# SECTION C: WYSIWYG (Milkdown)
**Status: done.** Local edits are pushed from Milkdown's own debounced `markdownUpdated` (200ms) rather than an extra 300ms timer; a pending local edit is flushed before a remote reload. Remote reloads replace the document with `addToHistory: false` instead of `replaceAll`, so undo never reverts other people's edits. Milkdown has no task-list command, so the toolbar has none (typing `[ ] ` in a list item still makes one), and task checkboxes are display-only.

- **Dependencies:** `@milkdown/kit` (core, `preset-commonmark`, `preset-gfm`, `plugin-listener`, `plugin-history`), `@milkdown/react` and `diff-match-patch` (plus `@types/diff-match-patch`).
- **Why a bridge:** Milkdown's own collab plugin binds to a `Y.XmlFragment`, which would split the document away from the `Y.Text` the other two views share. So this view syncs with `Y.Text` through markdown text.
- `src/editors/markdown/wysiwyg/yTextBridge.ts`
  - **Remote → editor:** on `ytext.observe` where the transaction origin isn't the bridge, debounce 100ms. If the markdown differs from `lastApplied`, apply it with Milkdown's `replaceAll(markdown)`. Keep the selection as best we can by mapping the textblock index and offset. Then set `baseline = serialize(editor)`.
  - **Editor → remote:** on the `markdownUpdated` listener, debounce 300ms.
    1. Build `patches = dmp.patch_make(baseline, newMarkdown)`. These patches hold only the user's real edit, so the serializer's normalisation (for example `*` vs `-`) doesn't overwrite text it didn't touch.
    2. Apply them to the live text: `result = dmp.patch_apply(patches, ytext.toString())`.
    3. Write the minimal `dmp.diff_main(current, result)` into `ytext` as delete/insert ops in `doc.transact(…, BRIDGE_ORIGIN)`.
    4. `baseline = newMarkdown`.
- `wysiwyg/WysiwygView.tsx`: `<MilkdownProvider>` + `useEditor` with commonmark, gfm, history and listener. The editor is set up with `ytext.toString()` and the bridge is attached on mount. Styles come from MUI typography in an `sx` wrapper, with no Milkdown theme package.
- `wysiwyg/WysiwygToolbar.tsx`: `ToolbarAction` buttons (from `src/components/folders/ToolbarAction.tsx`) for bold, italic, inline code, H1–H3, bullet/ordered/task list, quote, link and table, each calling Milkdown's `callCommand(...)`.
- **Known limits:** this view shows no remote cursors, and the local cursor may shift when someone edits the same paragraph at the same moment.
- **Verification:**
  - Format text with the toolbar. The Source tab should show the right markdown, and untouched list markers and emphasis styles should stay unchanged.
  - Edit at the same time in two windows (one WYSIWYG, one Source). Both should converge, and the file on disk should match after autosave.
