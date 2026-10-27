# Workbench context and client tools

## Context
Chat should know what the user is doing in the workbench, and be able to act there. For example, a user edits a contact, switches to Chat and types "Schedule an appointment with him for next week". To answer, the AI needs:
- **who "him" is**: the contact that was just being edited;
- **when it is ambiguous, the candidates**: if Curt and Mike were both open, it asks "Do you mean Curt or Mike?". After "Mike", it knows Mike's contact id;
- **a way to act**: a tool that books the appointment using Mike's default contact method.

This plan adds three pieces:
1. a **workbench context**: client state, kept in the browser, describing open editors and what each one is editing;
2. a **snapshot** of that state, sent with every chat message;
3. **client tools**: tools the model calls that run in the browser, to read more of that state or to act in the UI.

Contacts and appointments are an example domain: no plan defines them yet. The mechanism is generic, so any editor's object can be referred to this way.

Status: partly built. Slice 4 built the store, `useWorkbenchSubject`, the snapshot and the `workbench` context provider, on the proposals of questions 1, 2, 3 and 6; see [Built in slice 4](#built-in-slice-4). Slice 8 built the client tools `workbench.getState`, `workbench.open` and `chat.askChoice`, with their pause and resume; see [Built in slice 8](#built-in-slice-8).

## The workbench context store
- **One store per browser tab**, `src/workbench/context/WorkbenchContextStore.ts`, read through `useSyncExternalStore` like the DevTools stores.
  - A React context (`WorkbenchContextProvider` in `Workbench.tsx`) hands the store to editors. The context value is only the store, so a change in one editor does not re-render every editor; components subscribe to the slice they read.
- **What it holds**:

| Field | Source |
|---|---|
| `editors`: each open tab's id, kind, title, and its **subject** (below) | the flexlayout model, plus each editor's report |
| `focused`: the focused editor's tab id | flexlayout's active tab |
| `recent`: the last subjects the user focused or edited, newest first, with a time and what happened (`viewed`, `edited`, `created`) | the store records focus changes and editor reports |
| `selection`: per editor, an optional selection (text range, selected rows) | each editor's report |
| `surface`: which chat surface is sending (`main` or `document`) | the chat component |

- **Subjects**: what an editor is editing, as a typed reference the server can resolve:
  ```ts
  interface SubjectRef {
    type: string      // a resource type from RBAC.md: 'contact', 'markdown', 'volume'
    id: string        // TSID as a string
    label: string     // "Mike Ross"
    summary?: string  // one short line: "Contact, default method: email"
  }
  ```
- **Editors report** through one hook: `useWorkbenchSubject(tabId, subject, { selection, dirty })`. The hook updates the store when the object or selection changes and removes the entry when the tab closes. An editor that does not call it still appears, with its kind and title only.
- **Focus tracking needs no editor code**: `Workbench.tsx` listens to the layout model's selection changes and records them in `recent`. That is what makes "him" work: the contact editor was the last subject focused before Chat.
- **Not persisted**: the store lives as long as the browser tab. `recent` keeps the last 20 subjects (see the open questions).
- **Identity is not in the store**: the user and tenant come from the JWT on the server ([MultiTenant.md](MultiTenant.md)). The snapshot never carries them, so it can never claim to be someone else.

## The snapshot sent with a message
- **`Send` gains `workbench`**: a JSON snapshot built from the store when the message is sent:
  ```json
  {
    "surface": "main",
    "focused": "chat:main",
    "previous": "contact:1234",
    "editors": [
      { "tab": "contact:1234", "kind": "contact", "title": "Mike Ross",
        "subject": { "type": "contact", "id": "1234", "label": "Mike Ross" } },
      { "tab": "contact:5678", "kind": "contact", "title": "Curt Smith",
        "subject": { "type": "contact", "id": "5678", "label": "Curt Smith" } }
    ],
    "recent": [
      { "subject": { "type": "contact", "id": "1234", "label": "Mike Ross" }, "event": "edited", "at": "…" },
      { "subject": { "type": "contact", "id": "5678", "label": "Curt Smith" }, "event": "viewed", "at": "…" }
    ]
  }
  ```
  `previous` is the editor focused before the chat surface, which is usually what "this" or "him" refers to.
- **Small by default**: references, labels and selections only, never an editor's content. It is capped (about 4 KB), and older `recent` entries are dropped first. More detail is fetched on demand with client tools.
- **On the server**, a `chat.ContextProvider` with id `workbench` ([ChatCompletion.md](ChatCompletion.md)) renders the snapshot as a short "The user's workspace" section of the prompt.
  - The snapshot is **untrusted data**. Labels are quoted as data, never as instructions. Ids are hints: every tool that uses one still runs `authz.Check` ([RBAC.md](RBAC.md)), so a forged id gains nothing.
- **Kept with the round**: each `Round` stores the snapshot it was sent with. A later round, such as the "Mike" reply, can still resolve against the candidates the earlier question was asked about.
- **The Context toggle** in the composer's settings popover ([ChatCompletion.md](ChatCompletion.md)) turns the snapshot off for a conversation.

## Resolving references
Resolution is the model's job, guided by the mode's system prompt and the snapshot:
1. A clear referent (one recent contact) is used directly.
2. An ambiguous one (two contacts open, both recent) makes the model ask. It asks with the **`chat.askChoice`** client tool, which shows the candidates as buttons in the transcript ("Curt Smith", "Mike Ross") and still allows a typed answer. The tool returns the chosen `SubjectRef`, so the next step has the id, not just a name.
3. No referent in the workspace makes the model search with a server tool (`contacts.search` in the example), then confirm or ask.

The confirmation card for the write tool ([Tools.md](Tools.md)) names the resolved subject ("Schedule with **Mike Ross**, next Tuesday 10:00, by email"), so a wrong resolution is caught before anything is written.

## Client tools
A client tool is a `tools.Tool` whose `Run` happens in the browser.
- **Declaration**:
  - frontend: a `chat.clientTool` contribution `{ id (the name), title, description, input schema, effect, run(args, ctx) or Card }`;
  - backend: the browser declares its client tools' names and schemas with each message (built in slice 8; the plan had once per session), and the loop checks every call's arguments against the schema. The backend never runs them.
- **Effects**: client tools are `read` (`workbench.getState`) or `ui` (`workbench.open`, `chat.askChoice`). A new `ui` effect means "changes only what this user sees", and runs without confirmation. A client tool that changes data is not allowed: it calls a server tool or a service instead, so the server keeps the authority.
- **The round trip** reuses the pause that write-tool confirmation already needs:
  1. `tools.Loop` gets a client tool call and pauses the round.
  2. The SSE stream sends a `tool` event with the call, status `client` (built in slice 8; the plan had a `clientToolCall` event).
  3. The chat surface runs the contribution, or shows its card, and posts `POST /conversation/answer` with `{ roundId, callId, result }` or an error (the plan had `/ai/chat/rounds/:roundId/tool-results`).
  4. The loop resumes. If the result does not arrive in time (30 seconds by default, except `chat.askChoice`, which waits for the user), the call fails with a note to the model.
- **Where they can run**: only in rounds started from a chat surface with a browser attached. A Cognition graph that runs in the background ([Cognition.md](Cognition.md)) cannot use them; the graph validator rejects client tools in graphs marked as background.
- **Tracing**: client tool calls are `ToolCall` records like any other, and the browser logs them under the round's `X-Trace-Id` ([Logging.md](Logging.md)).

**Candidate client tools**

| Tool | Effect | Does |
|---|---|---|
| `workbench.getState` | read | the full store, or one editor's subject and selection |
| `editor.getSelection` | read | the selected text or rows of an editor |
| `workbench.open` | ui | opens a subject in its editor, through `EditorRegistry.edit` |
| `workbench.focus` | ui | focuses an open editor |
| `chat.askChoice` | ui | asks the user to pick from candidates, returns the chosen one |

## A mode that uses it
Modes are Cognition graphs ([Cognition.md](Cognition.md)). An **Assistant** mode is a graph whose `Agent` node:
- enables the `workbench` context provider;
- allows the client tools and the domain's server tools;
- has a system prompt saying how to resolve references and to ask when unsure.

Other modes can opt in to any part of this. A plain Q&A mode can leave client tools off and still use the snapshot.

## Relation to other plans
- [Contributors.md](Contributors.md): new frontend points `chat.clientTool` and `workbench.subjectDescriber` (for editors that cannot use the hook, such as one built by another feature); the `workbench` context provider.
- [ChatCompletion.md](ChatCompletion.md): `Send` gains `workbench`; the Context toggle controls it; `Round` stores the snapshot.
- [Tools.md](Tools.md): a tool has a location (`server` or `client`), the `ui` effect, and the pause and resume protocol shared with confirmation.
- [RBAC.md](RBAC.md): subject types are RBAC resource types; ids from the snapshot are always checked.
- [Cognition.md](Cognition.md): client tools are allowed only in graphs that run attached to a chat surface.

## Built in slice 4
**Store** (`src/workbench/context/`)
- **`WorkbenchContextStore.ts`**: the store, one per browser tab (`workbenchContext`).
  - It holds `editors`, `focused`, `focusOrder` (most recent first) and `recent`.
  - `snapshot(surface, surfaceTab)` builds what Chat sends.
- **`WorkbenchContextProvider.tsx`**: wraps the workbench.
- **`WorkbenchContext.ts`**: the hooks `useWorkbenchContext`, `useWorkbenchState` and `useWorkbenchSubject(tabId, subject, { selection, dirty })`. Editors get their `tabId` from `EditorProps` ([Contributors.md](Contributors.md#built-in-slice-4)).
- **Focus tracking**: `Workbench.tsx` registers a flexlayout change listener that passes the editor tabs and the focused one to `setLayout`. Border tabs (DevTools, Settings) are left out. Focusing a tab whose editor reports a subject records `viewed`.
- **Recent**
  - It keeps 20 subjects.
  - Repeated events of one kind for one subject keep a single entry, moved to the front.
  - It is not persisted.
- **What the editors report**

| Editor | Subject | Selection |
|---|---|---|
| Markdown | `markdown`, the file id, the file name, the path | the selected lines in Live Preview or Source (`CollabSession.setSelection`) |
| Volume | `volume`, the TSID, the folder name, the current path | the selected entry |
| Volumes | `folder`, the path as id | the selected folder |
| Log Explorer | `logQuery`, the tab id, the query | – |

  - The Markdown editor also reports `dirty` while a save is pending.
  - It records `edited` when a local view changes the text. Seeding and remote updates have no origin and are ignored, and edits are recorded at most every 5 seconds.
  - `created` is not used yet.
- **The snapshot**
  - `previous` is the most recently focused tab other than the chat surface.
  - It is cut to about 4 KB: first the oldest recent entries are dropped, then the editors that are neither focused nor previous.
  - It is sent only while the conversation's **Context** control is on. That control is a `chat.contextSettings` contribution in `src/workbench/context/contributions.ts`, on by default.
- **On the server**: `features/workbench/context` (`workbenchcontext`) registers the `workbench` chat.contextProvider, with defaults `{"enabled": true}`.
  - It renders the snapshot as a short list: the editor focused before the chat, the other open editors, and the recent subjects.
  - Every label and summary is quoted with `strconv.Quote`. The unquoted fields (type, id, kind, event, time) are cut to one short token, so no field can carry instructions.
  - `Send` rejects a snapshot over 16 KB (`workbench_too_large`) or one that is not valid JSON, and stores it on the round.
- **Verified in the browser**: with Volumes open and then Chat, the stored snapshot has `previous: "volumes:volumes"` and the Volumes subject.

**Still open**: questions 1 (snapshot size), 2 (recent history), 3 (editor content) and 6 (several browser tabs) are built on their proposals and wait for a decision after seeing them. Question 5 (the example domain) was answered before slice 8; question 4 (asking) was built on its proposal in slice 8.

**Not yet**
- `workbench.subjectDescriber`.
- The `document` surface.

## Built in slice 8
The client tools, with the server side in [Tools.md › Built in slice 8](Tools.md#built-in-slice-8).
- **`chat.clientTool`** (`editors/chat/points.ts`): `{ title, description, input, effect: 'read' | 'ui', run?(args, ctx), Card? }`, where `ctx` is `{ registry, store, tabId }`.
  - A tool with a `Card` is declared as waiting for the user, and its card in the transcript answers it.
  - The rest run as soon as their `tool` event arrives; `useChatStream` answers with the result, or the error's message.
- **The tools** (`src/tools/contributions.ts`):
  - `workbench.getState` (read): the open editors with their subjects and selections, the focus order (10) and the recent subjects, or one tab's entry.
  - `workbench.open` (ui): focuses an open tab (`EditorRegistry.focus`, new), opens a Volume file with its lines selected (`openMarkdownAt`), or opens a Chronicle entry. It also stands in for the planned `workbench.focus`.
  - `chat.askChoice` (ui): the question and 2 to 8 choices `{label, value?, subject?}` as buttons, and a field for another answer. It returns `{choice, value, subject}` or `{text}`.
- **Declared with each message** while the conversation's Tools switch is on. The server accepts only `read` and `ui` client tools, at most 20.
- **Verified** in headless Chromium against a stand-in for the Gemini API: *Ask you to choose* answered with a button, and `workbench.open` opened the file with its lines.
- **Not yet**: `editor.getSelection`; refusing client tools in background graphs (no graph can call them yet: the `Agent` node has none).

## Open questions
1. **Snapshot size**: always send the small snapshot and fetch the rest with `workbench.getState`, or send everything every time? The proposal is the small snapshot plus the tool. *Decision*:
2. **Recent history**: how many subjects, and should it survive a reload (kept per user on the server)? The proposal is 20, not persisted. *Decision*:
3. **Editor content**: should the snapshot ever include unsaved form values (a half-edited contact), or only references? The proposal is references only; content through `workbench.getState`. *Decision*:
4. **Asking**: buttons through `chat.askChoice`, or a plain text question? The proposal is buttons, with a typed answer still allowed. *Decision*: (after seeing) built on the proposal in slice 8, with the typed answer in the card rather than the composer.
5. **The example domain**: are contacts, appointments and a calendar a planned feature (a CRM), or only an illustration? *Decision*: **only an illustration**. There are no contact tools, and the client tools stay generic over any editor's subject.
6. **Several browser tabs**: each tab has its own store. Should a chat in one tab see editors open in another? The proposal is no. *Decision*:
