# Workspaces

A **workspace** is a context a person switches to: the editors they have open and how they are laid out, the Volumes and Chronicle entries that are active, and the chats that belong to it. It is planned in forge's [016-Workspaces.md](../../forge/documentation/plans/016-Workspaces.md) and built in parts. Parts 16a (the records and the menu), 16b (the editors and layout), 16c (the active Volumes and Chronicle entries) and 16d (the chats) are built.

## Built in forge 16a

### The record
- `features/workspace` keeps a person's workspaces as CQRS state on the live core, on **its own area `ws`** (stream `EVT_<tenant>_ws`), folded by the projector `workspace.workspaces`.
- Each workspace is `workspace.Workspace {id, name, isDefault, position, layout, context, version, createdAt, updatedAt}`.
  - `layout` (16b) and `context` (16c) are JSON that those parts define.
  - `version` counts the changes: the fold raises it with every event.
- Workspaces are **not definitions**. Their commands are not ops of the operation log, have no undo, and are not a person's activity.
- **One person's workspaces are theirs alone.** The target type is `ws_<userId>`, made from the principal by the new `live.Command.TypeFor` ([Live.md › Changed in forge 16a](Live.md#changed-in-forge-16a)).
  - A command can never name another person's workspace: an id names the caller's own or nothing. That holds for an administrator too.
  - The query watches only the key pattern `ws_<userId>.*`.
  - The resource type `workspace` (`read`, `write`, both `self`) sits at `tenant:<t>/user:<u>/workspaces`. Agents, apps and services have no workspaces: their commands are refused and the query fails.
- **Default** has the fixed id `default`. The internal `workspace.makeDefault` makes it, sent by `workspace.EnsureDefault` on the first read of a person's workspaces in a tenant.
  - The command id is fixed (`ws-default-<tenant>-<user>`), and the commit expects no workspace there.
  - So processes reading at once make one Default between them (`TestDefaultIsMadeOnceUnderConcurrentReads`).

### Commands, query and tools
| Command | Args | What it does |
|---|---|---|
| `workspace.create` | `{name, from?}` | The client sends the id (`w<tsid>`), so a resend makes one; a create on an existing id does nothing. Without a target (the chat's tool), `NewTarget` makes one. It goes after the others in the menu. `from` duplicates another workspace's `layout` and `context`, read through the decision. |
| `workspace.rename` | `{name}` | Default can be renamed too. |
| `workspace.move` | `{position}` | Renumbers every one of the person's workspaces in one atomic batch. Each is read through the decision, so the commit fails if any changed in between. |
| `workspace.delete` | `{}` | Refuses Default (`default_workspace`). Deleting one that is already gone succeeds and does nothing. |
| `workspace.update` | `{version, layout?, context?}` | For 16b and 16c; `NoTool`. A stale `version` is refused (`stale_workspace`), and the client merges and retries. Only the named parts change. |

- **Names** are trimmed, have at most 60 characters (`workspace.MaxName`) and are unique per person, compared by `workspace.Normalized` (letters and digits, lower case). A clash is `workspace_name_taken`.
  - Uniqueness is checked against the person's workspaces as the store has them, not under an expected sequence. Two tabs that make the same name in the same instant can both succeed. This is accepted, since it is one person's own list.
- A missing workspace is `workspace_not_found`.
- **`workspace.list`** (live query, `workspace.read`) gives `{workspaces: [...]}` in the menu's order (position, then name), sent whole on every change. Its first read makes Default, and its snapshot waits for it.
- **Tools for the chat** (16a question 7):
  - `workspace.create`, `rename`, `move` and `delete` are the commands' own tools (writes, which ask).
  - `workspace.list` (read) lists `{id, name, isDefault}`.
  - `workspace.switch` is a browser client tool (below).
- **The workbench context** sends the id of the workspace the tab shows (`WorkbenchSnapshot.workspace`). The `workbench` provider reads its name from the caller's own workspaces (`workbenchcontext.SetWorkspaceNames`, wired in `serve.go` to `workspace.Name`) and tells the model *This browser tab shows the workspace "…"*. It never takes a name from the browser.
- Wiring (both `serve.go`s): `workspace.ContributeAccess`, then `workspace.Contribute` before `livetools.Contribute`, with `workspace.Deps{Store, Bus}`. `apps/tools`' `TestMetadata` registers it.

### In the browser (`frontend/common/src/workspace/`)
- **`workspaceStore.ts`** holds one `workspace.list` subscription, started by the first `useWorkspace()`.
  - **Which workspace a tab shows**: `sessionStorage` keeps this tab's own id and `localStorage` the last one used in this browser, both per tenant and user (`workspace.current.<tenant>.<user>`, `workspace.last.…`).
  - A new tab opens the last one used, and a reload keeps its own. An id that no longer exists (deleted elsewhere) falls back to Default quietly, never to the last one (`current.ts` `chooseCurrent`, unit-tested).
  - `workspaces.switchTo(id)`, `create`, `createAndSwitch`, `duplicate` (named *X copy*, *X copy 2*…, then shown), `rename`, `move` and `remove`. Removing the workspace in use switches to Default first.
  - A workspace just made is acknowledged before the list's change arrives. `switchTo(id, true)` keeps it pending for up to ten seconds and switches when it appears.
  - **16b, 16c and 16d follow the current workspace through `useWorkspace()`** (`{workspaces, loaded, current}`).
- **The Workspaces menu** (`WorkspaceMenu.tsx`, 16a question 6) is a button in the side bar above the account button.
  - The button shows the current workspace's initials, with *Workspace: <name>* as its tooltip and accessible name.
  - The menu lists the workspaces with the current one ticked, then *New workspace…*, *Rename…*, *Duplicate*, *Move up*, *Move down* and *Delete…*. *Delete…* is absent on Default, and its confirmation says *Its chats stay in your other workspaces.*
  - Names are checked as the server checks them (`nameProblem`), and a refusal is shown in the dialog.
- **`/workspace`** (`contributions.ts`): `/workspace` lists them, the current one marked; `/workspace <name>` switches by name or a unique prefix (`matchWorkspace`); `/workspace new <name>` makes one and switches to it.
- **`workspace.switch`** (client tool, effect `ui`) switches the tab by `id` or `name`. It waits up to two seconds for a workspace the model has just made.

### Tests
- `go test ./features/workspace`:
  - Default made once under concurrent reads from two processes;
  - idempotent create, names (trim, length, clashes), rename, positions;
  - duplicate copying layout and context, stale `update` refused, Default not deleted;
  - move renumbering;
  - privacy: Bo can't see, rename or delete Ana's, and an agent has none;
  - the live query following another process;
  - the tools.
- `go test ./features/workbench/context` checks that the name is read on the server.
- `yarn test` in `frontend/common` runs `src/workspace/current.test.ts`.
- Forge's walk `documentation/walks/16a.json` and its `forge workspaces` tool cover the rest ([forge plans/016a-WorkspaceRecords.md › Test](../../forge/documentation/plans/016a-WorkspaceRecords.md#test)).

![The Workspaces menu](../../forge/documentation/images/workspaces-menu.png)

## Built in forge 16b

A workspace now holds its **open editors and their layout**: switching shows the other workspace's editors and splits, and a reload or another browser shows what was saved. Planned in forge's [016b-WorkspaceLayout.md](../../forge/documentation/plans/016b-WorkspaceLayout.md).

### Decisions
- **Editors that cannot be restored** (question 8, 2026-11-25): a switch closes them, as a reload does. A tab with unsaved changes asks first, *Switch anyway* or *Stay*.
- **Two windows on one workspace** (question 9, 2026-11-25, the user's answer): the person chooses, per window, between ***Live follow*** and ***Don't save this window***. So the browser tracks which windows show each workspace.

Decided while building:
- **Which windows show a workspace is presence.** Each window is present in `ws_<userId>.<workspaceId>` (`live.presence`). Only that person may join or follow such an object: `presence.Owned("ws")`, registered by `workspace.Contribute` ([Live.md › Changed in forge 16b](Live.md#changed-in-forge-16b)). It needs no new state and works across processes, since presence is a KV bucket.
- **A window alone on its workspace saves and follows.** The question is asked only of a window that opens a workspace (on load or by a switch) while another window already shows it. The first window is never asked.
- **The choice lasts for the tab**, per workspace (`sessionStorage`). It applies again after a reload only while another window is still there; a window found alone saves and forgets the choice.
- **Live follow takes the other window's saved layout as it arrives**, replacing the model with `Model.fromJson(json, previous)`. Tabs both layouts hold keep their view state and stay mounted, and so do the border tabs, so the chat is untouched. The following window keeps its own unsaveable tabs.
- **Last writer wins** between following windows. A window with a change pending or in flight ignores incoming layouts; its own save, refused as stale, is sent again on the newer version.
- **Don't save this window** neither saves nor follows. The Workspaces button shows a dot (amber once the workspace has changed elsewhere), the menu says *Not saved in this window*, and *Live follow* there takes the saved layout and joins. A saving window beside others has *Don't save this window* in its menu.
- **A deleted workspace's window falls back to Default without asking.** The deletion was confirmed already, and Default's layout is shown.
- **The size of a layout or context** is at most 256 KB (`workspace.MaxPart`, `workspace_too_large`).
- **Markdown counts edits its autosave has not sent yet as unsaved** (`CollabState.pending`, reported as `dirty`). Before this, `dirty` covered only a save in flight.

### What is saved
- `layout` is `{format: 1, layout: <flexlayout row>}`: the main area's rows, tabsets, weights, the active tabset and each tabset's selected tab (`workspace/layout.ts`, `layoutToSave`).
- Only **restorable** tabs are kept: app editor kinds, and editors with `restorable: true`. A tabset left empty, and a row left empty, are removed. A tabset's selected tab stays selected when kept; otherwise the tab now in its place is selected.
- Border tabs and their open state are not part of a workspace. They are contributions, the same in every workspace.
- The side bar's open and collapsed views stay per browser.

### In the browser
- **`Workbench.tsx`** holds the model in state. `EditorRegistry.setModel` swaps it under the same registry, so editors keep the registry they were given.
  - The model starts empty. Deep links (`?logs=`, `?app=`) open into it, and the workspace's layout is shown when `useWorkspace()` brings it, keeping those tabs. If they change the layout, it is saved.
  - On a switch the old workspace's layout is saved at once as it was (`LayoutSaver.retire`). Then the other's is shown, closing every tab it lacks.
- **`LayoutSaver`** (`workspace/layoutSaver.ts`) sends `workspace.update {version, layout}` half a second after the last layout change (a drag sends one).
  - It skips a layout the workspace already holds.
  - It uses the version its own last save made when the list has not brought it yet.
  - On `stale_workspace` it waits for a newer version and sends again (the window's layout wins).
  - It sends nothing while the window does not save.
- **`workspaceWindows`** (`workspace/windows.ts`) joins the workspace's presence on every switch and decides the window's mode from the first list of who is there: `checking`, `asking`, `saving` or `unsaved`. `WorkspaceWindowsDialog` asks; the menu shows the mode.
- **The switch guard** (`workspaces.setSwitchGuard`): before a switch, the workbench lists the tabs the target lacks whose subject reports `dirty`. If any, it shows *Switch to <name>?*, and `switchTo` returns false.
  - *Stay* keeps the workspace; *Switch anyway* calls `switchTo(id, true, true)`.
  - `/workspace` and the `workspace.switch` tool say when the person was asked (`switched: false`).
- **The move-in**: the first time a browser opens *Default* while it has no layout, the tabs this browser kept before workspaces (`workbench.tabs.<tenant>.<user>` in `localStorage`) become its layout, and the key is removed. A browser that finds a layout there takes it and removes its own key.

### Tests
- `go test ./features/workspace`: a layout over `MaxPart` is refused; `TestWindowsPresenceIsPrivate` (a person's own workspace object, not another's, not an agent's; ordinary objects unchanged).
- `yarn test` in `frontend/common` runs `src/workspace/layout.test.ts`:
  - the saved layout has no unsaveable or border tabs and no empty tabsets, and keeps the selected tab;
  - a split layout restores as it was;
  - kept tabs join the active tabset;
  - the move-in makes one tabset;
  - a drag sends one update, and the same layout again sends none;
  - a stale save is sent again on the newer version;
  - a retired saver sends the layout it had, not the next workspace's;
  - a window that does not save sends nothing.
- Forge's walk `documentation/walks/16b.json` and its `forge workspaces` tool (now printing each layout's editors) cover the rest ([forge plans/016b-WorkspaceLayout.md › Test](../../forge/documentation/plans/016b-WorkspaceLayout.md#test)).

![Default: Data Designer and Hello side by side](../../forge/documentation/images/workspace-layout-default.png)
![A second window opening Support, which the first window shows](../../forge/documentation/images/workspace-windows-ask.png)
![A Markdown file with unsaved changes asks before a switch](../../forge/documentation/images/workspace-switch-unsaved.png)

## Built in forge 16c

A workspace now holds its **active knowledge**: which Volumes chat searches there and which Chronicle entries it always uses. Planned in forge's [016c-ActiveKnowledge.md](../../forge/documentation/plans/016c-ActiveKnowledge.md).

### Decisions (2026-11-25, all four on their proposals)
- **Agents are unchanged** (question 1). A run has no workspace, so an agent searches its own Volumes, or every Volume its maker may read.
- **Chat's RAG Search follows the workspace** (question 3). Its Volume source and `kb.search` search only the checked Volumes, within the person's grants, and the checked entries replace the per-conversation checks. The dials and Top-K stay per conversation.
- **What a workspace lists** (question 10). Every Volume and entry the person can read is listed in every workspace. Volumes are checked unless unchecked there; entries are unchecked unless checked there. So *Default* and a new workspace search what chat searched before, and a new Volume is searched everywhere. *Duplicate* copies the checks.
- **Where they are edited** (question 11). The Chronicles side view became **Context**, with a *Volumes* group above the entries. The Volumes editor shows *Active in this workspace* on each Volume.

Decided while building:
- **Only differences from the defaults are stored.** `context` is `{"<kind>": {"<id>": checked}}`, such as `{"volume": {"42": false}, "chronicle": {"7": true}}` (`workspace.Context`, `ParseContext`, `Checked`, `Differing`). A workspace back at its defaults stores `{}`. Kinds are open: a later source adds its own (`CheckedByDefault` says unknown kinds start unchecked). `workspace.update` refuses a context of another shape (`bad_context`).
- **The workspace's Volumes narrow by exclusion.** A round leaves out the unchecked Volumes (`ExceptVolumeIDs`), so it never needs the list of Volumes. A Volume the person can no longer read stays listed, greyed, and the library source's own `volume.read` check still drops it. A workspace can only narrow what the person may read, never widen it.
- **The narrowing rides on the call as a `retrieval.Scope`**, separate from `retrieval.Options` ([ContextRetrieval.md › Changed in forge 16c](ContextRetrieval.md#changed-in-forge-16c)). A model node in the round's graph sets its own options (an Agent node's Retrieval group, *Run mode*), but never removes the scope, so nothing a round runs inline searches a Volume its workspace left out.
- **A checked entry is pinned with the reason `checked`.** An unchecked entry can still be found by retrieval and chosen by stage 1, as before 16c, with the reason `retrieved` (the walk checks both).
- **No workspace named means the person's Default**, so API callers and old browsers get it. A Default not yet made has the defaults. An id the caller does not own, someone else's included, is refused with `404 workspace_not_found` before a round is stored.

### On the server
- **`conversation.send` takes `workspace`** (`SendArgs.Workspace`, the tab's id). `Send` reads it before storing the round through `conversationimpl.WithWorkspaces(read)`. The reader is `workspace.KnowledgeReader(deps)`, which reads the caller's own record (`workspace.Get`, user principals only), wired in both `serve.go`s.
- Its `conversation.WorkspaceKnowledge {ID, Name, ExceptVolumeIDs, CheckedEntries}` is applied in two places:
  - `run` puts `retrieval.Scope{ExceptVolumeIDs}` on the round's context, so RAG Search's gather and every `kb.search` the round runs leave those Volumes out;
  - `settingsOf` puts `CheckedEntries` in the `chronicle` settings' `checked`, in place of any the conversation kept. Chronicle's *Chronicle settings* node reads them as before.
- Without `WithWorkspaces` (tests, Foundation's own stacks before wiring), rounds have no workspace and search as before.

### In the browser
- **`workspace/context.ts`** (unit-tested): `parseContext`, `isChecked`, `withCheck` (keeps only differences), and **`ContextWriter`**:
  - each toggle shows at once and is sent through `workspace.update {version, context}` on the newest version this browser saw;
  - on `stale_workspace` it waits for the newer version and re-applies its own toggles there, so another window's checks are kept;
  - a toggle stays shown until the list brings the version that holds it, and a failed write shows the saved checks again.
- **`workspace/contextStore.ts`**: `useActiveKnowledge()` gives `{workspace, context, checked(kind, id), setChecked(kind, id, on), error}` for the tab's workspace, one writer per workspace. Every window follows the live `workspace.list`.
- **`workspace/volumes.ts`**: `useVolumes()` follows `kb.volumes`.
- **The Context view** (`chronicle/ContextView.tsx`, side view id `context`; a browser that remembered `chronicles` opens it):
  - a caption names the workspace;
  - the *Volumes* group has a checkbox each (*Search <name>*), and clicking a name opens the Volume;
  - the entry tree's checkboxes (*Always use <title>*) are the workspace's, so the view works with no chat open;
  - items the workspace names that the person can no longer read are greyed;
  - the dot and ✎ for the focused chat's last reply stay (`chatFocus` no longer carries checks).
- **RAG Search** (`RagControl`) says *Volumes and memory: from the workspace "<name>"*, with a link that opens the Context view (`workbench/sideView.ts`, `openSideView(id)`, which the side bar follows). **Chronicles' ⚙** counts the entries checked in the workspace.
- **The Volumes editor** shows *Active in this workspace* on each Volume, matched to `kb.volumes` by path.
- **The chat sends `workspace`** with each message (`useChatStream`, `Conversation.send`).

### Tests
- `go test ./features/conversation/services/conversationservice/impl -run 'Workspace|Scope'` runs over the real library and chronicle sources, the real `rag` provider and the real `kb.search` (`retrievaltest`):
  - a round in Support finds only Support's passage, and so does its `kb.search`;
  - HR the other way round;
  - no workspace named reads Default and searches both;
  - an unknown id and another person's id are refused before any search;
  - the workspace's checked entries replace the conversation's;
  - a scope outlasts options set after it.
- `go test ./features/workspace`, `TestContextForRounds`:
  - a new workspace has the defaults, and a context of the wrong shape is refused;
  - Duplicate copies the checks;
  - Bo and an agent read nothing of Ana's;
  - the defaults per kind.
- `lancestore` `TestStore` (through `build.sh`) checks `volume_id NOT IN`, alone and with a list.
- `yarn test` in `frontend/common` runs `src/workspace/context.test.ts`:
  - the defaults, and only differences stored;
  - one write per toggle;
  - toggles queued behind a write;
  - a stale write re-applied over another window's toggle;
  - a toggle undone while being written;
  - a failed write.
- Agents' existing retrieval tests (Forge `starter/agents`, `starter/mail`) pass unchanged.
- Forge's walk `documentation/walks/16c.json` (53 steps) and `forge workspaces --context` cover the rest ([forge plans/016c-ActiveKnowledge.md › Test](../../forge/documentation/plans/016c-ActiveKnowledge.md#test)).

![Support: HR unchecked, Refund tone checked, and the replies in Default and Support](../../forge/documentation/images/workspace-context.png)
![RAG Search: Volumes and memory from the workspace](../../forge/documentation/images/workspace-context-rag.png)

## Built in forge 16d

A workspace now has its **chats**: the Chat tab lists only the conversations of the tab's workspace, a conversation can be shown in other workspaces or moved to one, and deleting a workspace keeps its chats in *Default*. Planned in forge's [016d-WorkspaceChats.md](../../forge/documentation/plans/016d-WorkspaceChats.md).

### Decisions (2026-11-25, both on their proposals)
- **A chat in several workspaces** (question 4). A conversation belongs to the workspace its first message was sent from. *Show in…* lists it in others too, *Move to…* changes where it belongs, and deleting a workspace moves its chats to *Default*.
- **App surfaces** (question 12). Every chat surface follows the tab's workspace. Today the Chat editor is the only one (`ChatSurface` `document` has no editor yet), so this is the Chat tab wherever it is docked.

Decided while building:
- **Chats are not part of the workspace record.** Conversations are rows in the tenant database, so where one is listed is too: `Conversation.WorkspaceID`, and a `ConversationWorkspace` row (conversation, workspace, owner) per workspace it is shown in. The workspace's `layout` and `context` are untouched, and showing a chat never races a layout save.
- **Whether a workspace exists is read, never stored.** `conversation.OwnWorkspace(c, ids)` is the conversation's workspace while the owner still has it, else their *Default*. So a deleted workspace's chats are *Default*'s at once, in every process, with no event to follow and nothing to lose if a process stops. A conversation from before workspaces (an empty id) is *Default*'s the same way: that is the migration.
- **Default's list writes this down.** Listing *Default* (its first page) sets `workspace_id = 'default'` on the person's conversations whose workspace is gone and deletes rows naming a workspace that is gone. It is idempotent, and it skips anything made in the last minute (`settleAfter`): a workspace just made in another process may not be in this process's view yet, and its chats are listed in *Default* meanwhile but not moved.
- **Continuing a chat from another workspace's tab** (a chat shown there) keeps it where it belongs. `Send` now updates only the columns a round sets on an existing conversation, so a *Move to…* made during a round is kept.
- **The Dashboard's top conversations** (`list` with `ids`) ignore workspaces unless one is named.
- **Other windows see a change on their next load** (a switch, a reload or reopening the Chat tab). Conversations have no live query yet, so the plan's "through the conversation stream" was not built (forge plan › TODO).
- **Links that open a conversation** do not exist yet (no `workbench:` subject for one), so the plan's *From your Support workspace … Show here* for a linked chat waits for them. A chat shown from another workspace carries that note on its divider.

### On the server
- `conversation.Conversation.WorkspaceID` (`not null default ''`, indexed) and the table `conversation_workspaces` (`conversation.ConversationWorkspace`, unique per conversation and workspace), both in `conversation.Models()`.
- **A new conversation** takes the round's workspace (`SendArgs.Workspace`, already checked as the caller's by 16c), else `default`.
- **`list`** (`ListArgs.Workspace`, REST `workspace`) lists one of the caller's workspaces: its own conversations and those shown there. Empty is *Default*, unless `ids` are given. An id that is not one of the caller's is `404 workspace_not_found`. Each conversation carries `workspace` (where it belongs) and `shownIn` (the caller's other workspaces it is shown in; rows for a workspace that is gone are left out).
- **`POST /conversation/workspaces {conversationId, move?, shownIn?}`** (`SetWorkspaces`, action `conversation.write`): `move` sets where it belongs, and `shownIn` replaces every workspace it is shown in besides that one (`[]` for none, left out to keep them). Every id must be the caller's. Only the owner finds the conversation (another person's is `conversation_not_found`). Repeating a call changes nothing. Moving to a workspace it was shown in removes that row.
- The service reads the caller's workspace ids through `conversationimpl.WithWorkspaceList(workspace.IDLister(deps))`, wired in both `serve.go`s, on every call. Without it (tests, stacks without workspaces) `list` ignores workspaces and `SetWorkspaces` is `503 no_workspaces`.
- `workspace.delete`'s description says its chats move to *Default*.

### In the browser
- **`useChatStream(workspace, ready)`** lists the tab's workspace, once the workspaces are known. A switch starts the scroll again on the other workspace's latest conversation; a load for a workspace since left is dropped. A message still streaming keeps going on the server.
- **The transcript loads earlier conversations by itself while it is too short to scroll**: before, a short latest conversation hid every earlier one, since no scroll event asked for them.
- **Each conversation's divider** has a ⋮ menu (*Workspaces of <title>*): *Show in…* is a checklist of the person's other workspaces, each tick sent at once; *Move to…* lists them. A conversation that belongs to another workspace says *From your <name> workspace*. A moved chat stays in the scroll until the next load.
- The Workspaces menu's *Delete…* confirmation says *Its chats move to <Default's name>; nothing is lost.*
- `Conversations.setWorkspaces` is the client; `Conversation` has `workspace` and `shownIn`.

### Tests
- `go test ./features/conversation/services/conversationservice/impl -run 'Chats|Deleted|Old'` (`Chats_test.go`, with a map of each person's workspaces):
  - a new conversation stores its workspace, and continuing it from another workspace keeps it;
  - Support's list has Support's alone, Default's has Default's, none named is Default, `ids` ignore workspaces;
  - another person's workspace, and an unknown one, are refused; Bo sees none of Ana's;
  - *Show in* lists it in both, twice changes nothing; Bo can neither show nor move Ana's, and Ana cannot use Bo's workspace;
  - *Move to* leaves the old workspace, keeps it shown elsewhere, and moving to a workspace it was shown in drops that row;
  - a deleted workspace's chats are Default's at once, written down only once older than `settleAfter`, its rows deleted, others' chats untouched;
  - a table from before workspaces is migrated, and its conversations are Default's.
- Forge's walk `documentation/walks/16d.json` (64 steps) and its `forge conversations` and `forge workspaces --chats` tools cover the rest ([forge plans/016d-WorkspaceChats.md › Test](../../forge/documentation/plans/016d-WorkspaceChats.md#test)).

![Show in… on a Support chat, Default ticked](../../forge/documentation/images/workspace-chats-show-in.png)
![Default's Chat tab: its own chat and one shown from Support](../../forge/documentation/images/workspace-chats-default.png)
