# Workbench context: what remains

The workbench context store, `useWorkbenchSubject`, the snapshot, the `workbench` context provider and the client tools `workbench.getState`, `workbench.open` and `chat.askChoice` are built; see [WorkbenchContext.md](../WorkbenchContext.md).

## The `document` surface
The Markdown Editor's Chat tab ([ChatCompletion.md](ChatCompletion.md)) sends the snapshot with `surface: "document"`, and its Context control is forced on.

## More of the store
- **`workbench.subjectDescriber`**: a frontend point that describes what an editor kind is editing, for editors that cannot call `useWorkbenchSubject`, such as one built by another feature ([Contributors.md](Contributors.md)).
- **The `created` event** in `recent`, for subjects the user creates.

## More client tools
| Tool | Effect | Does |
|---|---|---|
| `editor.getSelection` | read | the selected text or rows of an editor |

(`workbench.focus` was planned too; `workbench.open` focuses an open tab, so it stands in.)

## Where client tools may run
Only in rounds started from a chat surface with a browser attached. A Cognition graph that runs in the background ([Cognition.md](Cognition.md)) cannot use them: the graph validator rejects client tools in graphs marked as background. No graph can call them yet (the `Agent` node has none).

## A mode that uses it
Modes are Cognition graphs ([Cognition.md](Cognition.md)). An **Assistant** mode is a graph whose `Agent` node:
- enables the `workbench` context provider;
- allows the client tools and the domain's server tools;
- has a system prompt saying how to resolve references and to ask when unsure.

Other modes can opt in to any part of this. A plain Q&A mode can leave client tools off and still use the snapshot.

## TODO
### Open questions
1. **Snapshot size**: always send the small snapshot and fetch the rest with `workbench.getState`, or send everything every time? The proposal is the small snapshot plus the tool. *Decision*: (after seeing) built on the proposal in slice 4.
2. **Recent history**: how many subjects, and should it survive a reload (kept per user on the server)? The proposal is 20, not persisted. *Decision*: (after seeing) built on the proposal in slice 4.
3. **Editor content**: should the snapshot ever include unsaved form values, or only references? The proposal is references only; content through `workbench.getState`. *Decision*: (after seeing) built on the proposal in slice 4.
4. **Asking**: buttons through `chat.askChoice`, or a plain text question? The proposal is buttons, with a typed answer still allowed. *Decision*: (after seeing) built on the proposal in slice 8, with the typed answer in the card rather than the composer.
6. **Several browser tabs**: each tab has its own store. Should a chat in one tab see editors open in another? The proposal is no. *Decision*: (after seeing) built on the proposal in slice 4.

(Question 5, the example domain, is decided: only an illustration.)

### Not built yet
- The `document` surface.
- `workbench.subjectDescriber` and the `created` event.
- `editor.getSelection`.
- Refusing client tools in background graphs.
- The Assistant mode (with modes, [ChatCompletion.md](ChatCompletion.md)).
